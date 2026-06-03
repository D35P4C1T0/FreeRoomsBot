package bot

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"time"
)

// StartHealthServer starts a localhost HTTP health endpoint.
//
// The server responds with HTTP 200 on /healthz until ctx is canceled. It binds
// only to 127.0.0.1 because Docker runs the health-check command inside the
// container network namespace.
func StartHealthServer(ctx context.Context, port int, logger *slog.Logger) (*http.Server, error) {
	server := &http.Server{
		Addr:              fmt.Sprintf("127.0.0.1:%d", port),
		Handler:           newHealthHandler(),
		ReadHeaderTimeout: 2 * time.Second,
	}

	listener, err := net.Listen("tcp", server.Addr)
	if err != nil {
		return nil, err
	}

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			logger.Warn("health server shutdown failed", "error", err)
		}
	}()

	go func() {
		if err := server.Serve(listener); err != nil && err != http.ErrServerClosed {
			logger.Error("health server failed", "error", err)
		}
	}()

	return server, nil
}

func newHealthHandler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, "ok\n")
	})
	return mux
}

// CheckHealth performs the same localhost check used by the Docker
// HEALTHCHECK command.
func CheckHealth(ctx context.Context, port int) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf("http://127.0.0.1:%d/healthz", port), nil)
	if err != nil {
		return err
	}

	client := &http.Client{Timeout: 2 * time.Second}
	res, err := client.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("health endpoint returned %s", res.Status)
	}
	return nil
}
