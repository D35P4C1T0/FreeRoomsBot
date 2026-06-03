package bot

import (
	"strings"
	"testing"
)

func TestIsCommand(t *testing.T) {
	tests := []struct {
		text        string
		command     string
		botUsername string
		want        bool
	}{
		{text: "/start", command: "start", botUsername: "FreeClassroomsBot", want: true},
		{text: "/start payload", command: "start", botUsername: "FreeClassroomsBot", want: true},
		{text: "/start@FreeClassroomsBot", command: "start", botUsername: "FreeClassroomsBot", want: true},
		{text: "/start@FreeClassroomsBot payload", command: "start", botUsername: "FreeClassroomsBot", want: true},
		{text: "/start@OtherBot", command: "start", botUsername: "FreeClassroomsBot", want: false},
		{text: "/aiuto", command: "aiuto", botUsername: "FreeClassroomsBot", want: true},
		{text: "/aiuto@FreeClassroomsBot", command: "aiuto", botUsername: "FreeClassroomsBot", want: true},
	}

	for _, tt := range tests {
		if got := isCommand(tt.text, tt.command, tt.botUsername); got != tt.want {
			t.Fatalf("isCommand(%q, %q, %q) = %v, want %v", tt.text, tt.command, tt.botUsername, got, tt.want)
		}
	}
}

func TestRenderStartMessage(t *testing.T) {
	text := renderStartMessage("Free Classrooms Bot - UNITN", Departments())

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
	text := renderHelpMessage("Free Classrooms Bot - UNITN", Departments())

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
