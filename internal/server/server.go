// Package server implements the app's HTTP server.
package server

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"time"
)

const (
	greeting = "Hello from Mondoo Engineer!\n"

	// Bounded timeouts protect against slow-client (slowloris) exhaustion.
	readHeaderTimeout = 5 * time.Second
	readTimeout       = 10 * time.Second
	writeTimeout      = 10 * time.Second
	idleTimeout       = 60 * time.Second

	// shutdownTimeout stays below Kubernetes' default 30s termination grace period.
	shutdownTimeout = 10 * time.Second
)

// NewHandler returns the app's routes.
func NewHandler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", writeText(greeting))
	mux.HandleFunc("GET /healthz", writeText("ok\n"))

	return mux
}

func writeText(body string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		// A write error means the client went away; there is nothing to recover.
		_, _ = io.WriteString(w, body)
	}
}

// Server is an HTTP server with graceful shutdown.
type Server struct {
	httpServer *http.Server
	logger     *slog.Logger
}

// New returns a Server that will listen on addr.
func New(addr string, logger *slog.Logger) *Server {
	return &Server{
		httpServer: &http.Server{
			Addr:              addr,
			Handler:           NewHandler(),
			ReadHeaderTimeout: readHeaderTimeout,
			ReadTimeout:       readTimeout,
			WriteTimeout:      writeTimeout,
			IdleTimeout:       idleTimeout,
		},
		logger: logger,
	}
}

// Run listens on the configured address and serves until ctx is cancelled.
func (s *Server) Run(ctx context.Context) error {
	var lc net.ListenConfig

	ln, err := lc.Listen(ctx, "tcp", s.httpServer.Addr)
	if err != nil {
		return fmt.Errorf("listening on %q: %w", s.httpServer.Addr, err)
	}

	return s.Serve(ctx, ln)
}

// Serve accepts connections on ln until ctx is cancelled, then drains
// in-flight requests before returning.
func (s *Server) Serve(ctx context.Context, ln net.Listener) error {
	serveErr := make(chan error, 1)

	go func() { serveErr <- s.httpServer.Serve(ln) }()

	s.logger.Info("listening", "addr", ln.Addr().String())

	select {
	case err := <-serveErr:
		return fmt.Errorf("serving http: %w", err)
	case <-ctx.Done():
	}

	s.logger.Info("shutting down", "timeout", shutdownTimeout.String())

	// ctx is already cancelled; shutdown needs its own deadline.
	shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), shutdownTimeout)
	defer cancel()

	if err := s.httpServer.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("shutting down: %w", err)
	}

	if err := <-serveErr; !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("serving http: %w", err)
	}

	return nil
}
