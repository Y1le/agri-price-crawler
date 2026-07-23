package httpx_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/Y1le/agri-price-crawler/internal/platform/httpx"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestWithRequestIDPreservesOneSafeInboundValue(t *testing.T) {
	t.Parallel()

	const inbound = "edge-01:request_42.example"
	handler := httpx.WithRequestID(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, inbound, httpx.RequestID(r.Context()))
		w.WriteHeader(http.StatusNoContent)
	}))
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request.Header.Set("X-Request-ID", inbound)
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	require.Equal(t, inbound, response.Header().Get("X-Request-ID"))
}

func TestWithRequestIDReplacesAbsentOversizedUnsafeAndDuplicateValues(t *testing.T) {
	t.Parallel()

	tests := map[string]func(*http.Request){
		"absent": func(_ *http.Request) {},
		"oversized": func(r *http.Request) {
			r.Header.Set("X-Request-ID", strings.Repeat("a", 129))
		},
		"whitespace": func(r *http.Request) {
			r.Header.Set("X-Request-ID", "unsafe request id")
		},
		"comma joined": func(r *http.Request) {
			r.Header.Set("X-Request-ID", "first,second")
		},
		"duplicate": func(r *http.Request) {
			r.Header.Add("X-Request-ID", "first")
			r.Header.Add("X-Request-ID", "second")
		},
		"non ASCII": func(r *http.Request) {
			r.Header.Set("X-Request-ID", "请求")
		},
	}

	for name, configure := range tests {
		name, configure := name, configure
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			var fromContext string
			handler := httpx.WithRequestID(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				fromContext = httpx.RequestID(r.Context())
				w.WriteHeader(http.StatusNoContent)
			}))
			request := httptest.NewRequest(http.MethodGet, "/", nil)
			configure(request)
			response := httptest.NewRecorder()

			handler.ServeHTTP(response, request)

			generated := response.Header().Get("X-Request-ID")
			parsed, err := uuid.Parse(generated)
			require.NoError(t, err)
			require.Equal(t, parsed.String(), generated, "generated IDs must use canonical UUID spelling")
			require.Equal(t, generated, fromContext)
			require.Equal(t, generated, request.Header.Get("X-Request-ID"))
		})
	}
}

func TestRequestIDWithoutMiddlewareIsEmpty(t *testing.T) {
	t.Parallel()

	require.Empty(t, httpx.RequestID(context.Background()))
}

func TestWithRequestIDIsConcurrencySafe(t *testing.T) {
	t.Parallel()

	handler := httpx.WithRequestID(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.NotEmpty(t, httpx.RequestID(r.Context()))
		w.WriteHeader(http.StatusNoContent)
	}))

	const requests = 64
	ids := make(chan string, requests)
	var wait sync.WaitGroup
	wait.Add(requests)
	for range requests {
		go func() {
			defer wait.Done()
			request := httptest.NewRequest(http.MethodGet, "/", nil)
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			ids <- response.Header().Get("X-Request-ID")
		}()
	}
	wait.Wait()
	close(ids)

	seen := make(map[string]struct{}, requests)
	for id := range ids {
		_, exists := seen[id]
		require.False(t, exists)
		seen[id] = struct{}{}
	}
}
