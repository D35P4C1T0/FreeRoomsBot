package bot

import (
	"errors"
	"testing"

	tele "gopkg.in/telebot.v3"
)

func TestIsIgnoredTelegramError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{name: "nil", err: nil, want: false},
		{name: "message not modified constant", err: tele.ErrMessageNotModified, want: true},
		{name: "same message constant", err: tele.ErrSameMessageContent, want: true},
		{name: "query too old constant", err: tele.ErrQueryTooOld, want: true},
		{name: "wrapped message", err: errors.New("telegram: message is not modified (400)"), want: true},
		{name: "wrapped query", err: errors.New("telegram: query ID is invalid (400)"), want: true},
		{name: "real error", err: errors.New("telegram: unauthorized (401)"), want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isIgnoredTelegramError(tt.err); got != tt.want {
				t.Fatalf("got %v, want %v", got, tt.want)
			}
		})
	}
}
