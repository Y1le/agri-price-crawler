// Package wechat exchanges one-time mini-program login codes for durable
// provider identity data.
package wechat

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/Y1le/agri-price-crawler/internal/identity"
	"github.com/Y1le/agri-price-crawler/internal/platform/config"
)

const (
	codeExchangePath = "/sns/jscode2session"
	maxResponseBytes = 64 * 1024
)

// Client is a constrained jscode2session client. It deliberately retains only
// the provider credentials needed to make a request; session_key is decoded
// into a request-local value and is never returned.
type Client struct {
	appID     string
	appSecret string
	endpoint  *url.URL
	http      *http.Client
}

type exchangeResponse struct {
	OpenID  string `json:"openid"`
	UnionID string `json:"unionid"`
	ErrCode int    `json:"errcode"`
}

// New validates the adapter-specific configuration and creates a timeout-bound
// HTTP client.
func New(cfg config.WeChat) (*Client, error) {
	if strings.TrimSpace(cfg.AppID) == "" {
		return nil, fmt.Errorf("wechat: app ID is required")
	}
	if strings.TrimSpace(cfg.AppSecret) == "" {
		return nil, fmt.Errorf("wechat: app secret is required")
	}
	if cfg.Timeout <= 0 {
		return nil, fmt.Errorf("wechat: timeout must be positive")
	}

	baseURL, err := url.Parse(cfg.BaseURL)
	if err != nil ||
		(baseURL.Scheme != "http" && baseURL.Scheme != "https") ||
		baseURL.Host == "" ||
		baseURL.User != nil ||
		baseURL.RawQuery != "" ||
		baseURL.Fragment != "" {
		return nil, fmt.Errorf("wechat: base URL must be an absolute HTTP(S) URL without credentials, query, or fragment")
	}

	endpoint := *baseURL
	endpoint.Path = strings.TrimRight(endpoint.Path, "/") + codeExchangePath
	endpoint.RawPath = ""

	return &Client{
		appID:     cfg.AppID,
		appSecret: cfg.AppSecret,
		endpoint:  &endpoint,
		http: &http.Client{
			Timeout: cfg.Timeout,
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
	}, nil
}

// Exchange exchanges a one-time code and returns only identity fields that may
// be persisted. Provider response text and request credentials are never
// included in returned errors.
func (client *Client) Exchange(ctx context.Context, code string) (identity.WeChatIdentity, error) {
	if strings.TrimSpace(code) == "" {
		return identity.WeChatIdentity{}, fmt.Errorf("wechat: temporary code rejected: %w", identity.ErrCodeInvalid)
	}

	requestURL := *client.endpoint
	query := requestURL.Query()
	query.Set("appid", client.appID)
	query.Set("secret", client.appSecret)
	query.Set("js_code", code)
	query.Set("grant_type", "authorization_code")
	requestURL.RawQuery = query.Encode()

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL.String(), nil)
	if err != nil {
		return identity.WeChatIdentity{}, fmt.Errorf("wechat: build exchange request: %w", identity.ErrUpstreamUnavailable)
	}

	response, err := client.http.Do(request)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return identity.WeChatIdentity{}, fmt.Errorf("wechat: exchange canceled: %w", ctxErr)
		}
		return identity.WeChatIdentity{}, fmt.Errorf("wechat: exchange request failed: %w", identity.ErrUpstreamUnavailable)
	}
	defer response.Body.Close()

	body, err := io.ReadAll(io.LimitReader(response.Body, maxResponseBytes+1))
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return identity.WeChatIdentity{}, fmt.Errorf("wechat: exchange canceled: %w", ctxErr)
		}
		return identity.WeChatIdentity{}, fmt.Errorf("wechat: read exchange response: %w", identity.ErrUpstreamUnavailable)
	}
	if len(body) > maxResponseBytes {
		return identity.WeChatIdentity{}, fmt.Errorf("wechat: exchange response exceeds limit: %w", identity.ErrUpstreamUnavailable)
	}

	if response.StatusCode == http.StatusTooManyRequests || response.StatusCode >= http.StatusInternalServerError {
		return identity.WeChatIdentity{}, fmt.Errorf("wechat: provider temporarily unavailable: %w", identity.ErrUpstreamUnavailable)
	}

	var provider exchangeResponse
	if err := json.Unmarshal(body, &provider); err != nil {
		return identity.WeChatIdentity{}, fmt.Errorf("wechat: malformed exchange response: %w", identity.ErrUpstreamUnavailable)
	}

	if provider.ErrCode != 0 {
		return identity.WeChatIdentity{}, classifyProviderError(provider.ErrCode)
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return identity.WeChatIdentity{}, fmt.Errorf("wechat: provider rejected exchange request")
	}
	if provider.OpenID == "" {
		return identity.WeChatIdentity{}, fmt.Errorf("wechat: exchange response missing identity: %w", identity.ErrUpstreamUnavailable)
	}

	return identity.WeChatIdentity{
		AppID:   client.appID,
		OpenID:  provider.OpenID,
		UnionID: provider.UnionID,
	}, nil
}

func classifyProviderError(code int) error {
	switch code {
	case 40029, 40163:
		return fmt.Errorf("wechat: temporary code rejected: %w", identity.ErrCodeInvalid)
	case -1, 45009, 45011:
		return fmt.Errorf("wechat: provider temporarily unavailable: %w", identity.ErrUpstreamUnavailable)
	default:
		return fmt.Errorf("wechat: provider rejected exchange request")
	}
}
