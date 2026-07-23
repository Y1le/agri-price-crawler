// Package ingestion owns source ingestion, batch lifecycle, and data cleanup.
package ingestion

import (
	"fmt"
	"time"
)

const businessDateLayout = "2006-01-02"

var shanghaiLocation = func() *time.Location {
	location, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		panic(fmt.Sprintf("load Asia/Shanghai location: %v", err))
	}
	return location
}()

// BusinessDate is a calendar date interpreted in Asia/Shanghai.
// Its zero value is invalid; construct it with ParseBusinessDate or
// BusinessDateFromTime.
type BusinessDate string

func ParseBusinessDate(value string) (BusinessDate, error) {
	parsed, err := time.ParseInLocation(businessDateLayout, value, shanghaiLocation)
	if err != nil || parsed.Format(businessDateLayout) != value {
		return "", fmt.Errorf("business date must use YYYY-MM-DD")
	}
	return BusinessDate(value), nil
}

func BusinessDateFromTime(value time.Time) BusinessDate {
	return BusinessDate(value.In(shanghaiLocation).Format(businessDateLayout))
}

func (d BusinessDate) String() string {
	return string(d)
}

// StartOfDay returns the beginning of the business date in Asia/Shanghai.
func (d BusinessDate) StartOfDay() (time.Time, error) {
	parsed, err := ParseBusinessDate(d.String())
	if err != nil {
		return time.Time{}, err
	}
	return time.ParseInLocation(businessDateLayout, parsed.String(), shanghaiLocation)
}
