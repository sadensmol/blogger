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
