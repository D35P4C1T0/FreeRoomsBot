package bot

import (
	"testing"
	"time"
)

func instant(hour, minute int) time.Time {
	return time.Date(2026, 6, 3, hour, minute, 0, 0, time.UTC)
}

func TestFindFreeRoomsAt(t *testing.T) {
	dep := &Department{
		Rooms: []*Room{
			{
				Name: "1",
				Lectures: []Lecture{
					{Name: "morning", Start: instant(9, 0), End: instant(10, 0)},
					{Name: "late", Start: instant(12, 0), End: instant(13, 0)},
				},
			},
			{
				Name: "2",
				Lectures: []Lecture{
					{Name: "current", Start: instant(8, 0), End: instant(11, 0)},
				},
			},
			{Name: "10"},
		},
	}

	groups := dep.FindFreeRoomsAt(instant(10, 30))

	if len(groups[0].Rooms) != 2 {
		t.Fatalf("free rooms = %d, want 2", len(groups[0].Rooms))
	}
	if groups[0].Rooms[0].Name() != "10" || groups[0].Rooms[1].Name() != "1" {
		t.Fatalf("free order = %q, %q; want 10, 1", groups[0].Rooms[0].Name(), groups[0].Rooms[1].Name())
	}
	if !groups[0].Rooms[1].FreeInterval.End.Equal(instant(12, 0)) {
		t.Fatalf("room 1 free until %v, want noon", groups[0].Rooms[1].FreeInterval.End)
	}

	if len(groups[1].Rooms) != 1 || groups[1].Rooms[0].Name() != "2" {
		t.Fatalf("occupied rooms = %#v, want only room 2", groups[1].Rooms)
	}
	if !groups[1].Rooms[0].FreeInterval.Start.Equal(instant(11, 0)) {
		t.Fatalf("room 2 free from %v, want 11:00", groups[1].Rooms[0].FreeInterval.Start)
	}

	gotAll := []string{groups[2].Rooms[0].Name(), groups[2].Rooms[1].Name(), groups[2].Rooms[2].Name()}
	wantAll := []string{"1", "2", "10"}
	for i := range wantAll {
		if gotAll[i] != wantAll[i] {
			t.Fatalf("all order = %#v, want %#v", gotAll, wantAll)
		}
	}
}
