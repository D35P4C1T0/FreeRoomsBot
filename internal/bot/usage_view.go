package bot

import (
	"bytes"
	"context"
	_ "embed"
	"fmt"
	"html/template"
	"io"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo/options"
)

//go:embed usage.html
var usageHTML string
var usageTemplate = template.Must(template.New("usage").Parse(usageHTML))

type usageSummary struct{ Users, Today, Week, Month, Interactions int64 }
type usageBar struct {
	Date   string
	Count  int64
	Height int
}
type usageRow struct {
	ID, Frequency, First, Last, Relative string
	Total                                int64
	Chats                                []UsageChatRef
}
type usagePage struct {
	Summary                                usageSummary
	Bars                                   []usageBar
	Rows                                   []usageRow
	Query, Sort, Previous, Next, Generated string
	Page                                   int
}

type usageQuery struct {
	search, sort string
	page         int64
}

func parseUsageQuery(values url.Values) (usageQuery, error) {
	q := usageQuery{search: strings.ToLower(strings.TrimSpace(values.Get("q"))), sort: values.Get("sort"), page: 1}
	if len(q.search) > 16 || strings.Trim(q.search, "0123456789abcdef") != "" {
		return q, fmt.Errorf("search must be a hexadecimal ID prefix")
	}
	switch q.sort {
	case "":
		q.sort = "recent"
	case "recent", "total", "first":
	default:
		return q, fmt.Errorf("invalid sort")
	}
	if v := values.Get("page"); v != "" {
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil || n < 1 || n > 1000000 {
			return q, fmt.Errorf("invalid page")
		}
		q.page = n
	}
	return q, nil
}

func recentFrequency(days map[string]int64, now time.Time) string {
	active := 0
	for i := 0; i < 30; i++ {
		if days[now.AddDate(0, 0, -i).Format(usageDayLayout)] > 0 {
			active++
		}
	}
	switch {
	case active >= 12:
		return "frequent"
	case active >= 4:
		return "occasional"
	case active > 0:
		return "rare"
	default:
		return "inactive"
	}
}

func relativeUsageTime(last, now time.Time) string {
	age := now.Sub(last)
	switch {
	case age < time.Minute:
		return "just now"
	case age < time.Hour:
		return fmt.Sprintf("%dm ago", int(age.Minutes()))
	case age < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(age.Hours()))
	default:
		return fmt.Sprintf("%dd ago", int(age.Hours()/24))
	}
}

// usageDashboard computes global totals independently of the filtered page.
// Stream only required fields so summary memory does not grow with user count.
func (d *DatabaseService) usageDashboard(ctx context.Context, q usageQuery, now time.Time) (usagePage, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	p := usagePage{Query: q.search, Sort: q.sort, Page: int(q.page), Generated: now.Format("2006-01-02 15:04 UTC")}
	retained := bson.M{"LastSeen": bson.M{"$gt": now.Add(-d.retention)}}
	cursor, err := d.usage.Find(ctx, retained, options.Find().SetProjection(bson.M{"LastSeen": 1, "TotalInteractions": 1, "Days": 1}))
	if err != nil {
		return p, err
	}
	defer cursor.Close(ctx)
	totals := map[string]int64{}
	today := now.Truncate(24 * time.Hour)
	for cursor.Next(ctx) {
		var u UsageEntity
		if err := cursor.Decode(&u); err != nil {
			return p, err
		}
		p.Summary.Users++
		p.Summary.Interactions += u.TotalInteractions
		if !u.LastSeen.Before(today) {
			p.Summary.Today++
		}
		if !u.LastSeen.Before(now.Add(-7 * 24 * time.Hour)) {
			p.Summary.Week++
		}
		if !u.LastSeen.Before(now.Add(-30 * 24 * time.Hour)) {
			p.Summary.Month++
		}
		for day, count := range u.Days {
			if day >= today.AddDate(0, 0, -29).Format(usageDayLayout) && day <= today.Format(usageDayLayout) {
				totals[day] += count
			}
		}
	}
	if err := cursor.Err(); err != nil {
		return p, err
	}
	var max int64 = 1
	for _, count := range totals {
		if count > max {
			max = count
		}
	}
	for i := 29; i >= 0; i-- {
		day := today.AddDate(0, 0, -i).Format(usageDayLayout)
		count := totals[day]
		height := int(float64(count) / float64(max) * 100)
		if count > 0 && height < 2 {
			height = 2
		}
		p.Bars = append(p.Bars, usageBar{day, count, height})
	}
	filter := bson.M{"LastSeen": retained["LastSeen"]}
	if q.search != "" {
		filter["_id"] = bson.M{"$regex": "^" + q.search}
	}
	field := map[string]string{"recent": "LastSeen", "total": "TotalInteractions", "first": "FirstSeen"}[q.sort]
	rows, err := d.usage.Find(ctx, filter, options.Find().SetSort(bson.D{{Key: field, Value: -1}, {Key: "_id", Value: 1}}).SetSkip((q.page-1)*50).SetLimit(51))
	if err != nil {
		return p, err
	}
	defer rows.Close(ctx)
	var users []UsageEntity
	if err := rows.All(ctx, &users); err != nil {
		return p, err
	}
	link := func(page int64) string {
		v := url.Values{"q": {q.search}, "sort": {q.sort}, "page": {strconv.FormatInt(page, 10)}}
		return "/usage?" + v.Encode()
	}
	if q.page > 1 {
		p.Previous = link(q.page - 1)
	}
	if len(users) > 50 {
		p.Next = link(q.page + 1)
		users = users[:50]
	}
	for _, u := range users {
		row := usageRow{ID: u.ID, Total: u.TotalInteractions, Frequency: recentFrequency(u.Days, now), First: u.FirstSeen.UTC().Format("2006-01-02"), Last: u.LastSeen.UTC().Format("2006-01-02 15:04:05 UTC"), Relative: relativeUsageTime(u.LastSeen, now)}
		for _, chat := range u.Chats {
			row.Chats = append(row.Chats, chat)
		}
		sort.Slice(row.Chats, func(i, j int) bool {
			a, b := row.Chats[i], row.Chats[j]
			if a.Type != b.Type {
				return a.Type < b.Type
			}
			if a.Title != b.Title {
				return a.Title < b.Title
			}
			return a.LastAt.Before(b.LastAt)
		})
		p.Rows = append(p.Rows, row)
	}
	return p, nil
}

func newUsageViewerHandler(db *DatabaseService) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; form-action 'self'; frame-ancestors 'none'; base-uri 'none'")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "no-referrer")
		// Reject non-loopback clients and rebinding hosts, even behind a proxy.
		host := r.Host
		if h, _, err := net.SplitHostPort(host); err == nil {
			host = h
		}
		peer, _, err := net.SplitHostPort(r.RemoteAddr)
		if err != nil || !net.ParseIP(peer).IsLoopback() || (host != "127.0.0.1" && host != "localhost" && host != "::1") {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		if r.URL.Path != "/usage" {
			http.NotFound(w, r)
			return
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			http.Error(w, "method not allowed", 405)
			return
		}
		q, err := parseUsageQuery(r.URL.Query())
		if err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		p, err := db.usageDashboard(r.Context(), q, time.Now().UTC())
		if err != nil {
			http.Error(w, "usage query failed", 500)
			return
		}
		var body bytes.Buffer
		if err := usageTemplate.Execute(&body, p); err != nil {
			http.Error(w, "usage render failed", 500)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if r.Method == http.MethodGet {
			_, _ = w.Write(body.Bytes())
		}
	})
}

// ExportUsage reads the container-local dashboard without opening a listener.
func ExportUsage(ctx context.Context, port int, query string, out io.Writer) error {
	values, err := url.ParseQuery(query)
	if err != nil {
		return err
	}
	if _, err := parseUsageQuery(values); err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf("http://127.0.0.1:%d/usage?%s", port, values.Encode()), nil)
	if err != nil {
		return err
	}
	client := &http.Client{Timeout: 10 * time.Second, Transport: &http.Transport{Proxy: nil}, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	defer client.CloseIdleConnections()
	response, err := client.Do(req)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != 200 {
		return fmt.Errorf("usage endpoint returned %s", response.Status)
	}
	_, err = io.Copy(out, response.Body)
	return err
}
