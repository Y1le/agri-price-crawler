package options

import "github.com/spf13/pflag"

// EmailOptions defines the configuration for email sending.
type EmailOptions struct {
	Port     int    `json:"port" yaml:"port"`
	Username string `json:"username" yaml:"username"`
	Password string `json:"password" yaml:"password"`
	From     string `json:"from" yaml:"from"`
}

// NewEmailOptions creates a new EmailOptions with default values.
func NewEmailOptions() *EmailOptions {
	return &EmailOptions{
		Port:     465,
		Username: "",
		Password: "",
		From:     "",
	}
}

// Validate verifies flags passed to EmailOptions.
func (o *EmailOptions) Validate() []error {
	errs := []error{}

	return errs
}

// AddFlags adds flags for EmailOptions to the specified FlagSet.
func (o *EmailOptions) AddFlags(fs *pflag.FlagSet) {
	fs.IntVar(&o.Port, "email-port", o.Port, "SMTP server port.")
	fs.StringVar(&o.Username, "email-username", o.Username, "SMTP server username.")
	fs.StringVar(&o.Password, "email-password", o.Password, "SMTP server password.")
	fs.StringVar(&o.From, "email-from", o.From, "Email sender address.")
}
