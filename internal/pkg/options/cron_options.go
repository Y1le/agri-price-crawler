package options

import "github.com/spf13/pflag"

// CronOptions defines the configuration for cron jobs.
type CronOptions struct {
	// EnableDailyEmailSender 启用每日邮件发送任务
	EnableDailyEmailSender bool `json:"enableDailyEmailSender" mapstructure:"enable-daily-email-sender"`
	// DailyEmailTime 每日发送时间（格式: HH:MM，24小时制）
	DailyEmailTime string `json:"dailyEmailTime" mapstructure:"daily-email-time"`

	// DailyCrawTime 每日爬取时间（格式: HH:MM，24小时制）
	DailyCrawTime string `json:"dailyCrawTime" mapstructure:"daily-craw-time"`
}

// NewCronOptions creates a new CronOptions with default values.
func NewCronOptions() *CronOptions {
	return &CronOptions{
		EnableDailyEmailSender: true,        // 默认关闭，避免误发
		DailyEmailTime:         "0 7 * * *", // 默认早上7点
		DailyCrawTime:          "0 4 * * *", // 默认凌晨4点
	}
}

// Validate verifies flags passed to CronOptions.
func (o *CronOptions) Validate() []error {
	errs := []error{}

	return errs
}

// AddFlags adds flags for CronOptions to the specified FlagSet.
func (o *CronOptions) AddFlags(fs *pflag.FlagSet) {
	fs.BoolVar(&o.EnableDailyEmailSender, "cron.enable-daily-email-sender", o.EnableDailyEmailSender,
		"Enable the daily email sender cron job.")
	fs.StringVar(&o.DailyEmailTime, "cron.daily-email-time", o.DailyEmailTime,
		"0 7 * * *")
	fs.StringVar(&o.DailyCrawTime, "cron.daily-craw-time", o.DailyCrawTime,
		"0 4 * * *")
}
