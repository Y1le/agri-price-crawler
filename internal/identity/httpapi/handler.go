// Package httpapi exposes Identity application use cases through net/http.
package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"net/netip"
	"reflect"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/Y1le/agri-price-crawler/internal/identity"
	"github.com/Y1le/agri-price-crawler/internal/platform/httpx"
	"github.com/google/uuid"
)

const maxJSONBodyBytes = 1 << 20

var errBodyTooLarge = errors.New("request body is larger than 1 MiB")

// Service is the narrow Identity use-case surface required by HTTP handlers.
type Service interface {
	RequestEmailLoginCode(context.Context, string, string) error
	LoginEmail(context.Context, string, string, identity.ClientKind) (identity.LoginResult, error)
	LoginWeChat(context.Context, string, string, identity.ClientKind) (identity.LoginResult, error)
	Refresh(context.Context, string, identity.ClientKind) (identity.LoginResult, error)
	Logout(context.Context, identity.Principal) error
	LogoutAll(context.Context, identity.Principal) error
	RequestBindEmailCode(context.Context, identity.Principal, string, string) error
	BindEmail(context.Context, identity.Principal, string, string) (identity.BindResult, error)
	BindWeChat(context.Context, identity.Principal, string, string) (identity.BindResult, error)
	Me(context.Context, identity.Principal) (identity.AccountSummary, error)
}

// TokenParser validates self-contained access tokens at a caller-provided time.
type TokenParser interface {
	ParseAccess(string, time.Time) (identity.Principal, error)
}

// Config contains all dependencies and transport security policy.
type Config struct {
	Service        Service
	TokenParser    TokenParser
	Cookie         CookiePolicy
	TrustedProxies []netip.Prefix
	Clock          identity.Clock
	Logger         *slog.Logger
}

type handler struct {
	service        Service
	tokenParser    TokenParser
	cookie         CookiePolicy
	trustedProxies []netip.Prefix
	clock          identity.Clock
	logger         *slog.Logger
}

// New validates its immutable dependencies and registers exactly the public
// Identity route set on a private ServeMux.
func New(config Config) http.Handler {
	if nilLike(config.Service) {
		panic("identity httpapi: Service is required")
	}
	if nilLike(config.TokenParser) {
		panic("identity httpapi: TokenParser is required")
	}
	if (&http.Cookie{Name: config.Cookie.Name, Value: "token"}).String() == "" {
		panic("identity httpapi: Cookie.Name is invalid")
	}
	for _, origin := range config.Cookie.AllowedOrigins {
		if _, err := httpx.NormalizeOrigin(origin); err != nil {
			panic("identity httpapi: Cookie.AllowedOrigins contains an invalid origin")
		}
	}
	clock := config.Clock
	if nilLike(clock) {
		clock = systemClock{}
	}
	logger := config.Logger
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	h := &handler{
		service:     config.Service,
		tokenParser: config.TokenParser,
		cookie: CookiePolicy{
			Name:           config.Cookie.Name,
			Secure:         config.Cookie.Secure,
			AllowedOrigins: append([]string(nil), config.Cookie.AllowedOrigins...),
		},
		trustedProxies: append([]netip.Prefix(nil), config.TrustedProxies...),
		clock:          clock,
		logger:         logger,
	}

	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/v1/auth/email/code", h.requestEmailCode)
	mux.HandleFunc("POST /api/v1/auth/email/login", h.emailLogin)
	mux.HandleFunc("POST /api/v1/auth/wechat/login", h.wechatLogin)
	mux.HandleFunc("POST /api/v1/auth/refresh", h.refresh)
	mux.Handle("POST /api/v1/auth/logout", h.authenticate(http.HandlerFunc(h.logout)))
	mux.Handle("POST /api/v1/auth/logout-all", h.authenticate(http.HandlerFunc(h.logoutAll)))
	mux.Handle("POST /api/v1/auth/bind/email/code", h.authenticate(http.HandlerFunc(h.requestBindEmailCode)))
	mux.Handle("POST /api/v1/auth/bind/email", h.authenticate(http.HandlerFunc(h.bindEmail)))
	mux.Handle("POST /api/v1/auth/bind/wechat", h.authenticate(http.HandlerFunc(h.bindWeChat)))
	mux.Handle("GET /api/v1/me", h.authenticate(http.HandlerFunc(h.me)))
	return mux
}

type emailRequest struct {
	Email string `json:"email"`
}

type emailLoginRequest struct {
	Email      string              `json:"email"`
	Code       string              `json:"code"`
	ClientKind identity.ClientKind `json:"client_kind"`
}

type weChatLoginRequest struct {
	Code       string              `json:"code"`
	ClientKind identity.ClientKind `json:"client_kind"`
}

type refreshRequest struct {
	RefreshToken        string
	RefreshTokenPresent bool
	RefreshTokenString  bool
}

func (request *refreshRequest) UnmarshalJSON(data []byte) error {
	request.RefreshToken = ""
	request.RefreshTokenPresent = false
	request.RefreshTokenString = false

	decoder := json.NewDecoder(bytes.NewReader(data))
	token, err := decoder.Token()
	if err != nil || token != json.Delim('{') {
		return errors.New("refresh request must be a JSON object")
	}
	for decoder.More() {
		token, err := decoder.Token()
		if err != nil {
			return errors.New("decode refresh request field")
		}
		name, ok := token.(string)
		if !ok || name != "refresh_token" {
			return errors.New("refresh request contains an unknown field")
		}
		if request.RefreshTokenPresent {
			return errors.New("refresh request contains duplicate refresh_token fields")
		}
		request.RefreshTokenPresent = true

		var raw json.RawMessage
		if err := decoder.Decode(&raw); err != nil {
			return errors.New("decode refresh_token")
		}
		if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
			continue
		}
		if err := json.Unmarshal(raw, &request.RefreshToken); err != nil {
			return errors.New("refresh_token must be a string")
		}
		request.RefreshTokenString = true
	}
	token, err = decoder.Token()
	if err != nil || token != json.Delim('}') {
		return errors.New("refresh request has an invalid closing delimiter")
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		return errors.New("refresh request contains trailing data")
	}
	return nil
}

type bindEmailRequest struct {
	Email string `json:"email"`
	Code  string `json:"code"`
}

type weChatCodeRequest struct {
	Code string `json:"code"`
}

type acceptedResponse struct {
	Status string `json:"status"`
}

type accountResponse struct {
	ID         uuid.UUID           `json:"id"`
	Status     identity.UserStatus `json:"status"`
	CreatedAt  time.Time           `json:"created_at"`
	Identities []identityResponse  `json:"identities"`
}

type identityResponse struct {
	Kind    identity.IdentityKind `json:"kind"`
	Display string                `json:"display"`
}

type accountEnvelope struct {
	Account accountResponse `json:"account"`
}

type loginResponse struct {
	AccessToken  string          `json:"access_token"`
	TokenType    string          `json:"token_type"`
	ExpiresIn    int64           `json:"expires_in"`
	Account      accountResponse `json:"account"`
	RefreshToken string          `json:"refresh_token,omitempty"`
}

func (h *handler) requestEmailCode(w http.ResponseWriter, r *http.Request) {
	var request emailRequest
	if err := decodeJSON(r, &request); err != nil {
		h.writeDecodeError(w, r, err)
		return
	}
	if strings.TrimSpace(request.Email) == "" {
		h.writeError(w, r, errorContextEmailDelivery, identity.ErrInvalidRequest)
		return
	}
	ip, err := httpx.ClientIP(r, h.trustedProxies)
	if err != nil {
		h.writeError(w, r, errorContextEmailDelivery, identity.ErrInvalidRequest)
		return
	}
	if err := h.service.RequestEmailLoginCode(r.Context(), request.Email, ip.String()); err != nil {
		h.writeError(w, r, errorContextEmailDelivery, err)
		return
	}
	writeJSON(w, http.StatusAccepted, acceptedResponse{Status: "accepted"})
}

func (h *handler) emailLogin(w http.ResponseWriter, r *http.Request) {
	var request emailLoginRequest
	if err := decodeJSON(r, &request); err != nil {
		h.writeDecodeError(w, r, err)
		return
	}
	if strings.TrimSpace(request.Email) == "" || request.Code == "" || request.ClientKind.Validate() != nil {
		h.writeError(w, r, errorContextEmailProof, identity.ErrInvalidRequest)
		return
	}
	result, err := h.service.LoginEmail(r.Context(), request.Email, request.Code, request.ClientKind)
	if err != nil {
		h.writeError(w, r, errorContextEmailProof, err)
		return
	}
	if result.Client != request.ClientKind {
		h.writeError(w, r, errorContextDefault, identity.ErrStateUnavailable)
		return
	}
	h.writeLogin(w, r, result, accountFromUser(result.User))
}

func (h *handler) wechatLogin(w http.ResponseWriter, r *http.Request) {
	var request weChatLoginRequest
	if err := decodeJSON(r, &request); err != nil {
		h.writeDecodeError(w, r, err)
		return
	}
	if !validWeChatCode(request.Code) || request.ClientKind.Validate() != nil {
		h.writeError(w, r, errorContextWeChatProof, identity.ErrInvalidRequest)
		return
	}
	ip, err := httpx.ClientIP(r, h.trustedProxies)
	if err != nil {
		h.writeError(w, r, errorContextWeChatProof, identity.ErrInvalidRequest)
		return
	}
	result, err := h.service.LoginWeChat(r.Context(), request.Code, ip.String(), request.ClientKind)
	if err != nil {
		h.writeError(w, r, errorContextWeChatProof, err)
		return
	}
	if result.Client != request.ClientKind {
		h.writeError(w, r, errorContextDefault, identity.ErrStateUnavailable)
		return
	}
	h.writeLogin(w, r, result, accountFromUser(result.User))
}

func (h *handler) refresh(w http.ResponseWriter, r *http.Request) {
	var request refreshRequest
	if err := decodeJSON(r, &request); err != nil {
		h.writeDecodeError(w, r, err)
		return
	}
	cookieToken, cookieCount := namedCookie(r, h.cookie.Name)
	jsonPresent := request.RefreshTokenPresent
	if cookieCount > 1 || (cookieCount == 1 && cookieToken == "") ||
		(cookieCount == 1) == jsonPresent {
		h.writeError(w, r, errorContextRefresh, identity.ErrInvalidRequest)
		return
	}
	if cookieCount == 0 && (!request.RefreshTokenString || request.RefreshToken == "") {
		h.writeError(w, r, errorContextRefresh, identity.ErrInvalidRequest)
		return
	}

	client := identity.ClientWeChatMini
	rawToken := request.RefreshToken
	if cookieCount == 1 {
		if err := httpx.RequireAllowedOrigin(r, h.cookie.AllowedOrigins); err != nil {
			h.writeError(w, r, errorContextOrigin, err)
			return
		}
		client = identity.ClientWeb
		rawToken = cookieToken
	}
	result, err := h.service.Refresh(r.Context(), rawToken, client)
	if err != nil {
		h.writeError(w, r, errorContextRefresh, err)
		return
	}
	if result.Client != client {
		h.writeError(w, r, errorContextDefault, identity.ErrStateUnavailable)
		return
	}
	h.writeLogin(w, r, result, accountFromUser(result.User))
}

func (h *handler) logout(w http.ResponseWriter, r *http.Request) {
	principal, err := principalFromContext(r.Context())
	if err != nil {
		h.writeError(w, r, errorContextAccess, identity.ErrTokenInvalid)
		return
	}
	if err := h.service.Logout(r.Context(), principal); err != nil {
		h.writeError(w, r, errorContextAccess, err)
		return
	}
	h.clearRefreshCookie(w)
	writeJSON(w, http.StatusOK, acceptedResponse{Status: "ok"})
}

func (h *handler) logoutAll(w http.ResponseWriter, r *http.Request) {
	principal, err := principalFromContext(r.Context())
	if err != nil {
		h.writeError(w, r, errorContextAccess, identity.ErrTokenInvalid)
		return
	}
	if err := h.service.LogoutAll(r.Context(), principal); err != nil {
		h.writeError(w, r, errorContextAccess, err)
		return
	}
	h.clearRefreshCookie(w)
	writeJSON(w, http.StatusOK, acceptedResponse{Status: "ok"})
}

func (h *handler) requestBindEmailCode(w http.ResponseWriter, r *http.Request) {
	var request emailRequest
	if err := decodeJSON(r, &request); err != nil {
		h.writeDecodeError(w, r, err)
		return
	}
	principal, err := principalFromContext(r.Context())
	if err != nil || strings.TrimSpace(request.Email) == "" {
		h.writeError(w, r, errorContextAccess, identity.ErrInvalidRequest)
		return
	}
	ip, err := httpx.ClientIP(r, h.trustedProxies)
	if err != nil {
		h.writeError(w, r, errorContextEmailDelivery, identity.ErrInvalidRequest)
		return
	}
	if err := h.service.RequestBindEmailCode(r.Context(), principal, request.Email, ip.String()); err != nil {
		h.writeError(w, r, errorContextEmailDelivery, err)
		return
	}
	writeJSON(w, http.StatusAccepted, acceptedResponse{Status: "accepted"})
}

func (h *handler) bindEmail(w http.ResponseWriter, r *http.Request) {
	var request bindEmailRequest
	if err := decodeJSON(r, &request); err != nil {
		h.writeDecodeError(w, r, err)
		return
	}
	principal, err := principalFromContext(r.Context())
	if err != nil || strings.TrimSpace(request.Email) == "" || request.Code == "" {
		h.writeError(w, r, errorContextEmailProof, identity.ErrInvalidRequest)
		return
	}
	result, err := h.service.BindEmail(r.Context(), principal, request.Email, request.Code)
	if err != nil {
		h.writeError(w, r, errorContextEmailProof, err)
		return
	}
	h.writeBind(w, r, result)
}

func (h *handler) bindWeChat(w http.ResponseWriter, r *http.Request) {
	var request weChatCodeRequest
	if err := decodeJSON(r, &request); err != nil {
		h.writeDecodeError(w, r, err)
		return
	}
	principal, err := principalFromContext(r.Context())
	if err != nil || !validWeChatCode(request.Code) {
		h.writeError(w, r, errorContextWeChatProof, identity.ErrInvalidRequest)
		return
	}
	ip, err := httpx.ClientIP(r, h.trustedProxies)
	if err != nil {
		h.writeError(w, r, errorContextWeChatProof, identity.ErrInvalidRequest)
		return
	}
	result, err := h.service.BindWeChat(r.Context(), principal, request.Code, ip.String())
	if err != nil {
		h.writeError(w, r, errorContextWeChatProof, err)
		return
	}
	h.writeBind(w, r, result)
}

func (h *handler) me(w http.ResponseWriter, r *http.Request) {
	principal, err := principalFromContext(r.Context())
	if err != nil {
		h.writeError(w, r, errorContextAccess, identity.ErrTokenInvalid)
		return
	}
	result, err := h.service.Me(r.Context(), principal)
	if err != nil {
		h.writeError(w, r, errorContextAccess, err)
		return
	}
	writeJSON(w, http.StatusOK, accountEnvelope{Account: accountFromSummary(result)})
}

func (h *handler) writeBind(w http.ResponseWriter, r *http.Request, result identity.BindResult) {
	if result.Session == nil {
		writeJSON(w, http.StatusOK, accountEnvelope{Account: accountFromSummary(result.Account)})
		return
	}
	if result.Session.User.ID != result.Account.User.ID {
		h.writeError(w, r, errorContextDefault, identity.ErrStateUnavailable)
		return
	}
	h.writeLogin(w, r, *result.Session, accountFromSummary(result.Account))
}

func (h *handler) writeLogin(
	w http.ResponseWriter,
	r *http.Request,
	result identity.LoginResult,
	account accountResponse,
) {
	now := h.clock.Now().UTC()
	expiresIn, err := accessExpiresIn(now, result.AccessExpiresAt.UTC())
	if err != nil || result.AccessToken == "" || result.RefreshToken == "" || result.User.ID == uuid.Nil {
		h.writeError(w, r, errorContextDefault, identity.ErrStateUnavailable)
		return
	}
	response := loginResponse{
		AccessToken: result.AccessToken,
		TokenType:   "Bearer",
		ExpiresIn:   expiresIn,
		Account:     account,
	}
	switch result.Client {
	case identity.ClientWeb:
		if err := h.setRefreshCookie(w, result.RefreshToken, result.RefreshExpiresAt); err != nil {
			h.writeError(w, r, errorContextDefault, identity.ErrStateUnavailable)
			return
		}
	case identity.ClientWeChatMini:
		response.RefreshToken = result.RefreshToken
	default:
		h.writeError(w, r, errorContextDefault, identity.ErrStateUnavailable)
		return
	}
	writeJSON(w, http.StatusOK, response)
}

func accountFromUser(user identity.User) accountResponse {
	return accountResponse{
		ID: user.ID, Status: user.Status, CreatedAt: user.CreatedAt.UTC(),
		Identities: make([]identityResponse, 0),
	}
}

func accountFromSummary(summary identity.AccountSummary) accountResponse {
	identities := make([]identityResponse, 0, len(summary.Identities))
	for _, item := range summary.Identities {
		identities = append(identities, identityResponse{Kind: item.Kind, Display: item.Display})
	}
	return accountResponse{
		ID: summary.User.ID, Status: summary.User.Status,
		CreatedAt: summary.User.CreatedAt.UTC(), Identities: identities,
	}
}

func validWeChatCode(code string) bool {
	return code != "" && utf8.ValidString(code) && utf8.RuneCountInString(code) <= 256
}

func decodeJSON(r *http.Request, target any) error {
	values := r.Header.Values("Content-Type")
	if len(values) != 1 {
		return errors.New("Content-Type must be application/json")
	}
	mediaType, _, err := mime.ParseMediaType(values[0])
	if err != nil || !strings.EqualFold(mediaType, "application/json") {
		return errors.New("Content-Type must be application/json")
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, maxJSONBodyBytes+1))
	if err != nil {
		return errors.New("read request body")
	}
	if len(body) > maxJSONBodyBytes {
		return errBodyTooLarge
	}
	trimmed := bytes.TrimSpace(body)
	if len(trimmed) == 0 {
		return errors.New("request body is empty")
	}
	if trimmed[0] != '{' {
		return errors.New("request body must be one JSON object")
	}
	decoder := json.NewDecoder(bytes.NewReader(trimmed))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return errors.New("request body is invalid")
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return errors.New("request body must contain exactly one JSON object")
	}
	return nil
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

type errorContext uint8

const (
	errorContextDefault errorContext = iota
	errorContextEmailProof
	errorContextWeChatProof
	errorContextAccess
	errorContextRefresh
	errorContextEmailDelivery
	errorContextOrigin
)

type publicError struct {
	status int
	code   string
	title  string
	detail string
}

func (h *handler) writeDecodeError(w http.ResponseWriter, r *http.Request, err error) {
	if errors.Is(err, errBodyTooLarge) {
		h.writePublicError(w, r, publicError{
			status: http.StatusBadRequest, code: "invalid_request",
			title: "请求无效", detail: "请求体过大。",
		})
		return
	}
	detail := "请求 JSON 格式无效。"
	if strings.Contains(err.Error(), "Content-Type") {
		detail = "Content-Type 必须为 application/json。"
	}
	h.writePublicError(w, r, publicError{
		status: http.StatusBadRequest, code: "invalid_request",
		title: "请求无效", detail: detail,
	})
}

func (h *handler) writeError(w http.ResponseWriter, r *http.Request, context errorContext, err error) {
	h.writePublicError(w, r, mapError(context, err))
}

func (h *handler) writePublicError(w http.ResponseWriter, r *http.Request, public publicError) {
	traceID := httpx.RequestID(r.Context())
	if traceID == "" {
		traceID = uuid.NewString()
		w.Header().Set("X-Request-ID", traceID)
	}
	h.logger.Warn("identity HTTP request rejected",
		"category", public.code,
		"trace_id", traceID,
	)
	httpx.WriteProblem(w, httpx.Problem{
		Type: "about:blank", Title: public.title, Status: public.status,
		Code: public.code, TraceID: traceID, Detail: public.detail,
	})
}

func mapError(context errorContext, err error) publicError {
	switch {
	case context == errorContextOrigin || errors.Is(err, httpx.ErrOriginNotAllowed):
		return publicError{403, "origin_not_allowed", "来源不允许", "请求来源不在允许列表中。"}
	case errors.Is(err, identity.ErrInvalidRequest):
		return publicError{400, "invalid_request", "请求无效", "请检查请求参数。"}
	case errors.Is(err, identity.ErrCodeInvalid) && context == errorContextWeChatProof:
		return publicError{400, "invalid_wechat_code", "微信凭证无效", "微信登录凭证无效。"}
	case errors.Is(err, identity.ErrCodeInvalid):
		return publicError{400, "invalid_email_code", "邮箱验证码无效", "邮箱验证码无效。"}
	case errors.Is(err, identity.ErrCodeExpired):
		return publicError{400, "email_code_expired", "邮箱验证码已过期", "邮箱验证码已过期，请重新获取。"}
	case errors.Is(err, identity.ErrTokenInvalid) && context == errorContextRefresh:
		return publicError{401, "invalid_refresh_token", "刷新令牌无效", "刷新令牌无效或已过期。"}
	case errors.Is(err, identity.ErrTokenInvalid):
		return publicError{401, "invalid_access_token", "访问令牌无效", "访问令牌无效或已过期。"}
	case errors.Is(err, identity.ErrTokenReused):
		return publicError{401, "refresh_token_reused", "刷新令牌已被重用", "会话已失效，请重新登录。"}
	case errors.Is(err, identity.ErrAccountDisabled):
		return publicError{403, "account_disabled", "账户已停用", "账户当前不可用。"}
	case errors.Is(err, identity.ErrConflict):
		return publicError{409, "identity_conflict", "身份冲突", "该身份无法绑定到当前账户。"}
	case errors.Is(err, identity.ErrRateLimited):
		return publicError{429, "rate_limited", "请求过于频繁", "请求过于频繁，请稍后再试。"}
	case errors.Is(err, identity.ErrStateUnavailable):
		return publicError{503, "service_not_ready", "服务暂不可用", "服务暂不可用，请稍后重试。"}
	case errors.Is(err, identity.ErrUpstreamUnavailable) && context == errorContextEmailDelivery:
		return publicError{503, "email_delivery_unavailable", "邮件服务暂不可用", "验证码邮件暂时无法发送，请稍后重试。"}
	case errors.Is(err, identity.ErrUpstreamUnavailable) && context == errorContextWeChatProof:
		return publicError{503, "wechat_unavailable", "微信服务暂不可用", "微信服务暂不可用，请稍后重试。"}
	default:
		return publicError{500, "internal_error", "内部错误", "服务发生内部错误。"}
	}
}

func nilLike(value any) bool {
	if value == nil {
		return true
	}
	reflected := reflect.ValueOf(value)
	switch reflected.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return reflected.IsNil()
	default:
		return false
	}
}
