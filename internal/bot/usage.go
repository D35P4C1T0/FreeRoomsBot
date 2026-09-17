package bot

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"html"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	tele "gopkg.in/telebot.v3"
)

// UsageChat describes a Telegram chat identity for usage tracking.
//
// Telegram identifiers are never persisted here; they are reduced to a
// pseudonymous internal identifier by pseudonymousUsageID.
type UsageChat struct {
	// ID is the raw Telegram chat or user identifier used only in-memory to
	// derive the pseudonymous identifier.
	ID int64
	// Type is a human-readable chat type such as "Private" or "Group".
	Type string
	// Title is a display label (group title) when present; it stays empty for
	// private users so no personal metadata is stored.
	Title string
}

// UsageChatRef is the persisted, pseudonymous representation of a chat a user
// interacted from.
type UsageChatRef struct {
	// Type is a human-readable chat type such as "Private" or "Group".
	Type string `bson:"Type"`
	// Title is a display label for group chats; empty for private users.
	Title string `bson:"Title"`
	// LastAt is the most recent interaction time observed from this chat.
	LastAt time.Time `bson:"LastAt"`
}

// UsageEntity is the MongoDB representation of a user's interaction statistics.
//
// The document _id is a pseudonymous internal identifier derived from the
// Telegram user ID; no Telegram identifier is stored in this collection.
type UsageEntity struct {
	// ID is the pseudonymous internal user identifier.
	ID string `bson:"_id"`
	// FirstSeen is the first interaction time in UTC.
	FirstSeen time.Time `bson:"FirstSeen"`
	// LastSeen is the most recent interaction time in UTC.
	LastSeen time.Time `bson:"LastSeen"`
	// TotalInteractions counts every observed interaction.
	TotalInteractions int64 `bson:"TotalInteractions"`
	// Days maps a UTC day (2006-01-02) to the interaction count for that day.
	Days map[string]int64 `bson:"Days"`
	// Chats maps pseudonymous chat identifiers to their metadata.
	Chats map[string]UsageChatRef `bson:"Chats"`
}

// usageDayLayout is the UTC day key used in UsageEntity.Days.
const usageDayLayout = "2006-01-02"

// interactionGate bounds how often the same source may trigger a database
// write, so a flood of interactions from one chat cannot flood MongoDB with
// operations.
//
// The gate is used only for derived analytics (log buckets); per-user usage
// statistics stay exact because their storage is bounded by the number of
// distinct users and every upsert is a cheap indexed operation.
type interactionGate struct {
	mu     sync.Mutex
	last   map[int64]time.Time
	minGap time.Duration
}

// newInteractionGate creates a gate allowing at most one write per key every
// minGap.
func newInteractionGate(minGap time.Duration) *interactionGate {
	return &interactionGate{
		last:   make(map[int64]time.Time),
		minGap: minGap,
	}
}

// Allow reports whether a database write for key is permitted now.
//
// Calls within minGap of the previous allowed call for the same key are
// suppressed. The map is reset before it can grow without bound.
func (g *interactionGate) Allow(key int64) bool {
	g.mu.Lock()
	defer g.mu.Unlock()

	now := time.Now()
	if last, ok := g.last[key]; ok && now.Sub(last) < g.minGap {
		return false
	}
	if len(g.last) >= 8192 {
		g.last = make(map[int64]time.Time)
	}
	g.last[key] = now
	return true
}

// pseudonymousUsageID derives a stable pseudonymous internal identifier from a
// Telegram chat or user identifier.
//
// The value is unique per user but cannot be reversed to a Telegram ID without
// knowledge of this function, keeping the usage collection free of Telegram
// identifiers.
func pseudonymousUsageID(telegramID int64) string {
	sum := sha256.Sum256([]byte("usage:v1:" + strconv.FormatInt(telegramID, 10)))
	return hex.EncodeToString(sum[:8])
}

// usageChatLabel returns the display type label for a Telegram chat type.
func usageChatLabel(chatType tele.ChatType) string {
	if chatType == tele.ChatPrivate {
		return "Private"
	}
	return chatTypeString(chatType)
}

// usageChatForMessage builds a UsageChat for a Telegram message chat.
//
// Private chats get no title so no personal metadata is persisted; group-like
// chats keep their public title.
func usageChatForMessage(chat *tele.Chat) UsageChat {
	title := ""
	if chat.Type != tele.ChatPrivate {
		title = chat.Title
	}
	return UsageChat{ID: chat.ID, Type: usageChatLabel(chat.Type), Title: title}
}

// RecordInteraction upserts usage statistics for an interaction.
//
// The primary chat identifies the user (private chat or callback sender).
// Additional chats, typically the group the interaction happened in, are
// recorded as associated rooms. The database operation has its own short
// timeout derived from ctx.
func (d *DatabaseService) RecordInteraction(ctx context.Context, primary UsageChat, extras ...UsageChat) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	now := time.Now().UTC()
	day := now.Format(usageDayLayout)

	update := bson.M{
		"$inc": bson.M{
			"TotalInteractions": 1,
			"Days." + day:       1,
		},
		"$set": bson.M{
			"LastSeen": now,
			fmt.Sprintf("Chats.%s", pseudonymousUsageID(primary.ID)): UsageChatRef{
				Type:   primary.Type,
				Title:  primary.Title,
				LastAt: now,
			},
		},
		"$setOnInsert": bson.M{
			"FirstSeen": now,
		},
	}
	for _, chat := range extras {
		update["$set"].(bson.M)[fmt.Sprintf("Chats.%s", pseudonymousUsageID(chat.ID))] = UsageChatRef{
			Type:   chat.Type,
			Title:  chat.Title,
			LastAt: now,
		}
	}

	_, err := d.usage.UpdateOne(
		ctx,
		bson.M{"_id": pseudonymousUsageID(primary.ID)},
		update,
		options.Update().SetUpsert(true),
	)
	if err != nil && mongo.IsDuplicateKeyError(err) {
		// A concurrent interaction created the same user document first;
		// retry so the interaction count is not lost.
		_, err = d.usage.UpdateOne(ctx, bson.M{"_id": pseudonymousUsageID(primary.ID)}, update)
	}
	return err
}

// UsageSnapshot lists users by descending interaction count.
//
// The result is capped at limit entries; a limit of zero uses the default cap.
// The database operation has its own short timeout derived from ctx.
func (d *DatabaseService) UsageSnapshot(ctx context.Context, limit int64) ([]UsageEntity, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	if limit <= 0 {
		limit = 500
	}
	cursor, err := d.usage.Find(ctx, bson.M{}, options.Find().
		SetSort(bson.D{{Key: "TotalInteractions", Value: -1}}).
		SetLimit(limit))
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var users []UsageEntity
	return users, cursor.All(ctx, &users)
}

// newUsageViewerHandler returns the localhost-only, read-only HTML usage
// viewer backed by db.
func newUsageViewerHandler(db *DatabaseService) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/usage", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		ctx := r.Context()
		users, err := db.UsageSnapshot(ctx, 500)
		if err != nil {
			http.Error(w, "usage query failed", http.StatusInternalServerError)
			return
		}
		now := time.Now().UTC()
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		_ = renderUsagePage(w, users, now)
	})
	return mux
}

// usageFrequencyClass classifies a user's usage frequency from interactions
// per active day. Single-digit interaction counts are always rare because
// there is not yet enough data to infer a habit.
func usageFrequencyClass(total int64, first, last time.Time) string {
	if total <= 0 {
		return "none"
	}
	if total < 3 {
		return "rare"
	}
	spanDays := last.Sub(first).Hours()/24 + 1
	perDay := float64(total) / spanDays
	switch {
	case perDay >= 1:
		return "frequent"
	case perDay >= 1.0/7:
		return "occasional"
	default:
		return "rare"
	}
}

// usageBars renders the last 14 days of activity as relative-height blocks.
func usageBars(days map[string]int64, now time.Time) string {
	if len(days) == 0 {
		return ""
	}
	levels := []rune{'▁', '▂', '▃', '▄', '▅', '▆', '▇', '█'}
	var out strings.Builder
	for i := 13; i >= 0; i-- {
		day := now.AddDate(0, 0, -i).Format(usageDayLayout)
		count := days[day]
		switch {
		case count <= 0:
			out.WriteRune('·')
		case count >= 8:
			out.WriteRune('█')
		default:
			out.WriteRune(levels[count-1])
		}
	}
	return out.String()
}

// renderUsagePage writes the complete viewer HTML page.
func renderUsagePage(w http.ResponseWriter, users []UsageEntity, now time.Time) error {
	var interactions int64
	active7 := 0
	for _, u := range users {
		interactions += u.TotalInteractions
		if now.Sub(u.LastSeen) <= 7*24*time.Hour {
			active7++
		}
	}

	var b strings.Builder
	b.WriteString(`<!doctype html><html><head><meta charset="utf-8">
<title>Usage – Free Classrooms Bot</title><style>
body{font-family:ui-monospace,Menlo,monospace;margin:2rem;color:#eee;background:#111}
table{border-collapse:collapse;width:100%}th,td{padding:.35rem .6rem;border-bottom:1px solid #333;text-align:left;font-size:.85rem}
th{color:#8cf}tr:hover{background:#1c1c1c}code{color:#9f9}
.f-frequent{color:#7fd77f}.f-occasional{color:#e6d47f}.f-rare{color:#d7a27f}.f-none{color:#666}
h1{font-size:1.2rem}.sum{color:#888;font-size:.85rem}
</style></head><body>`)
	capNote := ""
	if len(users) >= 500 {
		capNote = " (top 500 by interactions; all figures cover only those)"
	}
	fmt.Fprintf(&b, `<h1>Bot usage</h1><p class="sum">users: <b>%d</b>%s · interactions: <b>%d</b> · active last 7 days: <b>%d</b> · local admin view (127.0.0.1 only)</p>`,
		len(users), capNote, interactions, active7)

	b.WriteString(`<table><tr><th>#</th><th>Internal ID</th><th>Total</th><th>Frequency</th><th>Activity (14d)</th><th>First seen</th><th>Last seen</th><th>Chats</th></tr>`)
	for i, u := range users {
		class := usageFrequencyClass(u.TotalInteractions, u.FirstSeen, u.LastSeen)
		var chatCells strings.Builder
		for _, c := range u.Chats {
			label := c.Type
			if c.Title != "" {
				label += ": " + c.Title
			}
			chatCells.WriteString(html.EscapeString(label))
			chatCells.WriteString("<br>")
		}
		fmt.Fprintf(&b, `<tr><td>%d</td><td><code>%s</code></td><td>%d</td><td class="f-%s">%s</td><td>%s</td><td>%s</td><td>%s</td><td class="sum">%s</td></tr>`,
			i+1,
			html.EscapeString(u.ID),
			u.TotalInteractions,
			class, class,
			usageBars(u.Days, now),
			u.FirstSeen.Format("2006-01-02"),
			u.LastSeen.Format("2006-01-02 15:04"),
			chatCells.String())
	}
	b.WriteString("</table></body></html>\n")
	_, err := w.Write([]byte(b.String()))
	return err
}
