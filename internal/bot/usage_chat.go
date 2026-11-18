package bot

import (
	"free-classrooms-bot-unitn/internal/usage"

	tele "gopkg.in/telebot.v3"
)

func chatTypeString(chatType tele.ChatType) string {
	switch chatType {
	case tele.ChatPrivate:
		return "Private"
	case tele.ChatGroup:
		return "Group"
	case tele.ChatSuperGroup:
		return "Supergroup"
	case tele.ChatChannel:
		return "Channel"
	default:
		return string(chatType)
	}
}

// usageChatForMessage builds the usage record for a Telegram message chat.
//
// Private chats get no title so no personal metadata is persisted; group-like
// chats keep their public title.
func usageChatForMessage(chat *tele.Chat) usage.Chat {
	title := ""
	if chat.Type != tele.ChatPrivate {
		title = chat.Title
	}
	return usage.Chat{ID: chat.ID, Type: chatTypeString(chat.Type), Title: title}
}
