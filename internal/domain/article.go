package domain

import (
	"slices"
	"time"
)

type ArticleID int64

type Article struct {
	ID          ArticleID
	SourceID    SourceID
	SourceTitle string
	URL         URL
	Title       string
	Excerpt     string
	PublishedAt time.Time
}

func newestFirst(a, b Article) int {
	return b.PublishedAt.Compare(a.PublishedAt)
}

func CutCandidates(articles []Article, total, perSource int) []Article {
	sorted := slices.SortedStableFunc(slices.Values(articles), newestFirst)

	kept := make([]Article, 0, len(sorted))
	counts := make(map[SourceID]int)
	for _, article := range sorted {
		if counts[article.SourceID] >= perSource {
			continue
		}
		counts[article.SourceID]++
		kept = append(kept, article)
	}

	if len(kept) > total {
		kept = kept[:total]
	}
	return kept
}
