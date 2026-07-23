package httpx

import (
	"context"
	"net/http"

	"github.com/google/uuid"
)

const (
	requestIDHeader = "X-Request-ID"
	maxRequestIDLen = 128
)

type requestIDContextKey struct{}

// RequestID returns the request ID installed in ctx by WithRequestID.
func RequestID(ctx context.Context) string {
	value, _ := ctx.Value(requestIDContextKey{}).(string)
	return value
}

// WithRequestID preserves one safe inbound request ID or installs a fresh UUID.
func WithRequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := inboundRequestID(r.Header.Values(requestIDHeader))
		if id == "" {
			id = uuid.NewString()
		}

		r.Header.Set(requestIDHeader, id)
		w.Header().Set(requestIDHeader, id)
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), requestIDContextKey{}, id)))
	})
}

func inboundRequestID(values []string) string {
	if len(values) != 1 {
		return ""
	}
	value := values[0]
	if len(value) == 0 || len(value) > maxRequestIDLen {
		return ""
	}
	for index := range len(value) {
		character := value[index]
		if isRequestIDCharacter(character) {
			continue
		}
		return ""
	}
	return value
}

func isRequestIDCharacter(character byte) bool {
	switch {
	case character >= 'a' && character <= 'z':
		return true
	case character >= 'A' && character <= 'Z':
		return true
	case character >= '0' && character <= '9':
		return true
	case character == '-', character == '_', character == '.', character == ':':
		return true
	default:
		return false
	}
}
