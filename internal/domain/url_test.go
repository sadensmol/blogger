package domain_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sadensmol/blogger/internal/domain"
)

func TestParseURL(t *testing.T) {
	tests := []struct {
		raw  string
		want domain.URL
	}{
		{"HTTPS://Example.COM/post/?utm_source=x&b=2&a=1#top", "https://example.com/post?a=1&b=2"},
		{"https://example.com", "https://example.com/"},
		{"http://example.com:80/a/", "http://example.com/a"},
		{"https://example.com/a?fbclid=1&gclid=2&ref=rss", "https://example.com/a"},
	}
	for _, tt := range tests {
		t.Run(tt.raw, func(t *testing.T) {
			got, err := domain.ParseURL(tt.raw)
			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestParseURLRejects(t *testing.T) {
	for _, raw := range []string{"ftp://x", "/relative", ""} {
		t.Run(raw, func(t *testing.T) {
			_, err := domain.ParseURL(raw)
			require.Error(t, err)
		})
	}
}
