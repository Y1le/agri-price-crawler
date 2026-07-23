package httpapi

import (
	"errors"
	"math"
	"net/http"
	"time"
)

const refreshCookiePath = "/api/v1/auth"

// CookiePolicy configures the browser-only refresh-token transport.
type CookiePolicy struct {
	Name           string
	Secure         bool
	AllowedOrigins []string
}

func (h *handler) setRefreshCookie(w http.ResponseWriter, token string, expiresAt time.Time) error {
	maxAge, err := futureSeconds(h.clock.Now().UTC(), expiresAt.UTC())
	if err != nil {
		return err
	}
	http.SetCookie(w, &http.Cookie{
		Name:     h.cookie.Name,
		Value:    token,
		Path:     refreshCookiePath,
		Expires:  expiresAt.UTC(),
		MaxAge:   maxAge,
		HttpOnly: true,
		Secure:   h.cookie.Secure,
		SameSite: http.SameSiteLaxMode,
	})
	return nil
}

func (h *handler) clearRefreshCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     h.cookie.Name,
		Value:    "",
		Path:     refreshCookiePath,
		Expires:  time.Unix(1, 0).UTC(),
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   h.cookie.Secure,
		SameSite: http.SameSiteLaxMode,
	})
}

func namedCookie(r *http.Request, name string) (string, int) {
	var value string
	count := 0
	for _, cookie := range r.Cookies() {
		if cookie.Name != name {
			continue
		}
		count++
		value = cookie.Value
	}
	return value, count
}

func futureSeconds(now, expiresAt time.Time) (int, error) {
	duration := expiresAt.Sub(now)
	if duration <= 0 {
		return 0, errors.New("credential expiry is not in the future")
	}
	seconds := math.Ceil(duration.Seconds())
	if seconds < 1 || seconds > float64(math.MaxInt32) {
		return 0, errors.New("credential expiry is outside the supported range")
	}
	return int(seconds), nil
}

func accessExpiresIn(now, expiresAt time.Time) (int64, error) {
	duration := expiresAt.Sub(now)
	if duration <= 0 {
		return 0, errors.New("access expiry is not in the future")
	}
	seconds := math.Ceil(duration.Seconds())
	if seconds < 1 || seconds > float64(math.MaxInt32) {
		return 0, errors.New("access expiry is outside the supported range")
	}
	return int64(seconds), nil
}
