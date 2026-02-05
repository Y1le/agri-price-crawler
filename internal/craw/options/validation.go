package options

// Validate checks Options and return a slice of found errs.
func (o *Options) Validate() []error {
	var errs []error
	errs = append(errs, o.MySQLOptions.Validate()...)
	errs = append(errs, o.Log.Validate()...)

	return errs
}
