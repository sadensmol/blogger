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
	SourceURL   URL // the blog page; filled by the candidate finders
	URL         URL
	Title       string
	Excerpt     string
	PublishedAt time.Time // zero: the date is unknown
	Shown       int       // times it was posted before; filled by the candidate finders
	Tags        []Tag
}

// PageLink is a link on a blog page with its text.
type PageLink struct {
	URL  URL
	Text string
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

type SpreadLimits struct{ Total, NewestPerSource, OlderPerSource int }

// SpreadCandidates keeps per source its newest articles and a sample spread evenly over its older ones.
// It takes from each source in turn, its newest and older articles alternately, so the total cut keeps older ones too.
// The result is newest first.
func SpreadCandidates(articles []Article, l SpreadLimits) []Article {
	sorted := slices.SortedStableFunc(slices.Values(articles), newestFirst)

	var order []SourceID
	bySource := make(map[SourceID][]Article)
	for _, article := range sorted {
		if _, ok := bySource[article.SourceID]; !ok {
			order = append(order, article.SourceID)
		}
		bySource[article.SourceID] = append(bySource[article.SourceID], article)
	}

	queues := make([][]Article, 0, len(order))
	for _, id := range order {
		all := bySource[id]
		newest := all[:min(l.NewestPerSource, len(all))]
		older := all[len(newest):]
		samples := min(l.OlderPerSource, len(older))
		sample := make([]Article, 0, samples)
		for i := range samples {
			sample = append(sample, older[(2*i+1)*len(older)/(2*samples)])
		}
		var queue []Article
		for i := range max(len(newest), len(sample)) {
			if i < len(newest) {
				queue = append(queue, newest[i])
			}
			if i < len(sample) {
				queue = append(queue, sample[i])
			}
		}
		queues = append(queues, queue)
	}

	kept := make([]Article, 0, l.Total)
	for round := 0; len(kept) < l.Total; round++ {
		taken := false
		for _, queue := range queues {
			if round < len(queue) && len(kept) < l.Total {
				kept = append(kept, queue[round])
				taken = true
			}
		}
		if !taken {
			break
		}
	}
	return slices.SortedStableFunc(slices.Values(kept), newestFirst)
}
