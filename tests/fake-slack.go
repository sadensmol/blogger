package tests

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/gorilla/websocket"
)

const maxSectionText = 3000

type PostFault int

const (
	PostOK PostFault = iota
	PostRejected // slack answers ok:false and posts nothing
	PostDropped  // slack posts the message, then the connection drops before the answer
	PostNotInChannel // slack answers not_in_channel: the bot is not a member of the channel
)

type FakeSlack struct {
	URL        string
	FailMethod string

	// PostFaults answers the chat.postMessage calls in order; calls past the list succeed
	PostFaults []PostFault

	// EphemeralError makes chat.postEphemeral answer ok:false with this error code
	EphemeralError string

	// a call to BlockMethod runs OnBlock, then waits until the client gives up
	BlockMethod string
	OnBlock     func()

	server    *httptest.Server
	envelopes chan string
	mu        sync.Mutex
	posted    []string
	postForms []url.Values
	updates   []SlackUpdate
	reactions []string
	replies   []string
	acks      []string
	ephemeral []SlackEphemeral
	calls     []SlackCall
	counter   int
	postCalls int
	// channelFault is the error code of a channel-level post fault; slack refuses private messages to that channel too
	channelFault string
}

type SlackUpdate struct {
	Channel, TS, Blocks string
}

// SlackEphemeral is a private channel message; Message holds its text and blocks as one JSON object.
type SlackEphemeral struct {
	Channel, User, Message string
}

const ResponseCall = "response"

// SlackCall is one call the bot made, in the order slack got them; Error is slack's error code of a refused call.
// Method is the web API method, or ResponseCall for a reply to a response URL.
type SlackCall struct {
	Method, Text, Error string
}

func NewFakeSlack() *FakeSlack {
	f := &FakeSlack{envelopes: make(chan string, 10)}
	f.server = httptest.NewServer(http.HandlerFunc(f.serve))
	f.URL = f.server.URL
	return f
}

func (f *FakeSlack) Close() {
	f.server.Close()
}

func (f *FakeSlack) serve(w http.ResponseWriter, r *http.Request) {
	method := strings.TrimPrefix(r.URL.Path, "/api/")
	if method == f.BlockMethod && f.OnBlock != nil {
		// the server sees the client go away only after the body is read
		if err := r.ParseForm(); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		f.OnBlock()
		<-r.Context().Done()
		return
	}

	if r.URL.Path == "/socket" {
		f.serveSocket(w, r)
		return
	}

	f.mu.Lock()
	defer f.mu.Unlock()

	if strings.HasPrefix(r.URL.Path, "/response/") {
		raw, _ := io.ReadAll(r.Body)
		f.replies = append(f.replies, string(raw))
		var reply struct {
			Text string `json:"text"`
		}
		_ = json.Unmarshal(raw, &reply)
		f.calls = append(f.calls, SlackCall{Method: ResponseCall, Text: reply.Text})
		w.WriteHeader(http.StatusOK)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if method == f.FailMethod {
		_, _ = w.Write([]byte(`{"ok":false,"error":"fake_failure"}`))
		return
	}

	_ = r.ParseForm()
	switch method {
	case "chat.postMessage":
		fault := PostOK
		if f.postCalls < len(f.PostFaults) {
			fault = f.PostFaults[f.postCalls]
		}
		f.postCalls++
		if fault == PostRejected {
			f.channelFault = "channel_not_found"
			f.calls = append(f.calls, SlackCall{Method: method, Text: r.PostForm.Get("text"), Error: "channel_not_found"})
			fmt.Fprint(w, `{"ok":false,"error":"channel_not_found"}`)
			return
		}
		if fault == PostNotInChannel {
			f.channelFault = "not_in_channel"
			f.calls = append(f.calls, SlackCall{Method: method, Text: r.PostForm.Get("text"), Error: "not_in_channel"})
			fmt.Fprint(w, `{"ok":false,"error":"not_in_channel"}`)
			return
		}
		if tooLongSection(r.PostForm.Get("blocks")) {
			f.calls = append(f.calls, SlackCall{Method: method, Text: r.PostForm.Get("text"), Error: "invalid_blocks"})
			fmt.Fprint(w, `{"ok":false,"error":"invalid_blocks"}`)
			return
		}
		f.calls = append(f.calls, SlackCall{Method: method, Text: r.PostForm.Get("text")})
		f.channelFault = ""
		f.counter++
		f.posted = append(f.posted, r.PostForm.Get("text"))
		f.postForms = append(f.postForms, r.PostForm)
		if fault == PostDropped {
			conn, _, err := w.(http.Hijacker).Hijack()
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			conn.Close()
			return
		}
		fmt.Fprintf(w, `{"ok":true,"channel":"C1","ts":"%d.000100"}`, f.counter)
	case "chat.update":
		if tooLongSection(r.PostForm.Get("blocks")) {
			fmt.Fprint(w, `{"ok":false,"error":"invalid_blocks"}`)
			return
		}
		f.updates = append(f.updates, SlackUpdate{Channel: r.PostForm.Get("channel"), TS: r.PostForm.Get("ts"), Blocks: r.PostForm.Get("blocks")})
		fmt.Fprintf(w, `{"ok":true,"channel":%q,"ts":%q}`, r.PostForm.Get("channel"), r.PostForm.Get("ts"))
	case "reactions.add":
		reaction := fmt.Sprintf("%s:%s", r.PostForm.Get("timestamp"), r.PostForm.Get("name"))
		f.reactions = append(f.reactions, reaction)
		f.calls = append(f.calls, SlackCall{Method: method, Text: reaction})
		_, _ = w.Write([]byte(`{"ok":true}`))
	case "chat.postEphemeral":
		f.serveEphemeral(w, r.PostForm)
	case "apps.connections.open":
		fmt.Fprintf(w, `{"ok":true,"url":"ws%s/socket"}`, strings.TrimPrefix(f.URL, "http"))
	case "auth.test":
		_, _ = w.Write([]byte(`{"ok":true,"user_id":"UBOT"}`))
	default:
		_, _ = w.Write([]byte(`{"ok":false,"error":"unknown_method"}`))
	}
}

// serveEphemeral answers chat.postEphemeral like slack: channel and user are required, the bot must be in the channel.
func (f *FakeSlack) serveEphemeral(w http.ResponseWriter, form url.Values) {
	text, rawBlocks := form.Get("text"), form.Get("blocks")
	refuse := func(code string) {
		f.calls = append(f.calls, SlackCall{Method: "chat.postEphemeral", Text: text, Error: code})
		fmt.Fprintf(w, `{"ok":false,"error":%q}`, code)
	}
	switch {
	case f.EphemeralError != "":
		refuse(f.EphemeralError)
		return
	case f.channelFault != "":
		refuse(f.channelFault)
		return
	case form.Get("channel") == "":
		refuse("channel_not_found")
		return
	case form.Get("user") == "":
		refuse("user_not_in_channel")
		return
	case rawBlocks == "" && text == "":
		refuse("no_text")
		return
	case tooLongSection(rawBlocks):
		refuse("invalid_blocks")
		return
	}
	if rawBlocks == "" {
		rawBlocks = "null"
	}
	encodedText, err := json.Marshal(text)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	f.counter++
	f.ephemeral = append(f.ephemeral, SlackEphemeral{
		Channel: form.Get("channel"),
		User:    form.Get("user"),
		Message: fmt.Sprintf(`{"text":%s,"blocks":%s}`, encodedText, rawBlocks),
	})
	f.calls = append(f.calls, SlackCall{Method: "chat.postEphemeral", Text: text})
	fmt.Fprintf(w, `{"ok":true,"message_ts":"%d.000200"}`, f.counter)
}

// Ephemerals returns the private channel messages that slack accepted.
func (f *FakeSlack) Ephemerals() []SlackEphemeral {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]SlackEphemeral(nil), f.ephemeral...)
}

// Calls returns every post, reaction, private message and response URL reply, in the order slack got them.
func (f *FakeSlack) Calls() []SlackCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]SlackCall(nil), f.calls...)
}

func (f *FakeSlack) Posted() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.posted...)
}

// PostedForms holds the form of each posted message, in the order of Posted.
func (f *FakeSlack) PostedForms() []url.Values {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]url.Values(nil), f.postForms...)
}

func (f *FakeSlack) Updates() []SlackUpdate {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]SlackUpdate(nil), f.updates...)
}

func (f *FakeSlack) Reactions() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.reactions...)
}

func (f *FakeSlack) Replies() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.replies...)
}

// SendEnvelope sends a raw Socket Mode envelope to the connected client.
func (f *FakeSlack) SendEnvelope(raw string) {
	f.envelopes <- raw
}

// Acks returns the envelope ids that the client acknowledged.
func (f *FakeSlack) Acks() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.acks...)
}

func (f *FakeSlack) serveSocket(w http.ResponseWriter, r *http.Request) {
	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer conn.Close()
	if err := conn.WriteJSON(map[string]any{"type": "hello", "num_connections": 1}); err != nil {
		return
	}

	gone := make(chan struct{})
	go func() {
		defer close(gone)
		for {
			var ack struct {
				EnvelopeID string `json:"envelope_id"`
			}
			if err := conn.ReadJSON(&ack); err != nil {
				return
			}
			f.mu.Lock()
			f.acks = append(f.acks, ack.EnvelopeID)
			f.mu.Unlock()
		}
	}()
	for {
		select {
		case raw := <-f.envelopes:
			if err := conn.WriteMessage(websocket.TextMessage, []byte(raw)); err != nil {
				return
			}
		case <-gone:
			return
		}
	}
}

// tooLongSection models slack's invalid_blocks answer for a section text over 3000 characters.
func tooLongSection(rawBlocks string) bool {
	if rawBlocks == "" {
		return false
	}
	var blocks []struct {
		Type string `json:"type"`
		Text struct {
			Text string `json:"text"`
		} `json:"text"`
	}
	if err := json.Unmarshal([]byte(rawBlocks), &blocks); err != nil {
		return true
	}
	for _, block := range blocks {
		if block.Type == "section" && utf8.RuneCountInString(block.Text.Text) > maxSectionText {
			return true
		}
	}
	return false
}
