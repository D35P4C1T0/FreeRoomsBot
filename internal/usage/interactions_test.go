package usage

import (
	"regexp"
	"strings"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/bson"
)

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
	entity := userUsage{
		ID:                "0123456789abcdef",
		FirstSeen:         time.Date(2026, 6, 1, 8, 0, 0, 0, time.UTC),
		LastSeen:          time.Date(2026, 6, 3, 9, 0, 0, 0, time.UTC),
		TotalInteractions: 7,
		Days:              map[string]int64{"2026-06-01": 2},
		Chats: map[string]chatRef{
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
