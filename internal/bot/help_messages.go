package bot

import (
	"fmt"
	"strings"

	"free-classrooms-bot-unitn/internal/rooms"

	tele "gopkg.in/telebot.v3"
)

// sendStart sends the introductory Markdown message to chatID.
func (a *App) sendStart(chatID int64) error {
	msg := renderStartMessage(a.name, a.departments)
	_, err := a.bot.Send(tele.ChatID(chatID), msg, tele.ModeMarkdown)
	return err
}

func renderStartMessage(botName string, departments []*rooms.Department) string {
	var msg strings.Builder
	msg.WriteString("Ciao! 🤓\n\n")
	fmt.Fprintf(&msg, "Sono *%s* e ti posso aiutare a trovare le aule libere presso i poli dell'Università di Trento 🎓\n\n", botName)
	msg.WriteString("Usa uno di questi comandi per ottenere la lista delle aule libere:\n\n")
	writeCommands(&msg, departments)
	msg.WriteString("\nAltre info in /aiuto\n")
	return msg.String()
}

// sendHelp sends the help Markdown message to chatID.
func (a *App) sendHelp(chatID int64) error {
	msg := renderHelpMessage(a.name, a.departments)
	_, err := a.bot.Send(tele.ChatID(chatID), msg, tele.ModeMarkdown, tele.NoPreview)
	return err
}

func renderHelpMessage(botName string, departments []*rooms.Department) string {
	var msg strings.Builder
	fmt.Fprintf(&msg, "*%s* è il bot per controllare la disponibilità delle aule presso i poli dell'Università di Trento 🎓\n\n", botName)
	msg.WriteString("👉 *Usa uno di questi comandi per ottenere la lista delle aule libere*\n\n")
	writeCommands(&msg, departments)
	msg.WriteString("\n🤫 Autore: @kirbychan\n")
	return msg.String()
}

func writeCommands(msg *strings.Builder, departments []*rooms.Department) {
	for _, dep := range departments {
		fmt.Fprintln(msg, "/"+dep.Slug)
	}
}
