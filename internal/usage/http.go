package usage

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
	"time"
)

//go:embed usage.html
var usageHTML string
var usageTemplate = template.Must(template.New("usage").Parse(usageHTML))

func (d *Store) Handler() http.Handler {
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
		p, err := d.usageDashboard(r.Context(), q, time.Now().UTC())
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

// Export reads the container-local dashboard without opening a listener.
func Export(ctx context.Context, port int, query string, out io.Writer) error {
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
