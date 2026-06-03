package bot

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

const easyRoomURL = "https://easyacademy.unitn.it/AgendaStudentiUnitn/rooms_call.php"
const maxEasyRoomResponseBytes = 2 << 20

// RoomsService loads and normalizes room schedules from the UNITN EasyRoom
// endpoint.
type RoomsService struct {
	client   *http.Client
	loc      *time.Location
	endpoint string
}

// NewRoomsService creates a RoomsService configured for the UNITN EasyRoom
// endpoint and the Europe/Rome time zone.
func NewRoomsService() (*RoomsService, error) {
	loc, err := time.LoadLocation("Europe/Rome")
	if err != nil {
		return nil, err
	}
	return &RoomsService{
		client:   &http.Client{Timeout: 30 * time.Second},
		loc:      loc,
		endpoint: easyRoomURL,
	}, nil
}

type easyRoomPayload struct {
	// AreaRooms maps EasyRoom department IDs to room IDs and names.
	AreaRooms map[string]map[string]struct {
		// RoomName is the upstream display name for a room.
		RoomName string `json:"room_name"`
	} `json:"area_rooms"`
	// Events contains scheduled occupied intervals from EasyRoom.
	Events []struct {
		// Name is the upstream event name.
		Name string `json:"name"`
		// From is the start clock in HH:MM format.
		From string `json:"from"`
		// To is the end clock in HH:MM format.
		To string `json:"to"`
		// CodiceAula is the EasyRoom room ID for the event.
		CodiceAula string `json:"CodiceAula"`
	} `json:"events"`
}

// LoadRooms fetches room metadata and scheduled lectures for department.
//
// The supplied department must be non-nil. LoadRooms updates the department's
// UpdatedAt field used by the caller's refresh bookkeeping, but returns a new
// room slice instead of publishing it directly.
func (s *RoomsService) LoadRooms(ctx context.Context, department *Department) ([]*Room, error) {
	today := time.Now().In(s.loc)
	return s.loadRoomsForDate(ctx, department, today)
}

func (s *RoomsService) loadRoomsForDate(ctx context.Context, department *Department, today time.Time) ([]*Room, error) {
	dateString := today.Format("02-01-2006")

	if department.UpdatedAt != dateString {
		department.Rooms = nil
		department.UpdatedAt = dateString
	}

	form := url.Values{}
	form.Set("form-type", "rooms")
	form.Set("sede", department.ID)
	form.Set("date", dateString)
	form.Set("_lang", "it")

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	res, err := s.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()

	// EasyRoom responses are small JSON payloads in normal operation. Keep a
	// hard cap so an upstream failure cannot force unbounded memory growth.
	body, err := io.ReadAll(io.LimitReader(res.Body, maxEasyRoomResponseBytes+1))
	if err != nil {
		return nil, err
	}
	if len(body) > maxEasyRoomResponseBytes {
		return nil, fmt.Errorf("easyroom response exceeded %d bytes", maxEasyRoomResponseBytes)
	}
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return nil, fmt.Errorf("easyroom returned %s: %s", res.Status, string(body))
	}

	var payload easyRoomPayload
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, err
	}

	rooms := parseRooms(payload, department)
	s.parseLectures(payload, rooms, today)
	return rooms, nil
}

func parseRooms(payload easyRoomPayload, department *Department) []*Room {
	roomsDic := payload.AreaRooms[department.ID]
	keys := make([]string, 0, len(roomsDic))
	for key := range roomsDic {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	var rooms []*Room
	for _, key := range keys {
		roomName := roomsDic[key].RoomName
		var ok bool
		roomName, ok = normalizeRoomName(department.Slug, roomName)
		if !ok {
			continue
		}
		rooms = append(rooms, &Room{Key: key, Name: roomName})
	}
	return rooms
}

var (
	povoRe       = regexp.MustCompile(`(?:Aula )?([AB]{1}[0-9]{3})`)
	psicologiaRe = regexp.MustCompile(`^Aula ([0-9]{1,2}|Magna)`)
	lettereRe    = regexp.MustCompile(`^Aula ([0-9]{1,3})`)
)

func normalizeRoomName(slug, roomName string) (string, bool) {
	// EasyRoom room names are free-form and differ by department. Keep these
	// rules conservative so non-classroom spaces do not appear as available
	// classrooms.
	switch slug {
	case "povo":
		if match := povoRe.FindStringSubmatch(roomName); len(match) > 1 {
			return match[1], true
		}
		if strings.HasPrefix(roomName, "Aula ") {
			return roomName, true
		}
		return "", false
	case "mesiano":
		if strings.HasPrefix(roomName, "Aula ") {
			roomName = roomName[5:]
			if roomName == "1R" || roomName == "2R" {
				return "", false
			}
			return roomName, true
		}
		if strings.HasPrefix(roomName, "Biblioteca") {
			return "Biblioteca", true
		}
		if roomName == "EALAB" {
			return roomName, true
		}
		return "", false
	case "psicologia":
		if match := psicologiaRe.FindStringSubmatch(roomName); len(match) > 1 {
			return match[1], true
		}
		if strings.HasPrefix(roomName, "Laboratorio informatico ") {
			return "Lab " + roomName[24:], true
		}
		return "", false
	case "sociologia":
		parts := strings.Split(roomName, " ")
		if len(parts) < 2 {
			return "", false
		}
		switch parts[0] {
		case "Aula":
			if parts[1] == "Kessler" {
				return "", false
			}
			return parts[1], true
		case "Laboratorio":
			return "Lab " + parts[1], true
		case "Sala":
			if parts[1] == "Studio" || parts[1] == "Gruppi" {
				return roomName[5:], true
			}
			if parts[1] == "archeologica" {
				return "Archeologica", true
			}
			return "", false
		default:
			return "", false
		}
	case "lettere":
		if match := lettereRe.FindStringSubmatch(roomName); len(match) > 1 {
			return match[1], true
		}
		if strings.HasPrefix(roomName, "Laboratorio m") && len([]rune(roomName)) > 25 {
			return "Lab " + string([]rune(roomName)[25]), true
		}
		return "", false
	case "economia":
		if strings.HasPrefix(roomName, "Aula informatica ") {
			return "Inf " + strings.ToUpper(strings.ReplaceAll(roomName[17:], " - ", " ")), true
		}
		if strings.HasPrefix(roomName, "Aula ") {
			return roomName[5:], true
		}
		if strings.HasPrefix(roomName, "Sala ") {
			if roomName == "Sala corso Nettuno" ||
				roomName == "Sala seminari" ||
				roomName == "Sala Conferenze" ||
				strings.HasPrefix(roomName, "Sala studio") ||
				strings.HasPrefix(roomName, "Sala DEM") {
				return "", false
			}
			runes := []rune(roomName)
			if len(runes) > 5 {
				runes[5] = []rune(strings.ToUpper(string(runes[5])))[0]
			}
			return strings.ReplaceAll(string(runes[5:]), " - ", " "), true
		}
		return "", false
	default:
		return roomName, true
	}
}

func (s *RoomsService) parseLectures(payload easyRoomPayload, rooms []*Room, today time.Time) {
	roomByID := make(map[string]*Room, len(rooms))
	for _, room := range rooms {
		roomByID[room.Key] = room
	}

	today = today.In(s.loc)
	for _, item := range payload.Events {
		fromHour, fromMinute := parseClock(item.From)
		toHour, toMinute := parseClock(item.To)
		if toHour == 24 && toMinute == 0 {
			// EasyRoom uses 24:00 as an end-of-day sentinel. time.Date does not
			// accept hour 24, and 23:59 preserves the intended occupied day.
			toHour, toMinute = 23, 59
		}

		start := time.Date(today.Year(), today.Month(), today.Day(), fromHour, fromMinute, 0, 0, s.loc)
		end := time.Date(today.Year(), today.Month(), today.Day(), toHour, toMinute, 0, 0, s.loc)
		if room := roomByID[item.CodiceAula]; room != nil {
			room.Lectures = append(room.Lectures, Lecture{Name: item.Name, Start: start, End: end})
		}
	}

	for _, room := range rooms {
		sort.Slice(room.Lectures, func(i, j int) bool {
			return room.Lectures[i].Start.Before(room.Lectures[j].Start)
		})
	}
}

func parseClock(value string) (int, int) {
	// Invalid upstream clock values fall back to midnight rather than failing
	// the whole refresh, matching the bot's best-effort refresh behavior.
	parts := strings.Split(value, ":")
	if len(parts) != 2 {
		return 0, 0
	}
	hour, err := strconv.Atoi(parts[0])
	if err != nil {
		return 0, 0
	}
	minute, err := strconv.Atoi(parts[1])
	if err != nil {
		return 0, 0
	}
	return hour, minute
}
