package tests

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
)

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

	// a call to BlockMethod runs OnBlock, then waits until the client gives up
	BlockMethod string
	OnBlock     func()

	server    *httptest.Server
	mu        sync.Mutex
	posted    []string
	reactions []string
	replies   []string
	counter   int
	postCalls int
}

func NewFakeSlack() *FakeSlack {
	f := &FakeSlack{}
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

	f.mu.Lock()
	defer f.mu.Unlock()

	if strings.HasPrefix(r.URL.Path, "/response/") {
		raw, _ := io.ReadAll(r.Body)
		f.replies = append(f.replies, string(raw))
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
			fmt.Fprint(w, `{"ok":false,"error":"channel_not_found"}`)
			return
		}
		if fault == PostNotInChannel {
			fmt.Fprint(w, `{"ok":false,"error":"not_in_channel"}`)
			return
		}
		f.counter++
		f.posted = append(f.posted, r.PostForm.Get("text"))
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
	case "reactions.add":
		f.reactions = append(f.reactions, fmt.Sprintf("%s:%s", r.PostForm.Get("timestamp"), r.PostForm.Get("name")))
		_, _ = w.Write([]byte(`{"ok":true}`))
	case "auth.test":
		_, _ = w.Write([]byte(`{"ok":true,"user_id":"UBOT"}`))
	default:
		_, _ = w.Write([]byte(`{"ok":false,"error":"unknown_method"}`))
	}
}

func (f *FakeSlack) Posted() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.posted...)
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
