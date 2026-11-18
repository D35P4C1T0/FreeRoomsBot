package rooms

import (
	"regexp"
	"strings"
)

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
