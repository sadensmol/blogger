package domain

import (
	"fmt"
	"slices"
	"strings"
)

const (
	notesHeading   = "## My notes"
	learnedHeading = "## Learned"
)

type Profile string

type ProfileChange string

const (
	ProfileInit   ProfileChange = "init"
	ProfileUpdate ProfileChange = "update"
	ProfileNotes  ProfileChange = "notes"
)

type profileSection struct {
	heading string
	body    string
}

func (p Profile) split() (preamble string, sections []profileSection) {
	var preambleBuilder strings.Builder
	var body strings.Builder
	flush := func() {
		if len(sections) > 0 {
			sections[len(sections)-1].body = body.String()
		}
		body.Reset()
	}
	for _, line := range strings.SplitAfter(string(p), "\n") {
		if strings.HasPrefix(line, "## ") {
			flush()
			sections = append(sections, profileSection{heading: strings.TrimSpace(line)})
			continue
		}
		if len(sections) == 0 {
			preambleBuilder.WriteString(line)
			continue
		}
		body.WriteString(line)
	}
	flush()
	return preambleBuilder.String(), sections
}

func indexOfSection(sections []profileSection, heading string) int {
	return slices.IndexFunc(sections, func(s profileSection) bool { return s.heading == heading })
}

func (p Profile) Notes() string {
	_, sections := p.split()
	i := indexOfSection(sections, notesHeading)
	if i < 0 {
		return ""
	}
	return strings.TrimSpace(sections[i].body)
}

func (p Profile) WithNotes(notes string) Profile {
	preamble, sections := p.split()

	notesSection := profileSection{heading: notesHeading, body: fmt.Sprintf("%s\n\n", strings.TrimSpace(notes))}
	learnedIndex := indexOfSection(sections, learnedHeading)
	switch notesIndex := indexOfSection(sections, notesHeading); {
	case notesIndex >= 0:
		sections[notesIndex] = notesSection
	case learnedIndex >= 0:
		sections = slices.Insert(sections, learnedIndex, notesSection)
	default:
		sections = append(sections, notesSection)
	}
	if learnedIndex < 0 {
		sections = append(sections, profileSection{heading: learnedHeading, body: "\n"})
	}

	var b strings.Builder
	b.WriteString(preamble)
	for _, section := range sections {
		b.WriteString(section.heading)
		b.WriteString("\n")
		b.WriteString(section.body)
		if !strings.HasSuffix(section.body, "\n") {
			b.WriteString("\n")
		}
	}
	return Profile(b.String())
}

func (p Profile) WithNotesFrom(current Profile) (Profile, error) {
	_, sections := p.split()
	if indexOfSection(sections, learnedHeading) < 0 {
		return "", fmt.Errorf("profile has no %q section: %w", learnedHeading, ErrBadOutput)
	}
	return p.WithNotes(current.Notes()), nil
}

func (p Profile) LearnedEmpty() bool {
	_, sections := p.split()
	i := indexOfSection(sections, learnedHeading)
	return i < 0 || strings.TrimSpace(sections[i].body) == ""
}
