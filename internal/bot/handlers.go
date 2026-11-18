package bot

import (
	"context"
	"strings"

	"free-classrooms-bot-unitn/internal/rooms"
	"free-classrooms-bot-unitn/internal/usage"

	tele "gopkg.in/telebot.v3"
)

type recordInteraction func(context.Context, int64, usage.Chat) error
type trackInteraction func(*tele.User, *tele.Chat)

func (a *App) registerHandlers(ctx context.Context, record recordInteraction) {
	track := func(user *tele.User, chat *tele.Chat) {
		userID := chat.ID
		if user != nil && user.ID != 0 {
			userID = user.ID
		}
		if err := record(ctx, userID, usageChatForMessage(chat)); err != nil {
			a.logger.Warn("record interaction failed", "error", err)
		}
	}

	a.bot.Handle(tele.OnText, func(c tele.Context) error {
		return a.handleText(c.Message(), track)
	})
	a.bot.Handle(tele.OnAddedToGroup, func(c tele.Context) error {
		return a.sendStart(c.Chat().ID)
	})
	a.bot.Handle(tele.OnCallback, func(c tele.Context) error {
		return a.handleCallback(c.Callback(), track)
	})
}

func (a *App) handleCallback(query *tele.Callback, track trackInteraction) error {
	if query.Message == nil || query.Message.Chat == nil {
		return nil
	}
	data := strings.Split(query.Data, ";")
	if len(data) != 3 || data[0] != "free" {
		return nil
	}
	var view rooms.View
	switch data[2] {
	case "now":
		view = rooms.Free
	case "future":
		view = rooms.Occupied
	case "all":
		view = rooms.All
	default:
		return nil
	}
	for _, dep := range a.departments {
		if dep.Slug == data[1] {
			track(query.Sender, query.Message.Chat)
			return a.sendRooms(query.Message, dep, view, query)
		}
	}
	return nil
}

func (a *App) handleText(message *tele.Message, track trackInteraction) error {
	username := a.bot.Me.Username
	text := message.Text
	if message.FromGroup() {
		text = strings.ReplaceAll(text, "@"+username, "")
	}
	text = strings.TrimSpace(text)

	switch {
	case isCommand(text, "start", username):
		track(message.Sender, message.Chat)
		return a.sendStart(message.Chat.ID)
	case isCommand(text, "aiuto", username):
		track(message.Sender, message.Chat)
		return a.sendHelp(message.Chat.ID)
	}
	lookup := strings.ToLower(text)
	for _, dep := range a.departments {
		if strings.Contains(lookup, dep.Slug) {
			track(message.Sender, message.Chat)
			return a.sendRooms(message, dep, rooms.Free, nil)
		}
	}
	return a.sendHelp(message.Chat.ID)
}

func isCommand(text, command, botUsername string) bool {
	base := "/" + command
	if text == base || strings.HasPrefix(text, base+" ") {
		return true
	}
	if botUsername == "" {
		return false
	}
	mentioned := base + "@" + botUsername
	return text == mentioned || strings.HasPrefix(text, mentioned+" ")
}
