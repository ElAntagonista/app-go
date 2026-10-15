package server_test

import (
	"context"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ElAntagonista/app-go/internal/server"
)

const greeting = "Hello from Mondoo Engineer!\n"

func TestNewHandler(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		path         string
		expectedBody string
	}{
		{name: "root returns greeting", path: "/", expectedBody: greeting},
		{name: "healthz returns ok", path: "/healthz", expectedBody: "ok\n"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, tt.path, http.NoBody)
			rec := httptest.NewRecorder()

			server.NewHandler().ServeHTTP(rec, req)

			if rec.Code != http.StatusOK || rec.Body.String() != tt.expectedBody {
				t.Errorf("got %d %q, want 200 %q", rec.Code, rec.Body.String(), tt.expectedBody)
			}
		})
	}
}

// TestServer_Serve exercises the real server over TCP: it must answer
// requests and shut down cleanly once its context is cancelled.
func TestServer_Serve(t *testing.T) {
	t.Parallel()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	serveErr := make(chan error, 1)

	go func() {
		serveErr <- server.New(ln.Addr().String(), slog.New(slog.DiscardHandler)).Serve(ctx, ln)
	}()

	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "http://"+ln.Addr().String()+"/", http.NoBody)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}

	resp, err := (&http.Client{Timeout: 2 * time.Second}).Do(req)
	if err != nil {
		t.Fatalf("GET /: %v", err)
	}

	body, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()

	if string(body) != greeting {
		t.Errorf("body = %q, want %q", body, greeting)
	}

	cancel()

	select {
	case err := <-serveErr:
		if err != nil {
			t.Errorf("Serve() after cancel = %v, want nil", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Serve() did not return within 5s of cancellation")
	}
}
