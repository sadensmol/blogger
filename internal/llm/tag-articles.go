package llm

import (
	"context"

	"github.com/rs/zerolog/log"

	"github.com/sadensmol/blogger/internal/domain"
)

type tagJobInput struct {
	Articles  []tagArticleJSON `json:"articles"`
	KnownTags []string         `json:"known_tags"`
}

type tagArticleJSON struct {
	URL     string `json:"url"`
	Title   string `json:"title"`
	Source  string `json:"source"`
	Excerpt string `json:"excerpt"`
}

func (a domainArticle) toTagArticleJSON() tagArticleJSON {
	return tagArticleJSON{URL: string(a.URL), Title: a.Title, Source: a.SourceTitle, Excerpt: a.Excerpt}
}

type tagAnswer struct {
	Articles []tagAnswerJSON `json:"articles"`
}

type tagAnswerJSON struct {
	URL  string   `json:"url"`
	Tags []string `json:"tags"`
}

// TagArticles asks for the themes of the articles. It maps each asked article with a valid tag to its tags.
// An answered url that is not one of the articles is dropped.
func (r *Runner) TagArticles(ctx context.Context, articles []domain.Article, known []domain.Tag) (map[domain.URL][]domain.Tag, Backend, error) {
	articlesJSON := make([]tagArticleJSON, 0, len(articles))
	asked := make(map[domain.URL]bool, len(articles))
	for _, article := range articles {
		articlesJSON = append(articlesJSON, domainArticle(article).toTagArticleJSON())
		asked[article.URL] = true
	}
	knownJSON := make([]string, 0, len(known))
	for _, tag := range known {
		knownJSON = append(knownJSON, string(tag))
	}
	req := jobRequest{
		skill: SkillTagArticles,
		input: tagJobInput{Articles: articlesJSON, KnownTags: knownJSON},
	}

	var tags map[domain.URL][]domain.Tag
	backend, err := r.run(ctx, req, func(raw []byte) error {
		var answer tagAnswer
		if err := decodeAnswer(raw, &answer); err != nil {
			return err
		}
		answered := make(map[domain.URL][]domain.Tag, len(answer.Articles))
		var unknown []string
		for _, a := range answer.Articles {
			u, err := domain.ParseURL(a.URL)
			if err != nil || !asked[u] {
				unknown = append(unknown, a.URL)
				continue
			}
			if parsed := domain.ParseTags(a.Tags); len(parsed) > 0 {
				answered[u] = parsed
			}
		}
		if len(unknown) > 0 {
			log.Ctx(ctx).Warn().Msgf("%s answer names %d urls that are not its articles, they are dropped: %q",
				SkillTagArticles, len(unknown), unknown)
		}
		tags = answered
		return nil
	})
	if err != nil {
		return nil, "", err
	}
	return tags, backend, nil
}
