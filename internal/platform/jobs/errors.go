package jobs

import "errors"

type permanentError struct {
	cause error
}

func (e permanentError) Error() string {
	return e.cause.Error()
}

func (e permanentError) Unwrap() error {
	return e.cause
}

func (permanentError) permanent() {}

// Permanent classifies err as a permanent job failure while preserving its cause.
func Permanent(err error) error {
	if err == nil || IsPermanent(err) {
		return err
	}
	return permanentError{cause: err}
}

// IsPermanent reports whether err or an error in its chain is permanent.
func IsPermanent(err error) bool {
	var classified interface{ permanent() }
	return errors.As(err, &classified)
}
