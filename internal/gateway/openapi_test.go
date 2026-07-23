package gateway

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestOpenAPIIdentityContract(t *testing.T) {
	t.Parallel()

	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("locate test source")
	}
	data, err := os.ReadFile(filepath.Join(filepath.Dir(filename), "..", "..", "api", "openapi.yaml"))
	if err != nil {
		t.Fatalf("read OpenAPI document: %v", err)
	}

	var document map[string]any
	if err := yaml.Unmarshal(data, &document); err != nil {
		t.Fatalf("parse OpenAPI document: %v", err)
	}
	paths := mapValue(t, document, "paths")

	expected := map[string]string{
		"/livez":                       "get",
		"/readyz":                      "get",
		"/api/v1/auth/email/code":      "post",
		"/api/v1/auth/email/login":     "post",
		"/api/v1/auth/wechat/login":    "post",
		"/api/v1/auth/refresh":         "post",
		"/api/v1/auth/logout":          "post",
		"/api/v1/auth/logout-all":      "post",
		"/api/v1/auth/bind/email/code": "post",
		"/api/v1/auth/bind/email":      "post",
		"/api/v1/auth/bind/wechat":     "post",
		"/api/v1/me":                   "get",
	}
	if len(paths) != len(expected) {
		t.Fatalf("OpenAPI paths = %d, want exactly %d", len(paths), len(expected))
	}
	for path, method := range expected {
		pathItem := mapValue(t, paths, path)
		mapValue(t, pathItem, method)
	}
	requiredResponses := map[string][]string{
		"/api/v1/auth/email/code":      {"202", "400", "429", "503"},
		"/api/v1/auth/email/login":     {"200", "400", "403", "409", "429", "503"},
		"/api/v1/auth/wechat/login":    {"200", "400", "403", "409", "429", "503"},
		"/api/v1/auth/refresh":         {"200", "400", "401", "403", "503"},
		"/api/v1/auth/logout":          {"200", "401", "403", "503"},
		"/api/v1/auth/logout-all":      {"200", "401", "403", "503"},
		"/api/v1/auth/bind/email/code": {"202", "400", "401", "403", "429", "503"},
		"/api/v1/auth/bind/email":      {"200", "400", "401", "403", "409", "429", "503"},
		"/api/v1/auth/bind/wechat":     {"200", "400", "401", "403", "409", "429", "503"},
		"/api/v1/me":                   {"200", "401", "403", "503"},
	}
	for path, statuses := range requiredResponses {
		operation := mapValue(t, mapValue(t, paths, path), expected[path])
		responses := mapValue(t, operation, "responses")
		for _, status := range statuses {
			mapValue(t, responses, status)
		}
	}

	protected := []string{
		"/api/v1/auth/logout",
		"/api/v1/auth/logout-all",
		"/api/v1/auth/bind/email/code",
		"/api/v1/auth/bind/email",
		"/api/v1/auth/bind/wechat",
		"/api/v1/me",
	}
	for _, path := range protected {
		operation := mapValue(t, mapValue(t, paths, path), expected[path])
		requireSecurityScheme(t, operation, "bearerAuth")
	}
	refresh := mapValue(t, mapValue(t, paths, "/api/v1/auth/refresh"), "post")
	requireSecurityScheme(t, refresh, "refreshCookie")

	walkErrorResponses(t, document, paths)

	components := mapValue(t, document, "components")
	securitySchemes := mapValue(t, components, "securitySchemes")
	bearer := mapValue(t, securitySchemes, "bearerAuth")
	for key, want := range map[string]string{
		"type": "http", "scheme": "bearer", "bearerFormat": "JWT",
	} {
		if got := bearer[key]; got != want {
			t.Fatalf("bearerAuth.%s = %v, want %q", key, got, want)
		}
	}
	cookie := mapValue(t, securitySchemes, "refreshCookie")
	for key, want := range map[string]string{
		"type": "apiKey", "in": "cookie", "name": "__Secure-agri_refresh",
	} {
		if got := cookie[key]; got != want {
			t.Fatalf("refreshCookie.%s = %v, want %q", key, got, want)
		}
	}
	schemas := mapValue(t, components, "schemas")
	for _, name := range []string{
		"Problem",
		"UserSummary",
		"BoundIdentitySummary",
		"LoginResponse",
		"BindResponse",
		"EmailCodeRequest",
		"EmailLoginRequest",
		"WeChatLoginRequest",
		"RefreshRequest",
		"BindEmailRequest",
		"BindWeChatRequest",
	} {
		mapValue(t, schemas, name)
	}

	clientKind := mapValue(t, schemas, "ClientKind")
	requireStringSet(t, clientKind["enum"], "web", "wechat_mini")

	emailCodeProperties := mapValue(t, mapValue(t, schemas, "EmailCodeRequest"), "properties")
	if got := mapValue(t, emailCodeProperties, "email")["format"]; got != "email" {
		t.Fatalf("EmailCodeRequest.email format = %v, want email", got)
	}
	emailLoginProperties := mapValue(t, mapValue(t, schemas, "EmailLoginRequest"), "properties")
	if got := mapValue(t, emailLoginProperties, "code")["pattern"]; got != "^[0-9]{6}$" {
		t.Fatalf("EmailLoginRequest.code pattern = %v", got)
	}
	bindEmailProperties := mapValue(t, mapValue(t, schemas, "BindEmailRequest"), "properties")
	if got := mapValue(t, bindEmailProperties, "email")["format"]; got != "email" {
		t.Fatalf("BindEmailRequest.email format = %v, want email", got)
	}
	if got := mapValue(t, bindEmailProperties, "code")["pattern"]; got != "^[0-9]{6}$" {
		t.Fatalf("BindEmailRequest.code pattern = %v", got)
	}
	for _, name := range []string{"WeChatLoginRequest", "BindWeChatRequest"} {
		properties := mapValue(t, mapValue(t, schemas, name), "properties")
		code := mapValue(t, properties, "code")
		if code["minLength"] != 1 || code["maxLength"] != 256 {
			t.Fatalf("%s.code length = %v..%v, want 1..256", name, code["minLength"], code["maxLength"])
		}
	}
	for _, name := range []string{"LoginResponse", "BindResponse"} {
		properties := mapValue(t, mapValue(t, schemas, name), "properties")
		expiresIn := mapValue(t, properties, "expires_in")
		if expiresIn["minimum"] != 1 {
			t.Fatalf("%s.expires_in minimum = %v, want 1", name, expiresIn["minimum"])
		}
		refreshDescription := stringValue(t, mapValue(t, properties, "refresh_token"), "description")
		if !strings.Contains(refreshDescription, "wechat_mini") ||
			!strings.Contains(refreshDescription, "Web") ||
			!strings.Contains(strings.ToLower(refreshDescription), "omit") {
			t.Fatalf("%s.refresh_token does not document mini/Web privacy transport", name)
		}
	}
	meDescription := stringValue(t,
		mapValue(t, mapValue(t, paths, "/api/v1/me"), "get"),
		"description",
	)
	for _, privacyTerm := range []string{"masked", "email", "OpenID", "UnionID"} {
		if !strings.Contains(meDescription, privacyTerm) {
			t.Fatalf("/api/v1/me description does not document %q privacy", privacyTerm)
		}
	}
}

func requireSecurityScheme(t *testing.T, operation map[string]any, expected string) {
	t.Helper()
	security, ok := operation["security"].([]any)
	if !ok {
		t.Fatalf("operation security is %T, want a list", operation["security"])
	}
	for _, entry := range security {
		requirement, ok := entry.(map[string]any)
		if !ok {
			continue
		}
		if _, ok := requirement[expected]; ok {
			return
		}
	}
	t.Fatalf("operation does not declare %q security", expected)
}

func walkErrorResponses(t *testing.T, document, paths map[string]any) {
	t.Helper()
	for path, rawPathItem := range paths {
		pathItem, ok := rawPathItem.(map[string]any)
		if !ok {
			t.Fatalf("path %s is %T, want a map", path, rawPathItem)
		}
		for method, rawOperation := range pathItem {
			operation, ok := rawOperation.(map[string]any)
			if !ok {
				continue
			}
			responses := mapValue(t, operation, "responses")
			for status, rawResponse := range responses {
				if !strings.HasPrefix(status, "4") && !strings.HasPrefix(status, "5") {
					continue
				}
				response, ok := rawResponse.(map[string]any)
				if !ok {
					t.Fatalf("%s %s response %s is %T", method, path, status, rawResponse)
				}
				response = resolveLocalReference(t, document, response)
				content := mapValue(t, response, "content")
				problem := mapValue(t, content, "application/problem+json")
				schema := mapValue(t, problem, "schema")
				if got := schema["$ref"]; got != "#/components/schemas/Problem" {
					t.Fatalf("%s %s response %s schema ref = %v, want Problem", method, path, status, got)
				}
			}
		}
	}
}

func resolveLocalReference(t *testing.T, document, value map[string]any) map[string]any {
	t.Helper()
	rawReference, ok := value["$ref"]
	if !ok {
		return value
	}
	reference, ok := rawReference.(string)
	if !ok || !strings.HasPrefix(reference, "#/") {
		t.Fatalf("unsupported reference %v", rawReference)
	}
	current := document
	segments := strings.Split(strings.TrimPrefix(reference, "#/"), "/")
	for index, segment := range segments {
		next := mapValue(t, current, segment)
		if index == len(segments)-1 {
			return next
		}
		current = next
	}
	t.Fatalf("empty reference %q", reference)
	return nil
}

func mapValue(t *testing.T, source map[string]any, key string) map[string]any {
	t.Helper()
	value, ok := source[key]
	if !ok {
		t.Fatalf("missing %q", key)
	}
	result, ok := value.(map[string]any)
	if !ok {
		t.Fatalf("%q is %T, want a map", key, value)
	}
	return result
}

func stringValue(t *testing.T, source map[string]any, key string) string {
	t.Helper()
	value, ok := source[key].(string)
	if !ok {
		t.Fatalf("%q is %T, want a string", key, source[key])
	}
	return value
}

func requireStringSet(t *testing.T, value any, expected ...string) {
	t.Helper()
	items, ok := value.([]any)
	if !ok {
		t.Fatalf("value is %T, want a list", value)
	}
	if len(items) != len(expected) {
		t.Fatalf("list has %d entries, want %d", len(items), len(expected))
	}
	found := make(map[string]bool, len(items))
	for _, item := range items {
		text, ok := item.(string)
		if !ok {
			t.Fatalf("list item is %T, want a string", item)
		}
		found[text] = true
	}
	for _, want := range expected {
		if !found[want] {
			t.Fatalf("list does not contain %q", want)
		}
	}
}
