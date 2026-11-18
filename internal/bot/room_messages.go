package bot

import (
	"fmt"
	"html"
	"strings"
	"time"

	"free-classrooms-bot-unitn/internal/rooms"

	tele "gopkg.in/telebot.v3"
)

func (a *App) sendRooms(message *tele.Message, department *rooms.Department, view rooms.View, query *tele.Callback) error {
	a.departMu.RLock()
	dep := *department // Refresh replaces the whole room slice.
	a.departMu.RUnlock()

	if len(dep.Rooms) == 0 {
		if query != nil {
			return a.bot.Respond(query, &tele.CallbackResponse{
				Text:      "❗ Dati aggiornati non disponibili",
				ShowAlert: true,
			})
		}
		_, err := a.bot.Send(message.Chat, "❗ Dati aggiornati non disponibili")
		return err
	}

	rendered := renderRoomsMessage(&dep, view, time.Now().In(a.roomClient.Location()))

	if query != nil {
		if _, err := a.bot.Edit(query.Message, rendered.Text, rendered.Markup, tele.ModeHTML); err != nil {
			if isIgnoredTelegramError(err) {
				return nil
			}
			return err
		}
		if err := a.bot.Respond(query, &tele.CallbackResponse{Text: fmt.Sprintf("Aggiornato alle %s", rendered.NowPretty)}); err != nil {
			if isIgnoredTelegramError(err) {
				return nil
			}
			return err
		}
		return nil
	}

	_, err := a.bot.Send(message.Chat, rendered.Text, rendered.Markup, tele.ModeHTML)
	return err
}

type renderedRoomsMessage struct {
	// Text is the Telegram message body.
	Text string
	// Markup is the inline keyboard attached to the message.
	Markup *tele.ReplyMarkup
	// NowPretty is the formatted timestamp shown to users.
	NowPretty string
}

func renderRoomsMessage(dep *rooms.Department, requested rooms.View, now time.Time) renderedRoomsMessage {
	nowPretty := now.Format("15:04")
	heading, empty := "Situazione aule", ""
	buttons := []tele.Btn{
		{Text: "Libere", Data: "free;" + dep.Slug + ";now"},
		{Text: "Occupate", Data: "free;" + dep.Slug + ";future"},
		{Text: "Tutte le aule", Data: "free;" + dep.Slug + ";all"},
	}
	switch requested {
	case rooms.Free:
		heading, empty = "Aule libere", "❌ Tutte le aule sono occupate."
		buttons[0].Text = "✅ Libere"
	case rooms.Occupied:
		heading, empty = "Aule occupate", "✳️ Tutte le aule sono libere."
		buttons[1].Text = "✅ Occupate"
	default:
		buttons[2].Text = "✅ Tutte le aule"
	}

	var msg strings.Builder
	fmt.Fprintf(&msg, "<strong>%s - %s alle %s</strong>\n\n", strings.ToUpper(dep.Name), heading, nowPretty)
	available := dep.AvailabilityAt(now, requested)
	if len(available) == 0 {
		msg.WriteString(empty)
	}
	for _, room := range available {
		addRoomAvailability(&msg, room, now.Location())
	}

	markup := &tele.ReplyMarkup{}
	markup.Inline(
		markup.Row(buttons[0], buttons[1]),
		markup.Row(buttons[2]),
	)

	return renderedRoomsMessage{
		Text:      msg.String(),
		Markup:    markup,
		NowPretty: nowPretty,
	}
}

func addRoomAvailability(msg *strings.Builder, room rooms.Availability, loc *time.Location) {
	name := html.EscapeString(room.Name)
	switch {
	case room.FreeNow && room.FreeUntil.IsZero():
		fmt.Fprintf(msg, "✳️ <strong>%s</strong>: Libera tutto il giorno\n", name)
	case room.FreeNow:
		fmt.Fprintf(msg, "✳️ <strong>%s</strong>: Libera fino alle %s\n", name, room.FreeUntil.In(loc).Format("15:04"))
	case room.FreeUntil.IsZero():
		fmt.Fprintf(msg, "❌ <strong>%s</strong>: Libera dalle %s in poi\n", name, room.FreeFrom.In(loc).Format("15:04"))
	default:
		fmt.Fprintf(msg, "❌ <strong>%s</strong>: Libera ore %s - %s\n", name, room.FreeFrom.In(loc).Format("15:04"), room.FreeUntil.In(loc).Format("15:04"))
	}
}
