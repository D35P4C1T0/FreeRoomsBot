package bot

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strconv"
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

func localUsageRequest(method, path string) *http.Request {
	r := httptest.NewRequest(method, "http://127.0.0.1:8080"+path, nil)
	r.RemoteAddr = "127.0.0.1:54321"
	return r
}

func TestRecentFrequency(t *testing.T) {
	now := time.Date(2026, 6, 3, 12, 0, 0, 0, time.UTC)
	days := map[string]int64{now.AddDate(0, 0, -60).Format(usageDayLayout): 100}
	if got := recentFrequency(days, now); got != "inactive" {
		t.Fatal(got)
	}
	days[now.Format(usageDayLayout)] = 100
	if got := recentFrequency(days, now); got != "rare" {
		t.Fatal(got)
	}
	for i := 1; i < 12; i++ {
		days[now.AddDate(0, 0, -i).Format(usageDayLayout)] = 1
	}
	if got := recentFrequency(days, now); got != "frequent" {
		t.Fatal(got)
	}
}

func TestUsageViewerSecurity(t *testing.T) {
	handler := newUsageViewerHandler(nil)
	tests := []struct {
		name, method, host, peer, path string
		status                         int
	}{
		{"external peer", "GET", "127.0.0.1:8080", "192.0.2.1:4321", "/usage", 403},
		{"rebinding host", "GET", "evil.example", "127.0.0.1:4321", "/usage", 403},
		{"post", "POST", "127.0.0.1:8080", "127.0.0.1:4321", "/usage", 405},
		{"query injection", "GET", "127.0.0.1:8080", "127.0.0.1:4321", "/usage?q=%24where", 400},
		{"negative page", "GET", "127.0.0.1:8080", "127.0.0.1:4321", "/usage?page=-1", 400},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := localUsageRequest(tt.method, tt.path)
			r.Host = tt.host
			r.RemoteAddr = tt.peer
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, r)
			if w.Code != tt.status {
				t.Fatalf("got %d want %d", w.Code, tt.status)
			}
			if !strings.Contains(w.Header().Get("Content-Security-Policy"), "default-src 'none'") {
				t.Fatal("missing CSP")
			}
		})
	}
}

func TestUsageTemplateEscapesChatTitles(t *testing.T) {
	var out strings.Builder
	page := usagePage{Sort: "recent", Page: 1, Rows: []usageRow{{ID: "1234", Chats: []UsageChatRef{{Title: "<script>alert(1)</script>"}}}}}
	if err := usageTemplate.Execute(&out, page); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out.String(), "<script>") {
		t.Fatal("unescaped script")
	}
	if !strings.Contains(out.String(), "&lt;script&gt;") {
		t.Fatal("missing escaped title")
	}
}

func TestExportUsage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/usage" || r.URL.Query().Get("sort") != "total" || r.URL.Query().Get("page") != "2" {
			t.Errorf("unexpected export URL %s", r.URL)
		}
		w.Write([]byte("<!doctype html><title>Private usage</title>"))
	}))
	defer server.Close()
	parsed, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	port, err := strconv.Atoi(parsed.Port())
	if err != nil {
		t.Fatal(err)
	}
	var out strings.Builder
	if err := ExportUsage(context.Background(), port, "sort=total&page=2", &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "Private usage") {
		t.Fatal("missing exported HTML")
	}
}

func TestExportUsageRejectsRedirect(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "https://example.com", http.StatusFound)
	}))
	defer server.Close()
	parsed, _ := url.Parse(server.URL)
	port, _ := strconv.Atoi(parsed.Port())
	var out strings.Builder
	if err := ExportUsage(context.Background(), port, "", &out); err == nil {
		t.Fatal("redirect accepted")
	}
	if out.Len() != 0 {
		t.Fatal("error response exported")
	}
}
