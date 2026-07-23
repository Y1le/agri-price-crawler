package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Y1le/agri-price-crawler/internal/platform/httpx"
	"github.com/stretchr/testify/require"
)

func TestHandlerLivezReturnsHealth(t *testing.T) {
	server := New(Config{})
	recorder := httptest.NewRecorder()

	server.Handler().ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/livez", nil))

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}
	if contentType := recorder.Header().Get("Content-Type"); contentType != "application/json" {
		t.Errorf("Content-Type = %q, want %q", contentType, "application/json")
	}
	if body := recorder.Body.String(); body != `{"status":"ok"}` {
		t.Errorf("body = %q, want %q", body, `{"status":"ok"}`)
	}
}

func TestHandlerReadyzReturnsHealthWhenReadyIsNil(t *testing.T) {
	server := New(Config{})
	recorder := httptest.NewRecorder()

	server.Handler().ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/readyz", nil))

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}
	if contentType := recorder.Header().Get("Content-Type"); contentType != "application/json" {
		t.Errorf("Content-Type = %q, want %q", contentType, "application/json")
	}
	if body := recorder.Body.String(); body != `{"status":"ok"}` {
		t.Errorf("body = %q, want %q", body, `{"status":"ok"}`)
	}
}

func TestHandlerReadyzReturnsProblemWhenNotReady(t *testing.T) {
	server := New(Config{
		Ready: func(context.Context) error {
			return errors.New("database is unavailable")
		},
	})
	recorder := httptest.NewRecorder()

	server.Handler().ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/readyz", nil))

	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusServiceUnavailable)
	}
	if contentType := recorder.Header().Get("Content-Type"); contentType != "application/problem+json" {
		t.Errorf("Content-Type = %q, want %q", contentType, "application/problem+json")
	}
	var problem httpx.Problem
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &problem))
	require.Equal(t, http.StatusServiceUnavailable, problem.Status)
	require.Equal(t, "service_not_ready", problem.Code)
	require.NotEmpty(t, problem.TraceID)
}

func TestHandlerMountsAPIAndWrapsWholeMuxWithMiddleware(t *testing.T) {
	var middlewareCalls int
	server := New(Config{
		API: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			require.Equal(t, "/api/v1/example", r.URL.Path)
			w.WriteHeader(http.StatusCreated)
		}),
		Middleware: func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				middlewareCalls++
				w.Header().Set("X-Middleware", "yes")
				next.ServeHTTP(w, r)
			})
		},
	})

	for _, target := range []string{"/api/v1/example", "/livez"} {
		response := httptest.NewRecorder()
		server.Handler().ServeHTTP(response, httptest.NewRequest(http.MethodGet, target, nil))
		require.Equal(t, "yes", response.Header().Get("X-Middleware"))
	}
	require.Equal(t, 2, middlewareCalls)
}

func TestHandlerDoesNotExposeAPIOutsideAPIPrefix(t *testing.T) {
	server := New(Config{API: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		t.Fatal("API must not receive paths outside /api/v1/")
	})})
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/private", nil))
	require.Equal(t, http.StatusNotFound, response.Code)
}

func TestRunShutsDownWhenContextIsCancelled(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := listener.Addr().String()
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	server := New(Config{Addr: addr})
	errs := make(chan error, 1)
	go func() {
		errs <- server.Run(ctx)
	}()

	client := &http.Client{Timeout: 100 * time.Millisecond}
	endpoint := "http://" + addr + "/livez"
	deadline := time.Now().Add(time.Second)
	for {
		response, err := client.Get(endpoint)
		if err == nil {
			response.Body.Close()
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("gateway did not start: %v", err)
		}
		time.Sleep(10 * time.Millisecond)
	}

	cancel()
	select {
	case err := <-errs:
		if err != nil {
			t.Fatalf("Run() error = %v, want nil", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Run() did not return after context cancellation")
	}
}
