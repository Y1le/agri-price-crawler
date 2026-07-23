package httpapi

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/Y1le/agri-price-crawler/internal/identity"
	"github.com/google/uuid"
)

type principalContextKey struct{}

func (h *handler) authenticate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token, ok := bearerToken(r.Header)
		if !ok {
			h.writeError(w, r, errorContextAccess, identity.ErrTokenInvalid)
			return
		}

		principal, err := h.tokenParser.ParseAccess(token, h.clock.Now().UTC())
		if err != nil || principal.UserID == uuid.Nil || principal.SessionID == uuid.Nil {
			h.writeError(w, r, errorContextAccess, identity.ErrTokenInvalid)
			return
		}
		ctx := context.WithValue(r.Context(), principalContextKey{}, principal)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func bearerToken(header http.Header) (string, bool) {
	values := header.Values("Authorization")
	if len(values) != 1 {
		return "", false
	}
	value := values[0]
	if value == "" || value[0] == ' ' || strings.ContainsAny(value, ",\t\r\n") {
		return "", false
	}
	separator := strings.IndexByte(value, ' ')
	if separator <= 0 || !strings.EqualFold(value[:separator], "Bearer") {
		return "", false
	}
	remainder := value[separator:]
	token := strings.TrimLeft(remainder, " ")
	if !validToken68(token) {
		return "", false
	}
	return token, true
}

func validToken68(value string) bool {
	if value == "" {
		return false
	}
	padding := false
	payloadCharacters := 0
	for index := range len(value) {
		character := value[index]
		if character == '=' {
			padding = true
			continue
		}
		if padding {
			return false
		}
		payloadCharacters++
		switch {
		case character >= 'a' && character <= 'z':
		case character >= 'A' && character <= 'Z':
		case character >= '0' && character <= '9':
		case strings.ContainsRune("-._~+/", rune(character)):
		default:
			return false
		}
	}
	return payloadCharacters != 0
}

func principalFromContext(ctx context.Context) (identity.Principal, error) {
	principal, ok := ctx.Value(principalContextKey{}).(identity.Principal)
	if !ok || principal.UserID == uuid.Nil || principal.SessionID == uuid.Nil {
		return identity.Principal{}, errors.New("authenticated principal is missing")
	}
	return principal, nil
}

type systemClock struct{}

func (systemClock) Now() time.Time { return time.Now().UTC() }
