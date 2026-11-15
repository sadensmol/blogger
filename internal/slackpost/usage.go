package slackpost

import (
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/slack-go/slack"

	"github.com/sadensmol/blogger/internal/domain"
)

const (
	maxSectionRunes = 3000
	noLLMCallText   = "Tokens used: 0 (no LLM call)"
)

func UsageText(u domain.TokenUsage) string {
	backends := u.Backends()
	if len(backends) == 0 {
		return noLLMCallText
	}
	entries := make([]string, 0, len(backends))
	anyReported := false
	for _, b := range backends {
		if b.Reported == 0 {
			entries = append(entries, fmt.Sprintf("%s: not reported", b.Backend))
			continue
		}
		anyReported = true
		entry := fmt.Sprintf("%s: %s in, %s out", b.Backend, withThousands(b.Input), withThousands(b.Output))
		if b.Unreported > 0 {
			entry = fmt.Sprintf("%s (incomplete)", entry)
		}
		entries = append(entries, entry)
	}
	total := "unknown"
	if anyReported {
		total = withThousands(u.Total())
	}
	return fmt.Sprintf("Tokens used: %s (%s)", total, strings.Join(entries, "; "))
}

func withThousands(n int64) string {
	digits := strconv.FormatInt(n, 10)
	sign := ""
	if n < 0 {
		sign, digits = "-", digits[1:]
	}
	var b strings.Builder
	b.WriteString(sign)
	for i, digit := range digits {
		if i > 0 && (len(digits)-i)%3 == 0 {
			b.WriteByte(',')
		}
		b.WriteRune(digit)
	}
	return b.String()
}

// UsageBlocks shows text in section blocks, then the token line as small text; an empty text gives the token line alone.
func UsageBlocks(text string, u domain.TokenUsage) []slack.Block {
	var blocks []slack.Block
	if text != "" {
		for _, section := range sectionTexts(text) {
			blocks = append(blocks, slack.NewSectionBlock(slack.NewTextBlockObject(slack.MarkdownType, section, false, false), nil, nil))
		}
	}
	return append(blocks, slack.NewContextBlock("", slack.NewTextBlockObject(slack.PlainTextType, UsageText(u), false, false)))
}

// sectionTexts cuts text at line ends into parts that fit a section; only a line longer than a section is cut inside.
func sectionTexts(text string) []string {
	var sections, current []string
	size := 0
	for _, piece := range linePieces(text) {
		n := utf8.RuneCountInString(piece)
		if len(current) > 0 && size+1+n > maxSectionRunes {
			sections = append(sections, strings.Join(current, "\n"))
			current, size = nil, 0
		}
		if len(current) > 0 {
			size++
		}
		current = append(current, piece)
		size += n
	}
	return append(sections, strings.Join(current, "\n"))
}

func linePieces(text string) []string {
	var pieces []string
	for line := range strings.SplitSeq(text, "\n") {
		runes := []rune(line)
		for len(runes) > maxSectionRunes {
			pieces = append(pieces, string(runes[:maxSectionRunes]))
			runes = runes[maxSectionRunes:]
		}
		pieces = append(pieces, string(runes))
	}
	return pieces
}
