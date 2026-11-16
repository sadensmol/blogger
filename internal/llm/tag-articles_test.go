package llm_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sadensmol/blogger/internal/domain"
	"github.com/sadensmol/blogger/internal/llm"
)

const (
	claudeTagSkill = "../../agent/.claude/skills/tag-articles/SKILL.md"
	codexTagSkill  = "../../agent/.agents/skills/tag-articles/SKILL.md"
)

func tagArticles() []domain.Article {
	return []domain.Article{
		{ID: 1, SourceID: 1, SourceTitle: "Example", URL: URLA, Title: "A", Excerpt: "About Go."},
		{ID: 2, SourceID: 1, SourceTitle: "Example", URL: URLB, Title: "B", Excerpt: "About SQLite."},
	}
}

func tagsAnswer(tags map[domain.URL][]string) map[string]any {
	articles := make([]map[string]any, 0, len(tags))
	for u, t := range tags {
		articles = append(articles, map[string]any{"url": u, "tags": t})
	}
	return map[string]any{"articles": articles}
}

func TestTagArticlesSendsArticlesAndKnownTags(t *testing.T) {
	f := newFakeAgents(t, llm.Claude)
	f.answer("claude", llm.SkillTagArticles, tagsAnswer(map[domain.URL][]string{URLA: {"go"}, URLB: {"sqlite"}}))

	_, _, err := f.runner().TagArticles(context.Background(), tagArticles(), []domain.Tag{"go", "databases"})

	require.NoError(t, err)
	require.FileExists(t, filepath.Join(f.home, "claude.tag-articles.input"))
	var input struct {
		Articles []struct {
			URL     string `json:"url"`
			Title   string `json:"title"`
			Source  string `json:"source"`
			Excerpt string `json:"excerpt"`
		} `json:"articles"`
		KnownTags []string `json:"known_tags"`
	}
	require.NoError(t, json.Unmarshal([]byte(f.read("claude.tag-articles.input")), &input))
	require.Len(t, input.Articles, 2)
	require.Equal(t, string(URLA), input.Articles[0].URL)
	require.Equal(t, "A", input.Articles[0].Title)
	require.Equal(t, "Example", input.Articles[0].Source)
	require.Equal(t, "About Go.", input.Articles[0].Excerpt)
	require.Equal(t, string(URLB), input.Articles[1].URL)
	require.Equal(t, []string{"go", "databases"}, input.KnownTags)
}

func TestTagArticlesReturnsTheAnsweredTags(t *testing.T) {
	f := newFakeAgents(t, llm.Claude)
	f.answer("claude", llm.SkillTagArticles, tagsAnswer(map[domain.URL][]string{URLA: {"go"}, URLB: {"sqlite", "databases"}}))

	tags, backend, err := f.runner().TagArticles(context.Background(), tagArticles(), nil)

	require.NoError(t, err)
	require.Equal(t, llm.Claude, backend)
	require.Equal(t, map[domain.URL][]domain.Tag{URLA: {"go"}, URLB: {"sqlite", "databases"}}, tags)
}

func TestTagArticlesDropsAnUnaskedURL(t *testing.T) {
	f := newFakeAgents(t, llm.Claude)
	f.answer("claude", llm.SkillTagArticles,
		tagsAnswer(map[domain.URL][]string{URLA: {"go"}, "https://evil.example/x": {"evil"}}))

	tags, _, err := f.runner().TagArticles(context.Background(), tagArticles()[:1], nil)

	require.NoError(t, err)
	require.Equal(t, map[domain.URL][]domain.Tag{URLA: {"go"}}, tags)
}

func TestTagArticlesNormalisesTheAnswer(t *testing.T) {
	f := newFakeAgents(t, llm.Claude)
	f.answer("claude", llm.SkillTagArticles, tagsAnswer(map[domain.URL][]string{
		URLA: {"Go", " go ", "SQLite", "Rust", "Zig"},
		URLB: {"", "   ", strings.Repeat("x", 41)},
	}))

	tags, _, err := f.runner().TagArticles(context.Background(), tagArticles(), nil)

	require.NoError(t, err)
	require.Equal(t, []domain.Tag{"go", "sqlite", "rust"}, tags[URLA])
	require.Empty(t, tags[URLB])
}

func TestTagArticlesSkillExistsForClaude(t *testing.T) {
	require.FileExists(t, claudeTagSkill)
	raw, err := os.ReadFile(claudeTagSkill)
	require.NoError(t, err)
	require.Contains(t, string(raw), "name: tag-articles")
}

func TestTagArticlesSkillExistsForCodex(t *testing.T) {
	require.FileExists(t, codexTagSkill)
	codex, err := filepath.EvalSymlinks(codexTagSkill)
	require.NoError(t, err)
	claude, err := filepath.EvalSymlinks(claudeTagSkill)
	require.NoError(t, err)
	require.Equal(t, claude, codex)
}
