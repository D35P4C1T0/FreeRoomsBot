package bot

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}

func TestNormalizeRoomName(t *testing.T) {
	tests := []struct {
		name string
		slug string
		in   string
		want string
		ok   bool
	}{
		{name: "povo strips aula code", slug: "povo", in: "Aula A101 - Piano Terra", want: "A101", ok: true},
		{name: "povo keeps aula number", slug: "povo", in: "Aula 7", want: "Aula 7", ok: true},
		{name: "povo skips unrelated", slug: "povo", in: "Laboratorio X", ok: false},
		{name: "mesiano strips aula", slug: "mesiano", in: "Aula 3", want: "3", ok: true},
		{name: "mesiano skips 1R", slug: "mesiano", in: "Aula 1R", ok: false},
		{name: "mesiano keeps biblioteca", slug: "mesiano", in: "Biblioteca centrale", want: "Biblioteca", ok: true},
		{name: "psicologia strips aula", slug: "psicologia", in: "Aula Magna piano 1", want: "Magna", ok: true},
		{name: "psicologia lab", slug: "psicologia", in: "Laboratorio informatico 2", want: "Lab 2", ok: true},
		{name: "sociologia aula", slug: "sociologia", in: "Aula 12", want: "12", ok: true},
		{name: "sociologia skips kessler", slug: "sociologia", in: "Aula Kessler", ok: false},
		{name: "sociologia sala studio", slug: "sociologia", in: "Sala Studio 1", want: "Studio 1", ok: true},
		{name: "lettere aula", slug: "lettere", in: "Aula 001 piano terra", want: "001", ok: true},
		{name: "economia informatica", slug: "economia", in: "Aula informatica 1 - 2", want: "Inf 1 2", ok: true},
		{name: "economia sala", slug: "economia", in: "Sala riunioni - grande", want: "Riunioni grande", ok: true},
		{name: "economia skips studio", slug: "economia", in: "Sala studio 1", ok: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := normalizeRoomName(tt.slug, tt.in)
			if ok != tt.ok {
				t.Fatalf("ok = %v, want %v", ok, tt.ok)
			}
			if got != tt.want {
				t.Fatalf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestParseRoomsFiltersDepartmentRooms(t *testing.T) {
	payload := easyRoomPayload{
		AreaRooms: map[string]map[string]struct {
			RoomName string `json:"room_name"`
		}{
			"E0503": {
				"1": {RoomName: "Aula A101"},
				"2": {RoomName: "Not a room"},
			},
			"E0301": {
				"3": {RoomName: "Aula 5"},
			},
		},
	}

	rooms := parseRooms(payload, &Department{ID: "E0503", Slug: "povo"})
	if len(rooms) != 1 {
		t.Fatalf("len(rooms) = %d, want 1", len(rooms))
	}
	if rooms[0].Key != "1" || rooms[0].Name != "A101" {
		t.Fatalf("room = %#v, want key=1 name=A101", rooms[0])
	}
}

func TestLoadRoomsPostsEasyRoomFormAndParsesRooms(t *testing.T) {
	loc, err := time.LoadLocation("Europe/Rome")
	if err != nil {
		t.Fatal(err)
	}

	sawRequest := false
	client := &http.Client{
		Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			sawRequest = true
			if r.Method != http.MethodPost {
				t.Fatalf("method = %s, want POST", r.Method)
			}
			if got := r.Header.Get("Content-Type"); got != "application/x-www-form-urlencoded" {
				t.Fatalf("content-type = %q, want form", got)
			}
			if err := r.ParseForm(); err != nil {
				t.Fatal(err)
			}
			if got := r.Form.Get("form-type"); got != "rooms" {
				t.Fatalf("form-type = %q, want rooms", got)
			}
			if got := r.Form.Get("sede"); got != "E0503" {
				t.Fatalf("sede = %q, want E0503", got)
			}
			if got := r.Form.Get("_lang"); got != "it" {
				t.Fatalf("_lang = %q, want it", got)
			}
			if got := r.Form.Get("date"); got == "" {
				t.Fatal("date form value is empty")
			}

			return &http.Response{
				StatusCode: http.StatusOK,
				Status:     "200 OK",
				Header:     http.Header{"Content-Type": []string{"application/json"}},
				Body: io.NopCloser(strings.NewReader(`{
			"area_rooms": {
				"E0503": {
					"101": {"room_name": "Aula A101"},
					"bad": {"room_name": "Laboratorio X"}
				}
			},
			"events": [
				{"name": "Lecture", "from": "09:00", "to": "10:00", "CodiceAula": "101"}
			]
		}`)),
			}, nil
		}),
	}

	service := &RoomsService{
		client:   client,
		loc:      loc,
		endpoint: "https://example.invalid/rooms_call.php",
	}

	rooms, err := service.LoadRooms(context.Background(), &Department{ID: "E0503", Slug: "povo"})
	if err != nil {
		t.Fatal(err)
	}
	if !sawRequest {
		t.Fatal("server did not receive request")
	}
	if len(rooms) != 1 {
		t.Fatalf("rooms = %d, want 1", len(rooms))
	}
	if rooms[0].Name != "A101" {
		t.Fatalf("room name = %q, want A101", rooms[0].Name)
	}
	if len(rooms[0].Lectures) != 1 {
		t.Fatalf("lectures = %d, want 1", len(rooms[0].Lectures))
	}
}

func TestLoadRoomsRejectsOversizedEasyRoomResponse(t *testing.T) {
	loc, err := time.LoadLocation("Europe/Rome")
	if err != nil {
		t.Fatal(err)
	}

	service := &RoomsService{
		client: &http.Client{
			Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				return &http.Response{
					StatusCode: http.StatusOK,
					Status:     "200 OK",
					Body:       io.NopCloser(strings.NewReader(strings.Repeat("x", maxEasyRoomResponseBytes+1))),
				}, nil
			}),
		},
		loc:      loc,
		endpoint: "https://example.invalid/rooms_call.php",
	}

	_, err = service.LoadRooms(context.Background(), &Department{ID: "E0503", Slug: "povo"})
	if err == nil {
		t.Fatal("LoadRooms succeeded with oversized response")
	}
	if !strings.Contains(err.Error(), "exceeded") {
		t.Fatalf("error = %q, want exceeded response error", err)
	}
}

func TestLoadRoomsUsesProvidedDateForFormAndLectures(t *testing.T) {
	loc, err := time.LoadLocation("Europe/Rome")
	if err != nil {
		t.Fatal(err)
	}

	requestDate := time.Date(2026, 6, 3, 23, 59, 0, 0, loc)
	service := &RoomsService{
		client: &http.Client{
			Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				if err := r.ParseForm(); err != nil {
					t.Fatal(err)
				}
				if got := r.Form.Get("date"); got != "03-06-2026" {
					t.Fatalf("date = %q, want 03-06-2026", got)
				}

				return &http.Response{
					StatusCode: http.StatusOK,
					Status:     "200 OK",
					Body: io.NopCloser(strings.NewReader(`{
						"area_rooms": {"E0503": {"101": {"room_name": "Aula A101"}}},
						"events": [{"name": "Lecture", "from": "23:00", "to": "24:00", "CodiceAula": "101"}]
					}`)),
				}, nil
			}),
		},
		loc:      loc,
		endpoint: "https://example.invalid/rooms_call.php",
	}

	rooms, err := service.loadRoomsForDate(context.Background(), &Department{ID: "E0503", Slug: "povo"}, requestDate)
	if err != nil {
		t.Fatal(err)
	}
	if len(rooms) != 1 || len(rooms[0].Lectures) != 1 {
		t.Fatalf("rooms = %#v, want one room with one lecture", rooms)
	}
	start := rooms[0].Lectures[0].Start.In(loc)
	if start.Year() != 2026 || start.Month() != time.June || start.Day() != 3 {
		t.Fatalf("lecture start date = %v, want 2026-06-03", start)
	}
}

func TestParseClockAcceptsMidnight24(t *testing.T) {
	hour, minute := parseClock("24:00")
	if hour != 24 || minute != 0 {
		t.Fatalf("parseClock(24:00) = %02d:%02d, want 24:00", hour, minute)
	}
}

func TestParseClockAcceptsSeconds(t *testing.T) {
	hour, minute := parseClock("08:30:00")
	if hour != 8 || minute != 30 {
		t.Fatalf("parseClock(08:30:00) = %02d:%02d, want 08:30", hour, minute)
	}
}

func TestParseLecturesMapsMidnight24To2359(t *testing.T) {
	loc, err := time.LoadLocation("Europe/Rome")
	if err != nil {
		t.Fatal(err)
	}
	service := &RoomsService{loc: loc}
	rooms := []*Room{{Key: "101", Name: "A101"}}
	payload := easyRoomPayload{
		Events: []struct {
			Name       string `json:"name"`
			From       string `json:"from"`
			To         string `json:"to"`
			CodiceAula string `json:"CodiceAula"`
		}{
			{Name: "Holiday", From: "00:00", To: "24:00", CodiceAula: "101"},
		},
	}

	service.parseLectures(payload, rooms, time.Date(2026, 6, 3, 12, 0, 0, 0, loc))
	if len(rooms[0].Lectures) != 1 {
		t.Fatalf("lectures = %d, want 1", len(rooms[0].Lectures))
	}
	end := rooms[0].Lectures[0].End.In(loc)
	if end.Hour() != 23 || end.Minute() != 59 {
		t.Fatalf("lecture end = %02d:%02d, want 23:59", end.Hour(), end.Minute())
	}
}

func TestParseLecturesAcceptsEasyRoomClockWithSeconds(t *testing.T) {
	loc, err := time.LoadLocation("Europe/Rome")
	if err != nil {
		t.Fatal(err)
	}
	service := &RoomsService{loc: loc}
	rooms := []*Room{{Key: "E0503/A101", Name: "A101"}}
	payload := easyRoomPayload{
		Events: []struct {
			Name       string `json:"name"`
			From       string `json:"from"`
			To         string `json:"to"`
			CodiceAula string `json:"CodiceAula"`
		}{
			{Name: "Lecture", From: "08:30:00", To: "10:30:00", CodiceAula: "E0503/A101"},
		},
	}

	service.parseLectures(payload, rooms, time.Date(2026, 6, 3, 12, 0, 0, 0, loc))
	if len(rooms[0].Lectures) != 1 {
		t.Fatalf("lectures = %d, want 1", len(rooms[0].Lectures))
	}
	start := rooms[0].Lectures[0].Start.In(loc)
	end := rooms[0].Lectures[0].End.In(loc)
	if start.Hour() != 8 || start.Minute() != 30 || end.Hour() != 10 || end.Minute() != 30 {
		t.Fatalf("lecture interval = %02d:%02d-%02d:%02d, want 08:30-10:30", start.Hour(), start.Minute(), end.Hour(), end.Minute())
	}
}
