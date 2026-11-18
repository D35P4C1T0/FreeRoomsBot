package rooms

import (
	"testing"
	"time"
)

func instant(hour, minute int) time.Time {
	return time.Date(2026, 6, 3, hour, minute, 0, 0, time.UTC)
}

func TestRoomsAt(t *testing.T) {
	dep := &Department{
		Rooms: []*Room{
			{
				Name: "1",
				Lectures: []Lecture{
					{Start: instant(9, 0), End: instant(10, 0)},
					{Start: instant(12, 0), End: instant(13, 0)},
				},
			},
			{
				Name: "2",
				Lectures: []Lecture{
					{Start: instant(8, 0), End: instant(11, 0)},
				},
			},
			{Name: "10"},
		},
	}

	now := instant(10, 30)
	free := dep.AvailabilityAt(now, Free)
	occupied := dep.AvailabilityAt(now, Occupied)
	all := dep.AvailabilityAt(now, All)

	if len(free) != 2 {
		t.Fatalf("free rooms = %d, want 2", len(free))
	}
	if free[0].Name != "10" || free[1].Name != "1" {
		t.Fatalf("free order = %q, %q; want 10, 1", free[0].Name, free[1].Name)
	}
	if !free[1].FreeUntil.Equal(instant(12, 0)) {
		t.Fatalf("room 1 free until %v, want noon", free[1].FreeUntil)
	}

	if len(occupied) != 1 || occupied[0].Name != "2" {
		t.Fatalf("occupied rooms = %#v, want only room 2", occupied)
	}
	if !occupied[0].FreeFrom.Equal(instant(11, 0)) {
		t.Fatalf("room 2 free from %v, want 11:00", occupied[0].FreeFrom)
	}

	gotAll := []string{all[0].Name, all[1].Name, all[2].Name}
	wantAll := []string{"1", "2", "10"}
	for i := range wantAll {
		if gotAll[i] != wantAll[i] {
			t.Fatalf("all order = %#v, want %#v", gotAll, wantAll)
		}
	}
}

func TestAvailabilityAcrossOverlappingAndAdjacentBookings(t *testing.T) {
	room := Room{Lectures: []Lecture{
		{Start: instant(8, 0), End: instant(11, 0)},
		{Start: instant(9, 0), End: instant(10, 0)},
		{Start: instant(11, 0), End: instant(12, 0)},
		{Start: instant(13, 0), End: instant(14, 0)},
	}}
	for _, hour := range []int{8, 10, 11, 12, 14} {
		at := instant(hour, 0)
		got := room.availabilityAt(at)
		wantFree := hour == 12 || hour == 14
		if got.FreeNow != wantFree {
			t.Errorf("at %s: free = %t, want %t", at, got.FreeNow, wantFree)
		}
		if hour < 14 && (!got.FreeFrom.Equal(instant(12, 0)) || !got.FreeUntil.Equal(instant(13, 0))) {
			t.Errorf("at %s: next free interval = %s to %s, want noon to 13:00", at, got.FreeFrom, got.FreeUntil)
		}
	}
}
