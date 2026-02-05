package options

import "github.com/spf13/pflag"

// CronOptions defines the configuration for cron jobs.
type CrawlerOptions struct {
	DeviceID string `json:"deviceID" mapstructure:"device-id"`
	Secret   string `json:"secret" mapstructure:"secret"`
}

// NewCrawlerOptions creates a new CrawlerOptions with default values.
func NewCrawlerOptions() *CrawlerOptions {
	return &CrawlerOptions{
		DeviceID: "",
		Secret:   "",
	}
}

// Validate verifies flags passed to CrawlerOptions.
func (o *CrawlerOptions) Validate() []error {
	errs := []error{}

	return errs
}

// AddFlags adds flags for CrawlerOptions to the specified FlagSet.
func (o *CrawlerOptions) AddFlags(fs *pflag.FlagSet) {
	fs.StringVar(&o.DeviceID, "crawler.device-id", o.DeviceID,
		"Device ID for the crawler.")
	fs.StringVar(&o.Secret, "crawler.secret", o.Secret,
		"Secret key for the crawler.")
}
