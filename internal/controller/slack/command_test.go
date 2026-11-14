package controllerslack_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	controllerslack "github.com/sadensmol/blogger/internal/controller/slack"
)

func TestParseBlogsCommand(t *testing.T) {
	tests := []struct {
		name, text, action, arg string
	}{
		{name: "empty text asks for help", text: "", action: "help"},
		{name: "list", text: "list", action: "list"},
		{name: "add with a plain url", text: "add https://x.com", action: "add", arg: "https://x.com"},
		{name: "add with a labelled slack link", text: "add <https://x.com|x.com>", action: "add", arg: "https://x.com"},
		{name: "add with a bare slack link", text: "add <https://x.com>", action: "add", arg: "https://x.com"},
		{name: "upper case and extra spaces", text: "  PAUSE   7 ", action: "pause", arg: "7"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd, err := controllerslack.ParseBlogsCommand(tt.text)

			require.NoError(t, err)
			require.Equal(t, controllerslack.BlogsCommand{Action: tt.action, Arg: tt.arg}, cmd)
		})
	}
}

func TestParseBlogsCommandFails(t *testing.T) {
	tests := []struct {
		name, text string
	}{
		{name: "missing argument", text: "remove"},
		{name: "unknown action", text: "foo"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := controllerslack.ParseBlogsCommand(tt.text)

			require.Error(t, err)
		})
	}
}
