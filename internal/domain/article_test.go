package domain_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/sadensmol/blogger/internal/domain"
)

func article(source domain.SourceID, day int) domain.Article {
	return domain.Article{
		ID:          domain.ArticleID(int(source)*100 + day),
		SourceID:    source,
		URL:         domain.URL(fmt.Sprintf("https://%d.example.com/%d", source, day)),
		PublishedAt: time.Date(2026, 11, day, 0, 0, 0, 0, time.UTC),
	}
}

func TestCutCandidates(t *testing.T) {
	const blogA, blogB = domain.SourceID(1), domain.SourceID(2)
	var all []domain.Article
	for day := 1; day <= 8; day++ {
		all = append(all, article(blogA, day))
	}
	all = append(all, article(blogB, 3), article(blogB, 9))

	got := domain.CutCandidates(all, 6, 5)

	want := []domain.Article{
		article(blogB, 9), article(blogA, 8), article(blogA, 7), article(blogA, 6), article(blogA, 5), article(blogA, 4),
	}
	require.Equal(t, want, got)
}

func TestSpreadCandidates(t *testing.T) {
	const blogA, blogB = domain.SourceID(1), domain.SourceID(2)
	var all []domain.Article
	for day := 1; day <= 12; day++ {
		all = append(all, article(blogA, day))
	}
	all = append(all, article(blogB, 5), article(blogB, 13))

	got := domain.SpreadCandidates(all, domain.SpreadLimits{Total: 6, NewestPerSource: 2, OlderPerSource: 2})

	want := []domain.Article{
		article(blogB, 13), article(blogA, 12), article(blogA, 11), article(blogA, 8), article(blogB, 5), article(blogA, 3),
	}
	require.Equal(t, want, got)
}

func TestSpreadCandidatesKeepsOlderArticlesUnderTheTotalCut(t *testing.T) {
	const blogA, blogB, blogC = domain.SourceID(1), domain.SourceID(2), domain.SourceID(3)
	var all []domain.Article
	for _, blog := range []domain.SourceID{blogA, blogB, blogC} {
		for day := 1; day <= 10; day++ {
			all = append(all, article(blog, day))
		}
	}

	got := domain.SpreadCandidates(all, domain.SpreadLimits{Total: 6, NewestPerSource: 3, OlderPerSource: 3})

	want := []domain.Article{
		article(blogA, 10), article(blogB, 10), article(blogC, 10), article(blogA, 6), article(blogB, 6), article(blogC, 6),
	}
	require.Equal(t, want, got)
}
