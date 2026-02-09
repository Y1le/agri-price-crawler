package options

import (
	"encoding/json"

	cliflag "github.com/marmotedu/component-base/pkg/cli/flag"

	genericoptions "github.com/Y1le/agri-price-crawler/internal/pkg/options"
	"github.com/Y1le/agri-price-crawler/pkg/log"
)

// Options runs a agri-price-crawler server.
type Options struct {
	GenericServerRunOptions *genericoptions.ServerRunOptions       `json:"server"   mapstructure:"server"`
	InsecureServing         *genericoptions.InsecureServingOptions `json:"insecure" mapstructure:"insecure"`
	SecureServing           *genericoptions.SecureServingOptions   `json:"secure"   mapstructure:"secure"`
	MySQLOptions            *genericoptions.MySQLOptions           `json:"mysql"      mapstructure:"mysql"`
	RedisOptions            *genericoptions.RedisOptions           `json:"redis"      mapstructure:"redis"`
	CronOptions             *genericoptions.CronOptions            `json:"cron"           mapstructure:"cron"`
	CrawlerOptions          *genericoptions.CrawlerOptions         `json:"crawler"       mapstructure:"crawler"`
	JwtOptions              *genericoptions.JwtOptions             `json:"jwt"      mapstructure:"jwt"`
	Log                     *log.Options                           `json:"log"      mapstructure:"log"`
	FeatureOptions          *genericoptions.FeatureOptions         `json:"feature"  mapstructure:"feature"`
	EmailOptions            *genericoptions.EmailOptions           `json:"email"      mapstructure:"email"`
}

// NewOptions creates a new Options object with default parameters.
func NewOptions() *Options {
	o := Options{
		MySQLOptions:   genericoptions.NewMySQLOptions(),
		RedisOptions:   genericoptions.NewRedisOptions(),
		CronOptions:    genericoptions.NewCronOptions(),
		CrawlerOptions: genericoptions.NewCrawlerOptions(),
		Log:            log.NewOptions(),
		EmailOptions:   genericoptions.NewEmailOptions(),
	}

	return &o
}

// Flags returns flags for a specific APIServer by section name.
func (o *Options) Flags() (fss cliflag.NamedFlagSets) {
	o.MySQLOptions.AddFlags(fss.FlagSet("mysql"))
	o.RedisOptions.AddFlags(fss.FlagSet("redis"))
	o.CronOptions.AddFlags(fss.FlagSet("cron"))
	o.Log.AddFlags(fss.FlagSet("logs"))
	o.CrawlerOptions.AddFlags(fss.FlagSet("crawler"))
	o.EmailOptions.AddFlags(fss.FlagSet("email"))

	return fss
}

func (o *Options) String() string {
	data, _ := json.Marshal(o)

	return string(data)
}
