package domain

import (
	"fmt"
	"unicode/utf8"
)

const maxDescriptionLength = 300

type Pick struct {
	URL         URL
	Description string
	Topics      []string
	Reason      string
}

func ValidatePicks(picks []Pick, candidates []Article, max int) error {
	if len(picks) > max {
		return fmt.Errorf("got %d picks, at most %d allowed: %w", len(picks), max, ErrBadOutput)
	}

	known := make(map[URL]bool, len(candidates))
	for _, candidate := range candidates {
		known[candidate.URL] = true
	}

	seen := make(map[URL]bool, len(picks))
	for _, pick := range picks {
		if !known[pick.URL] {
			return fmt.Errorf("pick url %s is not a candidate: %w", pick.URL, ErrBadOutput)
		}
		if seen[pick.URL] {
			return fmt.Errorf("pick url %s appears twice: %w", pick.URL, ErrBadOutput)
		}
		seen[pick.URL] = true

		length := utf8.RuneCountInString(pick.Description)
		if length == 0 || length > maxDescriptionLength {
			return fmt.Errorf("pick %s description has %d characters, want 1 to %d: %w",
				pick.URL, length, maxDescriptionLength, ErrBadOutput)
		}
	}
	return nil
}

type PostID int64

type Post struct {
	ID        PostID
	ArticleID ArticleID
	SourceID  SourceID // filled on read
	RunID     RunID
	ChannelID string
	MessageTS string
	Pick      Pick
}

type VotedArticle struct {
	Article Article
	Topics  []string
	Vote    Vote
}

type BlogProposal struct {
	SiteURL, FeedURL URL
	Title, Reason    string
}
