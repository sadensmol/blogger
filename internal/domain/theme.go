package domain

type Theme struct {
	Tag   Tag
	Votes Votes
}

type Themes []Theme

func (t Themes) Score(tags []Tag) int {
	return 0
}

func (t Themes) Rank(articles []Article) []Article {
	return articles
}
