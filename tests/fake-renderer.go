package tests

import (
	"context"
	"fmt"
	"sync"

	"github.com/sadensmol/blogger/internal/domain"
)

// FakeRenderer stands in for the browser: it answers the html set for a page.
type FakeRenderer struct {
	mu       sync.Mutex
	pages    map[domain.URL]string
	rendered []domain.URL
}

func NewFakeRenderer() *FakeRenderer {
	return &FakeRenderer{pages: map[domain.URL]string{}}
}

func (r *FakeRenderer) Set(pageURL domain.URL, body string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.pages[pageURL] = body
}

func (r *FakeRenderer) Render(_ context.Context, pageURL domain.URL) ([]byte, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.rendered = append(r.rendered, pageURL)
	body, ok := r.pages[pageURL]
	if !ok {
		return nil, fmt.Errorf("cannot render %s: no page set", pageURL)
	}
	return []byte(body), nil
}

// Rendered lists the pages asked for, oldest first.
func (r *FakeRenderer) Rendered() []domain.URL {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]domain.URL(nil), r.rendered...)
}
