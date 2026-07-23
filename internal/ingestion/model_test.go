package ingestion_test

import (
	"testing"
	"time"

	"github.com/Y1le/agri-price-crawler/internal/ingestion"
)

func TestParseBusinessDate(t *testing.T) {
	date, err := ingestion.ParseBusinessDate("2026-07-23")
	if err != nil {
		t.Fatal(err)
	}
	if date.String() != "2026-07-23" {
		t.Fatalf("String() = %q", date.String())
	}

	start, err := date.StartOfDay()
	if err != nil {
		t.Fatal(err)
	}
	if start.Location().String() != "Asia/Shanghai" || start.Hour() != 0 || start.Minute() != 0 {
		t.Fatalf("start = %v", start)
	}
}

func TestParseBusinessDateRejectsNonCanonicalValues(t *testing.T) {
	for _, value := range []string{"", "2026-7-23", "2026-02-29", "2026-07-23 ", "2026-07-23T00:00:00Z"} {
		t.Run(value, func(t *testing.T) {
			if _, err := ingestion.ParseBusinessDate(value); err == nil {
				t.Fatal("ParseBusinessDate succeeded")
			}
		})
	}
}

func TestBusinessDateFromTimeUsesShanghaiDate(t *testing.T) {
	instant := time.Date(2026, 7, 22, 16, 30, 0, 0, time.UTC)
	if got := ingestion.BusinessDateFromTime(instant); got.String() != "2026-07-23" {
		t.Fatalf("BusinessDateFromTime() = %q", got)
	}
}
