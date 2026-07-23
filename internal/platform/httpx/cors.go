package httpx

import (
	"errors"
	"fmt"
	"net/http"
	"net/netip"
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
		if isPreflight(r) {
			addVary(w.Header(), "Access-Control-Request-Method")
			addVary(w.Header(), "Access-Control-Request-Headers")
		}

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
		origin, err := NormalizeOrigin(value)
		if err != nil {
			return nil, fmt.Errorf("invalid allowed origin %q: %w", value, err)
		}
		origins[origin] = origin
	}
	return origins, nil
}

// NormalizeOrigin validates and canonicalizes one exact HTTP(S) origin.
//
// Hostnames must already be ASCII (internationalized names use their ASCII
// punycode spelling). IPv6 literals must be bracketed and must not have zones.
func NormalizeOrigin(value string) (string, error) {
	if value == "" || value == "*" || strings.Contains(value, ",") {
		return "", errors.New("origin must be one explicit HTTP origin")
	}
	schemeEnd := strings.Index(value, "://")
	if schemeEnd < 0 {
		return "", errors.New("origin must include a scheme")
	}
	scheme := strings.ToLower(value[:schemeEnd])
	if scheme != "http" && scheme != "https" {
		return "", errors.New("origin scheme must be http or https")
	}

	authority := value[schemeEnd+3:]
	if authority == "" || strings.ContainsAny(authority, "/?#@") {
		return "", errors.New("origin must contain only scheme and authority")
	}
	for index := range len(authority) {
		if authority[index] < 0x21 || authority[index] > 0x7e {
			return "", errors.New("origin authority must be printable ASCII")
		}
	}

	host, port, ipv6, err := splitOriginAuthority(authority)
	if err != nil {
		return "", err
	}
	if !ipv6 {
		host, err = normalizeOriginHost(host)
		if err != nil {
			return "", err
		}
	}

	if (scheme == "http" && port == "80") || (scheme == "https" && port == "443") {
		port = ""
	}
	if ipv6 {
		host = "[" + host + "]"
	}
	if port != "" {
		host += ":" + port
	}
	return scheme + "://" + host, nil
}

func splitOriginAuthority(authority string) (host string, port string, ipv6 bool, err error) {
	if strings.HasPrefix(authority, "[") {
		closeBracket := strings.IndexByte(authority, ']')
		if closeBracket < 0 {
			return "", "", false, errors.New("origin IPv6 literal is missing a closing bracket")
		}
		literal := authority[1:closeBracket]
		address, parseErr := netip.ParseAddr(literal)
		if parseErr != nil || !address.Is6() || address.Zone() != "" {
			return "", "", false, errors.New("brackets are permitted only for an unzoned IPv6 literal")
		}
		suffix := authority[closeBracket+1:]
		switch {
		case suffix == "":
		case strings.HasPrefix(suffix, ":"):
			port, err = normalizeOriginPort(suffix[1:])
			if err != nil {
				return "", "", false, err
			}
		default:
			return "", "", false, errors.New("invalid characters after origin IPv6 literal")
		}
		return address.String(), port, true, nil
	}

	if strings.ContainsAny(authority, "[]") {
		return "", "", false, errors.New("origin authority has invalid brackets")
	}
	switch strings.Count(authority, ":") {
	case 0:
		host = authority
	case 1:
		host, port, _ = strings.Cut(authority, ":")
		port, err = normalizeOriginPort(port)
		if err != nil {
			return "", "", false, err
		}
	default:
		return "", "", false, errors.New("origin IPv6 literals must be bracketed")
	}
	if host == "" {
		return "", "", false, errors.New("origin host is empty")
	}
	return host, port, false, nil
}

func normalizeOriginPort(value string) (string, error) {
	if value == "" {
		return "", errors.New("origin has an explicit empty port")
	}
	for index := range len(value) {
		if value[index] < '0' || value[index] > '9' {
			return "", errors.New("origin port must contain only decimal digits")
		}
	}
	number, err := strconv.ParseUint(value, 10, 16)
	if err != nil || number == 0 {
		return "", errors.New("origin port must be between 1 and 65535")
	}
	return strconv.FormatUint(number, 10), nil
}

func normalizeOriginHost(value string) (string, error) {
	host := strings.ToLower(value)
	for index := range len(host) {
		if host[index] > 0x7f {
			return "", errors.New("origin hostname must use ASCII or punycode")
		}
	}

	if address, err := netip.ParseAddr(host); err == nil {
		if !address.Is4() {
			return "", errors.New("origin IPv6 literals must be bracketed")
		}
		return address.String(), nil
	}
	if onlyDecimalAndDots(host) {
		return "", errors.New("origin host resembles an invalid IPv4 address")
	}
	if len(host) > 253 {
		return "", errors.New("origin hostname is longer than 253 bytes")
	}

	labels := strings.Split(host, ".")
	for _, label := range labels {
		if len(label) == 0 {
			return "", errors.New("origin hostname has an empty label")
		}
		if len(label) > 63 {
			return "", errors.New("origin hostname label is longer than 63 bytes")
		}
		if !isASCIILetterOrDigit(label[0]) || !isASCIILetterOrDigit(label[len(label)-1]) {
			return "", errors.New("origin hostname labels must start and end with a letter or digit")
		}
		for index := range len(label) {
			if !isASCIILetterOrDigit(label[index]) && label[index] != '-' {
				return "", errors.New("origin hostname contains an invalid character")
			}
		}
	}
	return host, nil
}

func onlyDecimalAndDots(value string) bool {
	for index := range len(value) {
		if (value[index] < '0' || value[index] > '9') && value[index] != '.' {
			return false
		}
	}
	return true
}

func isASCIILetterOrDigit(value byte) bool {
	return (value >= 'a' && value <= 'z') ||
		(value >= 'A' && value <= 'Z') ||
		(value >= '0' && value <= '9')
}

func requestOrigin(header http.Header) (string, bool, bool) {
	values := header.Values("Origin")
	if len(values) == 0 {
		return "", false, false
	}
	if len(values) != 1 {
		return "", true, false
	}
	origin, err := NormalizeOrigin(values[0])
	if err != nil {
		return "", true, false
	}
	return origin, true, true
}

func addVary(header http.Header, value string) {
	values := make([]string, 0, len(header.Values("Vary"))+1)
	seen := make(map[string]struct{})
	for _, line := range header.Values("Vary") {
		for member := range strings.SplitSeq(line, ",") {
			member = strings.TrimSpace(member)
			if member == "" {
				continue
			}
			canonical := strings.ToLower(member)
			if _, exists := seen[canonical]; exists {
				continue
			}
			seen[canonical] = struct{}{}
			values = append(values, member)
		}
	}
	if _, exists := seen[strings.ToLower(value)]; !exists {
		values = append(values, value)
	}
	header.Del("Vary")
	header.Set("Vary", strings.Join(values, ", "))
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
