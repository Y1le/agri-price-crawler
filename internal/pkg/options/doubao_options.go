package options

import (
	"fmt"

	"github.com/spf13/pflag"
)

// DoubaoOptions doubao api options
type DoubaoOptions struct {
	APIKey     string `json:"apiKey" mapstructure:"api-key"`         // 豆包API密钥
	BaseURL    string `json:"baseURL" mapstructure:"base-url"`       // 豆包API基础URL
	Model      string `json:"model" mapstructure:"model"`            // 豆包模型名称（如doubao-pro）
	TimeoutSec int    `json:"timeoutSec" mapstructure:"timeout-sec"` // 请求超时（秒）
	MaxRetries int    `json:"maxRetries" mapstructure:"max-retries"` // 最大重试次数
}

// NewDoubaoOptions creates a new DoubaoOptions with default values.
func NewDoubaoOptions() *DoubaoOptions {
	return &DoubaoOptions{
		APIKey:     "",
		BaseURL:    "https://ark.cn-beijing.volces.com/api/v3",
		Model:      "",
		TimeoutSec: 30,
		MaxRetries: 3,
	}
}

// Validate verifies flags passed to DoubaoOptions.
func (o *DoubaoOptions) Validate() []error {
	errs := []error{}
	if o.APIKey == "" {
		errs = append(errs, fmt.Errorf("doubao api key is required"))
	}
	// BaseURL is required
	if o.BaseURL == "" {
		errs = append(errs, fmt.Errorf("doubao base url is required"))
	}

	if o.Model == "" {
		errs = append(errs, fmt.Errorf("doubao model is required"))
	}
	return errs
}

// AddFlags adds flags for DoubaoOptions to the specified FlagSet.
func (o *DoubaoOptions) AddFlags(fs *pflag.FlagSet) {
	fs.StringVar(&o.APIKey, "doubao-api-key", o.APIKey, "Doubao API key.")
	fs.StringVar(&o.BaseURL, "doubao-base-url", o.BaseURL, "Doubao API base URL.")
	fs.StringVar(&o.Model, "doubao-model", o.Model, "Doubao model name.")
	fs.IntVar(&o.TimeoutSec, "doubao-timeout", o.TimeoutSec, "Doubao request timeout (seconds).")
	fs.IntVar(&o.MaxRetries, "doubao-max-retries", o.MaxRetries, "Doubao max retries.")
}
