package options

import (
	"time"

	"github.com/spf13/pflag"
)

// AlertOptions defines the configuration for the alert system.
type AlertOptions struct {
	Enabled            bool          `json:"enabled" yaml:"enabled"`
	EvaluationInterval time.Duration `json:"evaluation_interval" yaml:"evaluation_interval"`
	EnabledAlerts      []string      `json:"enabled_alerts" yaml:"enabled_alerts"`
	PriceSpikePercent  float64       `json:"price_spike_percent" yaml:"price_spike_percent"`
	PriceSpikeAbsolute float64       `json:"price_spike_absolute" yaml:"price_spike_absolute"`
	PriceDropPercent   float64       `json:"price_drop_percent" yaml:"price_drop_percent"`
	PriceDropAbsolute  float64       `json:"price_drop_absolute" yaml:"price_drop_absolute"`
	VolatilityIndex    float64       `json:"volatility_index" yaml:"volatility_index"`
	DataMissingHours   int           `json:"data_missing_hours" yaml:"data_missing_hours"`
	Email              EmailOptions  `json:"email" yaml:"email"`
	WatchList          []string      `json:"watch_list" yaml:"watch_list"`
}

// NewAlertOptions creates a new AlertOptions with default values.
func NewAlertOptions() *AlertOptions {
	return &AlertOptions{
		Enabled:            false,
		EvaluationInterval: 60 * time.Second,
		EnabledAlerts:      []string{"price_change", "volatility", "missing_data"},
		PriceSpikePercent:  50,
		PriceSpikeAbsolute: 2.0,
		PriceDropPercent:   40,
		PriceDropAbsolute:  1.0,
		VolatilityIndex:    0.8,
		DataMissingHours:   3,
		Email:              *NewEmailOptions(),
		WatchList:          []string{},
	}
}

// Validate verifies flags passed to AlertOptions.
func (o *AlertOptions) Validate() []error {
	return []error{}
}

// AddFlags adds flags for AlertOptions to the specified FlagSet.
func (o *AlertOptions) AddFlags(fs *pflag.FlagSet) {
	fs.BoolVar(&o.Enabled, "alert.enabled", o.Enabled, "Enable alert system.")
	fs.DurationVar(&o.EvaluationInterval, "alert.evaluation-interval", o.EvaluationInterval, "Evaluation interval for alerts.")
	fs.Float64Var(&o.PriceSpikePercent, "alert.price-spike-percent", o.PriceSpikePercent, "Price spike percentage threshold.")
	fs.Float64Var(&o.PriceSpikeAbsolute, "alert.price-spike-absolute", o.PriceSpikeAbsolute, "Price spike absolute threshold.")
	fs.Float64Var(&o.PriceDropPercent, "alert.price-drop-percent", o.PriceDropPercent, "Price drop percentage threshold.")
	fs.Float64Var(&o.PriceDropAbsolute, "alert.price-drop-absolute", o.PriceDropAbsolute, "Price drop absolute threshold.")
	fs.Float64Var(&o.VolatilityIndex, "alert.volatility-index", o.VolatilityIndex, "Volatility index threshold.")
	fs.IntVar(&o.DataMissingHours, "alert.data-missing-hours", o.DataMissingHours, "Hours before data is considered missing.")
	fs.StringSliceVar(&o.WatchList, "alert.watch-list", o.WatchList, "List of products to watch.")
}
