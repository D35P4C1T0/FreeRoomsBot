package bot

import (
	"strings"
	"testing"
	"time"
)

func testDepartmentForRendering() *Department {
	return &Department{
		Name: "Povo",
		Slug: "povo",
		Rooms: []*Room{
			{
				Name: "A101",
				Lectures: []Lecture{
					{Name: "busy", Start: romeTime(9, 0), End: romeTime(10, 0)},
				},
			},
			{
				Name: "B<script>",
			},
		},
	}
}

func TestRenderRoomsMessageFree(t *testing.T) {
	rendered := renderRoomsMessage(testDepartmentForRendering(), AvailabilityFree, romeTime(8, 30))

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
	rendered := renderRoomsMessage(testDepartmentForRendering(), AvailabilityOccupied, romeTime(9, 30))

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
	rendered := renderRoomsMessage(testDepartmentForRendering(), AvailabilityAny, romeTime(11, 0))

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

func TestPrettyLocalTimeUsesRomeTimezone(t *testing.T) {
	utc := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	if got := prettyLocalTime(utc); got != "13:00" {
		t.Fatalf("prettyLocalTime = %q, want 13:00", got)
	}
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
