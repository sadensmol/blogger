package domain_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sadensmol/blogger/internal/domain"
)

func TestParseTagsLowersTrimsAndCollapsesSpace(t *testing.T) {
	tags := domain.ParseTags([]string{"  Go ", "Distributed \t\n  Systems"})

	require.Equal(t, []domain.Tag{"go", "distributed systems"}, tags)
}

func TestParseTagsDropsDuplicatesAndKeepsTheFirstThree(t *testing.T) {
	tags := domain.ParseTags([]string{"Go", "go", " GO ", "SQLite", "Rust", "Zig"})

	require.Equal(t, []domain.Tag{"go", "sqlite", "rust"}, tags)
}

func TestParseTagsKeepsFortyCharactersAndDropsLonger(t *testing.T) {
	fortyRunes := strings.Repeat("ü", 40)
	fortyOneRunes := strings.Repeat("b", 41)

	tags := domain.ParseTags([]string{fortyOneRunes, fortyRunes, "c"})

	require.Equal(t, []domain.Tag{domain.Tag(fortyRunes), "c"}, tags)
}

func TestParseTagsDropsEmptyValuesBeforeTheLimit(t *testing.T) {
	tags := domain.ParseTags([]string{"", "   ", "\t\n", "a", "b", "c"})

	require.Equal(t, []domain.Tag{"a", "b", "c"}, tags)
}
