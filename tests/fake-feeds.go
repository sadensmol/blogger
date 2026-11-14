package tests

import (
	"net/http"
	"net/http/httptest"
	"sync"
)

type fakeFeed struct {
	contentType, body string
}

type FakeFeeds struct {
	server   *httptest.Server
	mu       sync.Mutex
	feeds    map[string]fakeFeed
	requests map[string]int
}

func NewFakeFeeds() *FakeFeeds {
	f := &FakeFeeds{feeds: map[string]fakeFeed{}, requests: map[string]int{}}
	f.server = httptest.NewServer(http.HandlerFunc(f.serve))
	return f
}

func (f *FakeFeeds) serve(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	f.requests[r.URL.RequestURI()]++
	page, ok := f.feeds[r.URL.RequestURI()]
	if !ok {
		page, ok = f.feeds[r.URL.Path]
	}
	f.mu.Unlock()
	if !ok {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", page.contentType)
	_, _ = w.Write([]byte(page.body))
}

func (f *FakeFeeds) URL() string {
	return f.server.URL
}

func (f *FakeFeeds) Close() {
	f.server.Close()
}

// Set serves body at path; a path with a query wins over the same path without one.
func (f *FakeFeeds) Set(path, contentType, body string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.feeds[path] = fakeFeed{contentType: contentType, body: body}
}

// Requests counts the requests to path with its query: feed.xml?paged=2 is not feed.xml.
func (f *FakeFeeds) Requests(path string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.requests[path]
}
