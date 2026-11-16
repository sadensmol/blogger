package domain

import "slices"

type Theme struct {
	Tag   Tag
	Votes Votes
}

type Themes []Theme

func (t Themes) Score(tags []Tag) int {
	score := 0
	for _, tag := range tags {
		for _, theme := range t {
			if theme.Tag == tag {
				score += theme.Votes.Net()
			}
		}
	}
	return score
}

// Rank orders the articles by the score of their tags, highest first. Equal scores keep their order.
func (t Themes) Rank(articles []Article) []Article {
	scores := make(map[URL]int, len(articles))
	for _, article := range articles {
		scores[article.URL] = t.Score(article.Tags)
	}
	ranked := slices.Clone(articles)
	slices.SortStableFunc(ranked, func(a, b Article) int {
		return scores[b.URL] - scores[a.URL]
	})
	return ranked
}
