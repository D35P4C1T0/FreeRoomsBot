package bot

import (
	"fmt"
	"html"
	"strings"
	"time"

	tele "gopkg.in/telebot.v3"
)

// SendRooms sends a room availability message or edits an existing callback
// message.
//
// If query is non-nil, SendRooms edits query.Message and answers the callback.
// Telegram "not modified" and expired-query errors are treated as successful
// no-ops because they are common when users tap stale inline buttons.
func (a *App) SendRooms(message *tele.Message, dep *Department, requested AvailabilityType, query *tele.Callback) error {
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

	now := time.Now().In(a.loc)
	rendered := renderRoomsMessage(dep, requested, now)

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

func renderRoomsMessage(dep *Department, requested AvailabilityType, now time.Time) renderedRoomsMessage {
	groups := dep.FindFreeRoomsAt(now)
	var msg strings.Builder
	markup := &tele.ReplyMarkup{}
	buttons := make([]tele.Btn, 3)

	slug := dep.Slug
	prefix := strings.ToUpper(dep.Name)
	nowPretty := prettyLocalTime(now)

	switch requested {
	case AvailabilityFree:
		msg.WriteString("<strong>")
		msg.WriteString(prefix)
		msg.WriteString(" - Aule libere alle ")
		msg.WriteString(nowPretty)
		msg.WriteString("</strong>\n\n")
		if len(groups[0].Rooms) > 0 {
			for _, room := range groups[0].Rooms {
				addRoomAvailability(&msg, room)
			}
		} else {
			msg.WriteString("❌ Tutte le aule sono occupate.")
		}
		buttons[0] = tele.Btn{Text: "✅ Libere", Data: fmt.Sprintf("free;%s;now", slug)}
		buttons[1] = tele.Btn{Text: "Occupate", Data: fmt.Sprintf("free;%s;future", slug)}
		buttons[2] = tele.Btn{Text: "Tutte le aule", Data: fmt.Sprintf("free;%s;all", slug)}
	case AvailabilityOccupied:
		msg.WriteString("<strong>")
		msg.WriteString(prefix)
		msg.WriteString(" - Aule occupate alle ")
		msg.WriteString(nowPretty)
		msg.WriteString("</strong>\n\n")
		if len(groups[1].Rooms) > 0 {
			for _, room := range groups[1].Rooms {
				addRoomAvailability(&msg, room)
			}
		} else {
			msg.WriteString("✳️ Tutte le aule sono libere.")
		}
		buttons[0] = tele.Btn{Text: "Libere", Data: fmt.Sprintf("free;%s;now", slug)}
		buttons[1] = tele.Btn{Text: "✅ Occupate", Data: fmt.Sprintf("free;%s;future", slug)}
		buttons[2] = tele.Btn{Text: "Tutte le aule", Data: fmt.Sprintf("free;%s;all", slug)}
	default:
		msg.WriteString("<strong>")
		msg.WriteString(prefix)
		msg.WriteString(" - Situazione aule alle ")
		msg.WriteString(nowPretty)
		msg.WriteString("</strong>\n\n")
		for _, room := range groups[2].Rooms {
			addRoomAvailability(&msg, room)
		}
		buttons[0] = tele.Btn{Text: "Libere", Data: fmt.Sprintf("free;%s;now", slug)}
		buttons[1] = tele.Btn{Text: "Occupate", Data: fmt.Sprintf("free;%s;future", slug)}
		buttons[2] = tele.Btn{Text: "✅ Tutte le aule", Data: fmt.Sprintf("free;%s;all", slug)}
	}

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

func addRoomAvailability(msg *strings.Builder, room RoomAvailability) {
	// Room names come from an upstream HTML-capable Telegram message, so escape
	// them before rendering with ModeHTML.
	name := html.EscapeString(room.Name())
	if room.IsFreeNow {
		msg.WriteString("✳️ <strong>")
		msg.WriteString(name)
		msg.WriteString("</strong>: ")
		if room.FreeInterval.HasEnd {
			msg.WriteString("Libera fino alle ")
			msg.WriteString(prettyLocalTime(room.FreeInterval.End))
		} else {
			msg.WriteString("Libera tutto il giorno")
		}
	} else {
		msg.WriteString("❌ <strong>")
		msg.WriteString(name)
		msg.WriteString("</strong>: ")
		if room.FreeInterval.HasEnd {
			msg.WriteString("Libera ore ")
			msg.WriteString(prettyLocalTime(room.FreeInterval.Start))
			msg.WriteString(" - ")
			msg.WriteString(prettyLocalTime(room.FreeInterval.End))
		} else {
			msg.WriteString("Libera dalle ")
			msg.WriteString(prettyLocalTime(room.FreeInterval.Start))
			msg.WriteString(" in poi")
		}
	}
	msg.WriteString("\n")
}

func prettyLocalTime(t time.Time) string {
	loc, err := time.LoadLocation("Europe/Rome")
	if err != nil {
		return t.Format("15:04")
	}
	return t.In(loc).Format("15:04")
}
