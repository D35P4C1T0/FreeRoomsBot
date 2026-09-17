package bot

import (
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/bson"
)

func TestInteractionGateSuppressesBursts(t *testing.T) {
	gate := newInteractionGate(50 * time.Millisecond)

	if !gate.Allow(1) {
		t.Fatal("first call for key 1 must be allowed")
	}
	for i := 0; i < 20; i++ {
		if gate.Allow(1) {
			t.Fatalf("call %d within the gap must be suppressed", i)
		}
	}
	if !gate.Allow(2) {
		t.Fatal("a different key must be allowed while key 1 is suppressed")
	}
	time.Sleep(60 * time.Millisecond)
	if !gate.Allow(1) {
		t.Fatal("key 1 must be allowed again after the gap")
	}
}

func TestInteractionGateStaysBounded(t *testing.T) {
	gate := newInteractionGate(time.Hour)

	allowed := 0
	for i := int64(1); i <= 10000; i++ {
		if gate.Allow(i) {
			allowed++
		}
	}
	if allowed != 10000 {
		t.Fatalf("distinct keys must all be allowed, got %d", allowed)
	}
	gate.mu.Lock()
	size := len(gate.last)
	gate.mu.Unlock()
	if size > 8192 {
		t.Fatalf("gate map grew to %d entries, want bounded at 8192", size)
	}
}

func TestPseudonymousUsageIDIsStableAndUnique(t *testing.T) {
	first := pseudonymousUsageID(42)
	again := pseudonymousUsageID(42)
	other := pseudonymousUsageID(43)

	if first != again {
		t.Fatalf("pseudonymousUsageID(42) changed across calls: %q != %q", first, again)
	}
	if first == other {
		t.Fatalf("different telegram IDs produced the same pseudonymous ID %q", first)
	}
	if !regexp.MustCompile("^[0-9a-f]{16}$").MatchString(first) {
		t.Fatalf("pseudonymousUsageID(%d) = %q, want 16 hex characters", 42, first)
	}
}

func TestPseudonymousUsageIDDoesNotContainTelegramID(t *testing.T) {
	// The pseudonymous identifier must not encode the Telegram ID directly.
	id := pseudonymousUsageID(123456789)
	if strings.Contains(id, "75bcd15") || strings.Contains(id, "123456789") {
		t.Fatalf("pseudonymousUsageID leaks the telegram ID: %q", id)
	}
}

func TestUsageEntityBSONFieldNames(t *testing.T) {
	entity := UsageEntity{
		ID:                "0123456789abcdef",
		FirstSeen:         time.Date(2026, 6, 1, 8, 0, 0, 0, time.UTC),
		LastSeen:          time.Date(2026, 6, 3, 9, 0, 0, 0, time.UTC),
		TotalInteractions: 7,
		Days:              map[string]int64{"2026-06-01": 2},
		Chats: map[string]UsageChatRef{
			"fedcba9876543210": {Type: "Group", Title: "Test", LastAt: entityLastAt()},
		},
	}

	raw, err := bson.Marshal(entity)
	if err != nil {
		t.Fatal(err)
	}
	var out bson.M
	if err := bson.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}

	for _, key := range []string{"FirstSeen", "LastSeen", "TotalInteractions", "Days", "Chats"} {
		if _, ok := out[key]; !ok {
			t.Fatalf("missing BSON key %q in %#v", key, out)
		}
	}
}

func entityLastAt() time.Time {
	return time.Date(2026, 6, 3, 9, 30, 0, 0, time.UTC)
}

func TestUsageFrequencyClass(t *testing.T) {
	now := time.Date(2026, 6, 3, 12, 0, 0, 0, time.UTC)
	first := now.AddDate(0, 0, -30)

	tests := []struct {
		name  string
		total int64
		first time.Time
		last  time.Time
		want  string
	}{
		{name: "none", total: 0, first: first, last: now, want: "none"},
		{name: "rare", total: 2, first: first, last: now, want: "rare"},
		{name: "occasional", total: 8, first: first, last: now, want: "occasional"},
		{name: "frequent", total: 60, first: first, last: now, want: "frequent"},
		{name: "single use is rare", total: 1, first: now, last: now, want: "rare"},
	}

	for _, tt := range tests {
		if got := usageFrequencyClass(tt.total, tt.first, tt.last); got != tt.want {
			t.Fatalf("%s: usageFrequencyClass = %q, want %q", tt.name, got, tt.want)
		}
	}
}

func TestUsageBars(t *testing.T) {
	now := time.Date(2026, 6, 3, 12, 0, 0, 0, time.UTC)

	if got := usageBars(nil, now); got != "" {
		t.Fatalf("usageBars(nil) = %q, want empty", got)
	}
	if got := usageBars(map[string]int64{"3000-01-01": 3}, now); got != strings.Repeat("·", 14) {
		t.Fatalf("usageBars(future only) = %q, want 14 empty-day dots", got)
	}

	today := now.Format(usageDayLayout)
	got := usageBars(map[string]int64{today: 2}, now)
	want := strings.Repeat("·", 13) + "▂"
	if got != want {
		t.Fatalf("usageBars(today=2) = %q, want %q", got, want)
	}
}

func TestRenderUsagePage(t *testing.T) {
	now := time.Date(2026, 6, 3, 12, 0, 0, 0, time.UTC)
	users := []UsageEntity{{
		ID:                "0123456789abcdef",
		FirstSeen:         now.AddDate(0, 0, -10),
		LastSeen:          now.Add(-time.Hour),
		TotalInteractions: 11,
		Days:              map[string]int64{now.Format(usageDayLayout): 1},
		Chats: map[string]UsageChatRef{
			"fedcba9876543210": {Type: "Group", Title: "Povo <test>", LastAt: now.Add(-time.Hour)},
		},
	}}

	rec := httptest.NewRecorder()
	if err := renderUsagePage(rec, users, now); err != nil {
		t.Fatal(err)
	}

	body := rec.Body.String()
	for _, want := range []string{
		"0123456789abcdef",
		"users: <b>1</b>",
		"interactions: <b>11</b>",
		"active last 7 days: <b>1</b>",
		"f-frequent",
		"Povo &lt;test&gt;",
		"2026-05-24",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("rendered page missing %q", want)
		}
	}
}

func TestUsageViewerHandlerRejectsUnsupportedMethods(t *testing.T) {
	handler := newUsageViewerHandler(nil)
	for _, method := range []string{"POST", "DELETE"} {
		req := httptest.NewRequest(method, "/usage", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusMethodNotAllowed {
			t.Fatalf("method %s returned status %d, want 405", method, rec.Code)
		}
	}
}
