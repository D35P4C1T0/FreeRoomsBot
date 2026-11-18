package rooms

import (
	"slices"
	"strings"
	"time"
)

// View selects which rooms to include in an availability result.
type View int

const (
	All View = iota
	Free
	Occupied
)

type Availability struct {
	Name                string
	FreeFrom, FreeUntil time.Time
	FreeNow             bool
}

// availabilityAt finds the first free interval that has not ended. Lectures
// are sorted by start time. Keep the latest end across overlapping bookings.
func (r *Room) availabilityAt(now time.Time) Availability {
	result := Availability{Name: r.Name}
	for _, lecture := range r.Lectures {
		if !lecture.Start.Before(lecture.End) {
			continue
		}
		if now.Before(lecture.Start) && lecture.Start.After(result.FreeFrom) {
			result.FreeUntil = lecture.Start
			break
		}
		if lecture.End.After(result.FreeFrom) {
			result.FreeFrom = lecture.End
		}
	}
	result.FreeNow = !now.Before(result.FreeFrom)
	return result
}

// AvailabilityAt returns rooms sorted for the requested view.
func (d *Department) AvailabilityAt(now time.Time, view View) []Availability {
	rooms := make([]Availability, 0, len(d.Rooms))
	for _, room := range d.Rooms {
		availability := room.availabilityAt(now)
		if view == Free && !availability.FreeNow || view == Occupied && availability.FreeNow {
			continue
		}
		rooms = append(rooms, availability)
	}
	slices.SortFunc(rooms, func(a, b Availability) int {
		switch view {
		case Free:
			// Rooms with no more bookings come before rooms with an end time.
			if order := a.FreeUntil.Compare(b.FreeUntil); order != 0 {
				if a.FreeUntil.IsZero() || b.FreeUntil.IsZero() {
					return order
				}
				return -order
			}
		case Occupied:
			return a.FreeFrom.Compare(b.FreeFrom)
		}
		return compareRoomNames(a.Name, b.Name)
	})
	return rooms
}

func compareRoomNames(x, y string) int {
	x = padSingleDigitRoomName(x)
	y = padSingleDigitRoomName(y)
	return strings.Compare(x, y)
}

func padSingleDigitRoomName(s string) string {
	if len(s) > 0 && s[0] >= '1' && s[0] <= '9' && (len(s) == 1 || s[1] == '-') {
		return "0" + s
	}
	return s
}
