package bot

import (
	"strings"
	"testing"
	"time"

	"free-classrooms-bot-unitn/internal/rooms"
)

func testDepartmentForRendering() *rooms.Department {
	return &rooms.Department{
		Name: "Povo",
		Slug: "povo",
		Rooms: []*rooms.Room{
			{
				Name: "A101",
				Lectures: []rooms.Lecture{
					{Start: romeTime(9, 0).UTC(), End: romeTime(10, 0).UTC()},
				},
			},
			{
				Name: "B<script>",
			},
		},
	}
}

func TestRenderRoomsMessageFree(t *testing.T) {
	rendered := renderRoomsMessage(testDepartmentForRendering(), rooms.Free, romeTime(8, 30))

	wantParts := []string{
		"<strong>POVO - Aule libere alle 08:30</strong>",
		"✳️ <strong>B&lt;script&gt;</strong>: Libera tutto il giorno",
		"✳️ <strong>A101</strong>: Libera fino alle 09:00",
	}
	for _, want := range wantParts {
		if !strings.Contains(rendered.Text, want) {
			t.Fatalf("rendered text missing %q:\n%s", want, rendered.Text)
		}
	}

	assertButton(t, rendered, 0, 0, "✅ Libere", "free;povo;now")
	assertButton(t, rendered, 0, 1, "Occupate", "free;povo;future")
	assertButton(t, rendered, 1, 0, "Tutte le aule", "free;povo;all")
}

func TestRenderRoomsMessageOccupied(t *testing.T) {
	rendered := renderRoomsMessage(testDepartmentForRendering(), rooms.Occupied, romeTime(9, 30))

	wantParts := []string{
		"<strong>POVO - Aule occupate alle 09:30</strong>",
		"❌ <strong>A101</strong>: Libera dalle 10:00 in poi",
	}
	for _, want := range wantParts {
		if !strings.Contains(rendered.Text, want) {
			t.Fatalf("rendered text missing %q:\n%s", want, rendered.Text)
		}
	}

	assertButton(t, rendered, 0, 0, "Libere", "free;povo;now")
	assertButton(t, rendered, 0, 1, "✅ Occupate", "free;povo;future")
	assertButton(t, rendered, 1, 0, "Tutte le aule", "free;povo;all")
}

func TestRenderRoomsMessageAll(t *testing.T) {
	rendered := renderRoomsMessage(testDepartmentForRendering(), rooms.All, romeTime(11, 0))

	wantParts := []string{
		"<strong>POVO - Situazione aule alle 11:00</strong>",
		"✳️ <strong>A101</strong>: Libera tutto il giorno",
		"✳️ <strong>B&lt;script&gt;</strong>: Libera tutto il giorno",
	}
	for _, want := range wantParts {
		if !strings.Contains(rendered.Text, want) {
			t.Fatalf("rendered text missing %q:\n%s", want, rendered.Text)
		}
	}

	assertButton(t, rendered, 0, 0, "Libere", "free;povo;now")
	assertButton(t, rendered, 0, 1, "Occupate", "free;povo;future")
	assertButton(t, rendered, 1, 0, "✅ Tutte le aule", "free;povo;all")
}

func assertButton(t *testing.T, rendered renderedRoomsMessage, row, col int, text, data string) {
	t.Helper()
	button := rendered.Markup.InlineKeyboard[row][col]
	if button.Text != text || button.Data != data {
		t.Fatalf("button[%d][%d] = (%q, %q), want (%q, %q)", row, col, button.Text, button.Data, text, data)
	}
}

func romeTime(hour, minute int) time.Time {
	loc, err := time.LoadLocation("Europe/Rome")
	if err != nil {
		panic(err)
	}
	return time.Date(2026, 6, 3, hour, minute, 0, 0, loc)
}
