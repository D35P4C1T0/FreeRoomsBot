package bot

import (
	"errors"
	"strings"

	tele "gopkg.in/telebot.v3"
)

func isIgnoredTelegramError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, tele.ErrMessageNotModified) ||
		errors.Is(err, tele.ErrSameMessageContent) ||
		errors.Is(err, tele.ErrQueryTooOld) {
		return true
	}

	msg := err.Error()
	// Telebot does not expose every Bot API error as a stable sentinel, so keep
	// string fallbacks for common stale callback/edit responses.
	return strings.Contains(msg, "query ID is invalid") ||
		strings.Contains(msg, "message is not modified")
}
