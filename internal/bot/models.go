package bot

import (
	"sort"
	"strings"
	"time"
)

// AvailabilityType selects which availability group is rendered for a room
// request.
type AvailabilityType int32

const (
	// AvailabilityAny includes every known room for a department.
	AvailabilityAny AvailabilityType = iota
	// AvailabilityFree includes rooms that are free at the requested instant.
	AvailabilityFree
	// AvailabilityOccupied includes rooms that are occupied at the requested
	// instant.
	AvailabilityOccupied
)

// RequestType identifies how a room request reached the bot.
type RequestType int32

const (
	// RequestMessage identifies a room request sent as a Telegram message.
	RequestMessage RequestType = iota
	// RequestCallbackQuery identifies a room request sent through an inline
	// keyboard callback.
	RequestCallbackQuery
)

// Department describes a UNITN campus area and its cached room schedule.
//
// Rooms and UpdatedAt are replaced during refreshes while App.departMu is held.
type Department struct {
	// ID is the EasyRoom department identifier.
	ID string
	// Name is the human-readable department name.
	Name string
	// Slug is the command and callback identifier for the department.
	Slug string
	// Rooms is the current cached room schedule for the department.
	Rooms []*Room
	// UpdatedAt is the EasyRoom date string for the cached Rooms data.
	UpdatedAt string
}

// Room describes a classroom and the lectures scheduled in it for one day.
//
// Lectures are expected to be sorted by Start before availability is computed.
type Room struct {
	// Key is the EasyRoom room identifier used by event payloads.
	Key string
	// Name is the normalized display name shown to Telegram users.
	Name string
	// Lectures contains occupied intervals for the room.
	Lectures []Lecture
}

// Lecture is a scheduled occupied interval for a room.
type Lecture struct {
	// Name is the upstream event name for the scheduled lecture.
	Name string
	// Start is the lecture start time in the room service location.
	Start time.Time
	// End is the lecture end time in the room service location.
	End time.Time
}

// Interval represents a possibly open-ended time range.
//
// When HasStart or HasEnd is false, the corresponding bound is treated as
// unbounded. The end bound is exclusive.
type Interval struct {
	// Start is the lower bound when HasStart is true.
	Start time.Time
	// End is the upper bound when HasEnd is true.
	End time.Time
	// HasStart reports whether Start is meaningful.
	HasStart bool
	// HasEnd reports whether End is meaningful.
	HasEnd bool
}

// Contains reports whether t falls inside i.
func (i Interval) Contains(t time.Time) bool {
	if i.HasStart && t.Before(i.Start) {
		return false
	}
	if i.HasEnd && !t.Before(i.End) {
		return false
	}
	return true
}

// RoomAvailability combines a room with its next free interval and current
// availability state.
type RoomAvailability struct {
	// Room is the room this availability result describes.
	Room *Room
	// FreeInterval is the current or next free interval for Room.
	FreeInterval Interval
	// IsFreeNow reports whether Room is currently available.
	IsFreeNow bool
}

// Name returns the display name of the underlying room.
func (r RoomAvailability) Name() string {
	return r.Room.Name
}

// AvailabilityGroup groups room availability results for one view.
type AvailabilityGroup struct {
	// Availability identifies the view represented by Rooms.
	Availability AvailabilityType
	// Rooms contains room availability results for the view.
	Rooms []RoomAvailability
}

// Departments returns the supported UNITN departments in display order.
//
// The returned Department values are new on each call, but their room caches
// are initially empty.
func Departments() []*Department {
	return []*Department{
		{ID: "E0503", Name: "Povo", Slug: "povo"},
		{ID: "E0301", Name: "Mesiano", Slug: "mesiano"},
		{ID: "E0601", Name: "Sociologia", Slug: "sociologia"},
		{ID: "E0801", Name: "Lettere", Slug: "lettere"},
		{ID: "E0101", Name: "Economia", Slug: "economia"},
		{ID: "E0705", Name: "Psicologia", Slug: "psicologia"},
	}
}

// FindFreeRoomsAt computes free, occupied, and all-room groups for now.
//
// The result always contains groups in AvailabilityFree, AvailabilityOccupied,
// and AvailabilityAny order. It assumes each room's Lectures slice is sorted by
// start time.
func (d *Department) FindFreeRoomsAt(now time.Time) []AvailabilityGroup {
	groups := []AvailabilityGroup{
		{Availability: AvailabilityFree},
		{Availability: AvailabilityOccupied},
		{Availability: AvailabilityAny},
	}

	for _, room := range d.Rooms {
		interval := Interval{}

		if len(room.Lectures) == 0 {
			// Leave interval unbounded: the room is free for the whole day.
		} else if now.Before(room.Lectures[0].Start) {
			interval = Interval{End: room.Lectures[0].Start, HasEnd: true}
		} else if now.After(room.Lectures[len(room.Lectures)-1].End) {
			interval = Interval{Start: room.Lectures[len(room.Lectures)-1].End, HasStart: true}
		} else {
			for i, lecture := range room.Lectures {
				var previous *Lecture
				if i > 0 {
					previous = &room.Lectures[i-1]
				}

				if !now.Before(lecture.Start) && now.Before(lecture.End) {
					for j := i + 1; j < len(room.Lectures); j++ {
						if room.Lectures[j].Start.After(room.Lectures[j-1].End) {
							interval = Interval{
								Start:    room.Lectures[j-1].End,
								End:      room.Lectures[j].Start,
								HasStart: true,
								HasEnd:   true,
							}
							break
						}
					}

					if !interval.HasStart {
						interval = Interval{
							Start:    room.Lectures[len(room.Lectures)-1].End,
							HasStart: true,
						}
					}
					break
				} else if previous != nil && !now.Before(previous.End) && now.Before(lecture.Start) {
					interval = Interval{
						Start:    previous.End,
						End:      lecture.Start,
						HasStart: true,
						HasEnd:   true,
					}
					break
				}
			}
		}

		freeNow := interval.Contains(now)
		availability := RoomAvailability{Room: room, FreeInterval: interval, IsFreeNow: freeNow}
		if freeNow {
			groups[0].Rooms = append(groups[0].Rooms, availability)
		} else {
			groups[1].Rooms = append(groups[1].Rooms, availability)
		}
		groups[2].Rooms = append(groups[2].Rooms, availability)
	}

	sort.Slice(groups[0].Rooms, func(i, j int) bool {
		x, y := groups[0].Rooms[i], groups[0].Rooms[j]
		if (!x.FreeInterval.HasEnd && !y.FreeInterval.HasEnd) ||
			(x.FreeInterval.HasEnd && y.FreeInterval.HasEnd && x.FreeInterval.End.Equal(y.FreeInterval.End)) {
			return compareRoomNames(x.Name(), y.Name()) < 0
		}
		if !x.FreeInterval.HasEnd {
			return true
		}
		if !y.FreeInterval.HasEnd {
			return false
		}
		return y.FreeInterval.End.Before(x.FreeInterval.End)
	})

	sort.Slice(groups[1].Rooms, func(i, j int) bool {
		return groups[1].Rooms[i].FreeInterval.Start.Before(groups[1].Rooms[j].FreeInterval.Start)
	})

	sort.Slice(groups[2].Rooms, func(i, j int) bool {
		return compareRoomNames(groups[2].Rooms[i].Name(), groups[2].Rooms[j].Name()) < 0
	})

	return groups
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
