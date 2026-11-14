package domain

import "strings"

type Vote int

const (
	VoteUp   Vote = 1
	VoteDown Vote = -1
)

func ParseVote(reaction string) (Vote, bool) {
	name, _, _ := strings.Cut(reaction, "::skin-tone-")
	switch name {
	case "+1", "thumbsup":
		return VoteUp, true
	case "-1", "thumbsdown":
		return VoteDown, true
	default:
		return 0, false
	}
}

func (v Vote) Reaction() string {
	switch v {
	case VoteUp:
		return "+1"
	case VoteDown:
		return "-1"
	default:
		return ""
	}
}

type ReactionEvent struct {
	ChannelID string
	MessageTS string
	UserID    string
	Vote      Vote
}
