// Package gateway provides the HTTP infrastructure gateway.
package gateway

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"time"
)

const (
	healthJSON  = `{"status":"ok"}`
	problemJSON = `{"type":"about:blank","title":"Service Not Ready","status":503,"code":"service_not_ready"}`
)

// Config configures an HTTP gateway server.
type Config struct {
	Addr   string
	Ready  func(context.Context) error
	Logger *slog.Logger
}

// Server serves infrastructure HTTP endpoints.
type Server struct {
	addr    string
	ready   func(context.Context) error
	logger  *slog.Logger
	handler http.Handler
}

// New creates an HTTP gateway server.
func New(config Config) *Server {
	server := &Server{
		addr:   config.Addr,
		ready:  config.Ready,
		logger: config.Logger,
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /livez", server.livez)
	mux.HandleFunc("GET /readyz", server.readyz)
	server.handler = mux

	return server
}

// Handler returns the gateway's HTTP handler.
func (s *Server) Handler() http.Handler {
	return s.handler
}

// Run listens for HTTP requests until the context is cancelled or serving fails.
func (s *Server) Run(ctx context.Context) error {
	httpServer := &http.Server{
		Addr:              s.addr,
		Handler:           s.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
	}

	listener, err := net.Listen("tcp", s.addr)
	if err != nil {
		return err
	}

	serveErr := make(chan error, 1)
	go func() {
		serveErr <- httpServer.Serve(listener)
	}()

	select {
	case err := <-serveErr:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		return err
	}

	if err := <-serveErr; !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

func (s *Server) livez(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, "application/json", healthJSON)
}

func (s *Server) readyz(w http.ResponseWriter, r *http.Request) {
	if s.ready != nil && s.ready(r.Context()) != nil {
		writeJSON(w, http.StatusServiceUnavailable, "application/problem+json", problemJSON)
		return
	}

	writeJSON(w, http.StatusOK, "application/json", healthJSON)
}

func writeJSON(w http.ResponseWriter, status int, contentType, body string) {
	w.Header().Set("Content-Type", contentType)
	w.WriteHeader(status)
	_, _ = w.Write([]byte(body))
}
