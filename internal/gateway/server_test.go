package gateway

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
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
	if body := recorder.Body.String(); body != `{"type":"about:blank","title":"Service Not Ready","status":503,"code":"service_not_ready"}` {
		t.Errorf("body = %q, want %q", body, `{"type":"about:blank","title":"Service Not Ready","status":503,"code":"service_not_ready"}`)
	}
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
