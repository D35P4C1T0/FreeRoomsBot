package bot

import (
	"strings"
	"testing"

	"free-classrooms-bot-unitn/internal/rooms"
)

func TestRenderStartMessage(t *testing.T) {
	text := renderStartMessage("Free Classrooms Bot - UNITN", rooms.Departments())

	wantParts := []string{
		"Ciao! 🤓",
		"Sono *Free Classrooms Bot - UNITN*",
		"/povo",
		"/mesiano",
		"/sociologia",
		"/lettere",
		"/economia",
		"/psicologia",
		"Altre info in /aiuto",
	}
	for _, want := range wantParts {
		if !strings.Contains(text, want) {
			t.Fatalf("start message missing %q:\n%s", want, text)
		}
	}
}

func TestRenderHelpMessage(t *testing.T) {
	text := renderHelpMessage("Free Classrooms Bot - UNITN", rooms.Departments())

	wantParts := []string{
		"*Free Classrooms Bot - UNITN* è il bot",
		"👉 *Usa uno di questi comandi",
		"/povo",
		"Autore: @kirbychan",
	}
	for _, want := range wantParts {
		if !strings.Contains(text, want) {
			t.Fatalf("help message missing %q:\n%s", want, text)
		}
	}

}
