package jobs_test

import (
	"errors"
	"fmt"
	"testing"

	"github.com/Y1le/agri-price-crawler/internal/platform/jobs"
)

func TestPermanentPreservesCauseTextAndClassification(t *testing.T) {
	cause := errors.New("invalid market code")
	classified := jobs.Permanent(cause)
	if classified == nil {
		t.Fatal("Permanent returned nil for a non-nil cause")
	}
	if classified.Error() != cause.Error() {
		t.Fatalf("error text = %q, want %q", classified, cause)
	}
	if !errors.Is(classified, cause) {
		t.Fatal("classified error does not preserve its cause")
	}
	if !jobs.IsPermanent(fmt.Errorf("process payload: %w", classified)) {
		t.Fatal("IsPermanent did not classify a wrapped permanent error")
	}
	if jobs.IsPermanent(cause) {
		t.Fatal("IsPermanent classified an ordinary error")
	}
	if jobs.Permanent(nil) != nil {
		t.Fatal("Permanent(nil) is non-nil")
	}
}
