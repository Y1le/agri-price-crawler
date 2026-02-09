package options

// Validate checks Options and return a slice of found errs.
func (o *Options) Validate() []error {
	var errs []error
	errs = append(errs, o.MySQLOptions.Validate()...)
	errs = append(errs, o.RedisOptions.Validate()...)
	errs = append(errs, o.Log.Validate()...)
	errs = append(errs, o.CronOptions.Validate()...)
	errs = append(errs, o.CrawlerOptions.Validate()...)
	errs = append(errs, o.EmailOptions.Validate()...)

	return errs
}
