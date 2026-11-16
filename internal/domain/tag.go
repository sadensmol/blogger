package domain

import (
	"slices"
	"strings"
	"unicode/utf8"
)

const (
	tagsPerArticle = 3
	tagMaxRunes    = 40
)

type Tag string

// ParseTags keeps the first three distinct valid tags, lower-cased with their white space collapsed.
func ParseTags(raw []string) []Tag {
	tags := make([]Tag, 0, tagsPerArticle)
	for _, value := range raw {
		tag := Tag(strings.Join(strings.Fields(strings.ToLower(value)), " "))
		if tag == "" || utf8.RuneCountInString(string(tag)) > tagMaxRunes || slices.Contains(tags, tag) {
			continue
		}
		tags = append(tags, tag)
		if len(tags) == tagsPerArticle {
			break
		}
	}
	return tags
}
