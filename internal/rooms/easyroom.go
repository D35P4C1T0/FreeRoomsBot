package rooms

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

const easyRoomURL = "https://easyacademy.unitn.it/AgendaStudentiUnitn/rooms_call.php"
const maxEasyRoomResponseBytes = 2 << 20

// Client loads and normalizes room schedules from the UNITN EasyRoom
// endpoint.
type Client struct {
	client   *http.Client
	loc      *time.Location
	endpoint string
}

// NewClient creates a Client configured for the UNITN EasyRoom
// endpoint and the Europe/Rome time zone.
func NewClient() (*Client, error) {
	loc, err := time.LoadLocation("Europe/Rome")
	if err != nil {
		return nil, err
	}
	return &Client{
		client:   &http.Client{Timeout: 30 * time.Second},
		loc:      loc,
		endpoint: easyRoomURL,
	}, nil
}

// Location is the time zone used by the room schedule.
func (s *Client) Location() *time.Location {
	return s.loc
}

type easyRoomPayload struct {
	// AreaRooms maps EasyRoom department IDs to room IDs and names.
	AreaRooms map[string]map[string]struct {
		// RoomName is the upstream display name for a room.
		RoomName string `json:"room_name"`
	} `json:"area_rooms"`
	// Events contains scheduled occupied intervals from EasyRoom.
	Events []struct {
		// From is the start clock in HH:MM format.
		From string `json:"from"`
		// To is the end clock in HH:MM format.
		To string `json:"to"`
		// CodiceAula is the EasyRoom room ID for the event.
		CodiceAula string `json:"CodiceAula"`
	} `json:"events"`
}

// Load fetches room metadata and scheduled lectures for department.
func (s *Client) Load(ctx context.Context, department *Department) ([]*Room, error) {
	today := time.Now().In(s.loc)
	return s.loadRoomsForDate(ctx, department, today)
}

func (s *Client) loadRoomsForDate(ctx context.Context, department *Department, today time.Time) ([]*Room, error) {
	dateString := today.Format("02-01-2006")

	form := url.Values{
		"form-type": {"rooms"},
		"sede":      {department.ID},
		"date":      {dateString},
		"_lang":     {"it"},
	}

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
		roomName, ok := normalizeRoomName(department.Slug, roomsDic[key].RoomName)
		if !ok {
			continue
		}
		rooms = append(rooms, &Room{Key: key, Name: roomName})
	}
	return rooms
}

func (s *Client) parseLectures(payload easyRoomPayload, rooms []*Room, today time.Time) {
	roomByID := make(map[string]*Room, len(rooms))
	for _, room := range rooms {
		roomByID[room.Key] = room
	}

	today = today.In(s.loc)
	for _, item := range payload.Events {
		fromHour, fromMinute := parseClock(item.From)
		toHour, toMinute := parseClock(item.To)
		if toHour == 24 && toMinute == 0 {
			// Keep the displayed end time on the requested day.
			toHour, toMinute = 23, 59
		}

		start := time.Date(today.Year(), today.Month(), today.Day(), fromHour, fromMinute, 0, 0, s.loc)
		end := time.Date(today.Year(), today.Month(), today.Day(), toHour, toMinute, 0, 0, s.loc)
		if room := roomByID[item.CodiceAula]; room != nil {
			room.Lectures = append(room.Lectures, Lecture{Start: start, End: end})
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
	if len(parts) < 2 {
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
