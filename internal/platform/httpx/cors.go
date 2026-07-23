package httpx

import (
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
)

var ErrOriginNotAllowed = errors.New("origin_not_allowed")

const (
	allowedMethods = "GET, POST, OPTIONS"
	allowedHeaders = "Authorization, Content-Type, X-Request-ID"
)

var (
	preflightMethods = map[string]struct{}{
		http.MethodGet:     {},
		http.MethodPost:    {},
		http.MethodOptions: {},
	}
	preflightHeaders = map[string]struct{}{
		"Authorization": {},
		"Content-Type":  {},
		"X-Request-Id":  {},
	}
)

// CORS adds credentialed CORS headers for exact configured origins.
func CORS(allowedOrigins []string, next http.Handler) (http.Handler, error) {
	origins, err := normalizeOrigins(allowedOrigins)
	if err != nil {
		return nil, err
	}
	if next == nil {
		return nil, errors.New("CORS next handler is nil")
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		addVary(w.Header(), "Origin")

		requestOrigin, present, valid := requestOrigin(r.Header)
		canonical, matched := origins[requestOrigin]
		if present && valid && matched {
			w.Header().Set("Access-Control-Allow-Origin", canonical)
			w.Header().Set("Access-Control-Allow-Credentials", "true")
		}

		if !isPreflight(r) {
			next.ServeHTTP(w, r)
			return
		}
		if !present || !valid || !matched || !validPreflight(r.Header) {
			w.WriteHeader(http.StatusForbidden)
			return
		}

		w.Header().Set("Access-Control-Allow-Methods", allowedMethods)
		w.Header().Set("Access-Control-Allow-Headers", allowedHeaders)
		w.WriteHeader(http.StatusNoContent)
	}), nil
}

// RequireAllowedOrigin enforces an Origin check when an unsafe request carries
// cookies. It is intended to protect browser credential transports from CSRF.
func RequireAllowedOrigin(r *http.Request, allowedOrigins []string) error {
	if isSafeMethod(r.Method) || !hasCookieCredentials(r.Header) {
		return nil
	}

	origins, err := normalizeOrigins(allowedOrigins)
	if err != nil {
		return fmt.Errorf("normalize allowed origins: %w", err)
	}
	origin, present, valid := requestOrigin(r.Header)
	if !present || !valid {
		return ErrOriginNotAllowed
	}
	if _, ok := origins[origin]; !ok {
		return ErrOriginNotAllowed
	}
	return nil
}

func normalizeOrigins(values []string) (map[string]string, error) {
	origins := make(map[string]string, len(values))
	for _, value := range values {
		origin, err := normalizeOrigin(value)
		if err != nil {
			return nil, fmt.Errorf("invalid allowed origin %q: %w", value, err)
		}
		origins[origin] = origin
	}
	return origins, nil
}

func normalizeOrigin(value string) (string, error) {
	if value == "" || value == "*" || strings.Contains(value, ",") {
		return "", errors.New("origin must be one explicit HTTP origin")
	}

	parsed, err := url.Parse(value)
	if err != nil {
		return "", fmt.Errorf("parse origin: %w", err)
	}
	scheme := strings.ToLower(parsed.Scheme)
	if scheme != "http" && scheme != "https" {
		return "", errors.New("origin scheme must be http or https")
	}
	if parsed.Opaque != "" || parsed.User != nil || parsed.Host == "" {
		return "", errors.New("origin must contain only scheme and host")
	}
	if parsed.RawQuery != "" || parsed.Fragment != "" || (parsed.Path != "" && parsed.Path != "/") {
		return "", errors.New("origin must not contain a path, query, or fragment")
	}

	hostname := strings.ToLower(parsed.Hostname())
	if hostname == "" || strings.ContainsAny(hostname, "*% \t\r\n") {
		return "", errors.New("origin host is invalid")
	}
	port := parsed.Port()
	if port != "" {
		number, err := strconv.ParseUint(port, 10, 16)
		if err != nil || number == 0 {
			return "", errors.New("origin port is invalid")
		}
		port = strconv.FormatUint(number, 10)
	}

	if address, err := netip.ParseAddr(hostname); err == nil {
		if address.Zone() != "" {
			return "", errors.New("origin host must not contain an IPv6 zone")
		}
		address = address.Unmap()
		hostname = address.String()
	}
	if (scheme == "http" && port == "80") || (scheme == "https" && port == "443") {
		port = ""
	}
	if strings.Contains(hostname, ":") {
		hostname = "[" + hostname + "]"
	}
	if port != "" {
		hostname = net.JoinHostPort(strings.Trim(hostname, "[]"), port)
	}
	return scheme + "://" + hostname, nil
}

func requestOrigin(header http.Header) (string, bool, bool) {
	values := header.Values("Origin")
	if len(values) == 0 {
		return "", false, false
	}
	if len(values) != 1 {
		return "", true, false
	}
	origin, err := normalizeOrigin(values[0])
	if err != nil {
		return "", true, false
	}
	return origin, true, true
}

func addVary(header http.Header, value string) {
	for _, line := range header.Values("Vary") {
		for member := range strings.SplitSeq(line, ",") {
			if strings.EqualFold(strings.TrimSpace(member), value) {
				return
			}
		}
	}
	header.Add("Vary", value)
}

func isPreflight(r *http.Request) bool {
	return r.Method == http.MethodOptions && len(r.Header.Values("Access-Control-Request-Method")) > 0
}

func validPreflight(header http.Header) bool {
	methods := header.Values("Access-Control-Request-Method")
	if len(methods) != 1 {
		return false
	}
	if _, ok := preflightMethods[methods[0]]; !ok {
		return false
	}

	for _, line := range header.Values("Access-Control-Request-Headers") {
		for member := range strings.SplitSeq(line, ",") {
			member = strings.TrimSpace(member)
			if member == "" {
				return false
			}
			if _, ok := preflightHeaders[http.CanonicalHeaderKey(member)]; !ok {
				return false
			}
		}
	}
	return true
}

func isSafeMethod(method string) bool {
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodOptions, http.MethodTrace:
		return true
	default:
		return false
	}
}

func hasCookieCredentials(header http.Header) bool {
	for _, value := range header.Values("Cookie") {
		if strings.TrimSpace(value) != "" {
			return true
		}
	}
	return false
}
