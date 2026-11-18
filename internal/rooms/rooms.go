// Package rooms loads EasyRoom schedules and computes classroom availability.
package rooms

import "time"

type Department struct {
	ID, Name, Slug string
	Rooms          []*Room
}

type Room struct {
	Key, Name string
	Lectures  []Lecture
}

type Lecture struct {
	Start, End time.Time
}

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
