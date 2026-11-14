package controllertext

import (
	"errors"
	"fmt"

	"github.com/sadensmol/blogger/internal/domain"
)

const (
	NoBlogsYet = "No blogs yet."
	BlogsUsage = "usage: list | add <url> | remove <id|url> | pause <id|url> | resume <id|url>"
	Failed     = "Something went wrong. See the logs."
)

func Checking(rawURL string) string {
	return fmt.Sprintf("Checking %s…", rawURL)
}

func SourceAdded(src domain.Source) string {
	return fmt.Sprintf("Added #%d %s (%s).", src.ID, src.Title, src.FeedURL)
}

func SourceRemoved(src domain.Source) string {
	return fmt.Sprintf("Removed #%d %s.", src.ID, src.Title)
}

func SourcePaused(src domain.Source) string {
	return fmt.Sprintf("Paused #%d %s.", src.ID, src.Title)
}

func SourceResumed(src domain.Source) string {
	return fmt.Sprintf("Resumed #%d %s.", src.ID, src.Title)
}

func SourceLine(src domain.Source) string {
	return fmt.Sprintf("#%d %s +%d/-%d %s — %s", src.ID, src.Status, src.Votes.Up, src.Votes.Down, src.Title, src.FeedURL)
}

func AddFailed(err error, rawURL string) (string, bool) {
	switch {
	case errors.Is(err, domain.ErrAlreadyExists):
		return "Already in the list.", true
	case errors.Is(err, domain.ErrNotFound):
		return fmt.Sprintf("No feed found at %s.", rawURL), true
	default:
		return "", false
	}
}

func RefFailed(err error, ref string) (string, bool) {
	if errors.Is(err, domain.ErrNotFound) {
		return fmt.Sprintf("No blog matches %s.", ref), true
	}
	return "", false
}
