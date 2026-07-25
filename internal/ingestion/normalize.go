package ingestion

import (
	"fmt"
	"math/big"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
)

const moneyScale int64 = 10_000

const maxInt64 = int64(^uint64(0) >> 1)

// Money stores CNY/kg as a fixed four-decimal value.
type Money int64

func ParseMoney(value string) (Money, error) {
	value = strings.TrimSpace(value)
	if value == "" || strings.HasPrefix(value, "+") || strings.HasPrefix(value, "-") {
		return 0, fmt.Errorf("money must be a non-negative decimal")
	}
	parts := strings.Split(value, ".")
	if len(parts) > 2 || parts[0] == "" {
		return 0, fmt.Errorf("money must be a non-negative decimal")
	}
	whole, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil || whole < 0 || whole > 999_999_999_999 {
		return 0, fmt.Errorf("money is out of range")
	}
	fraction := ""
	if len(parts) == 2 {
		fraction = parts[1]
	}
	if len(fraction) > 4 {
		return 0, fmt.Errorf("money supports at most four decimal places")
	}
	for _, digit := range fraction {
		if digit < '0' || digit > '9' {
			return 0, fmt.Errorf("money must be a non-negative decimal")
		}
	}
	fraction += strings.Repeat("0", 4-len(fraction))
	fractionValue := int64(0)
	if fraction != "" {
		fractionValue, err = strconv.ParseInt(fraction, 10, 64)
		if err != nil {
			return 0, fmt.Errorf("money must be a non-negative decimal")
		}
	}
	if whole > (maxInt64-fractionValue)/moneyScale {
		return 0, fmt.Errorf("money is out of range")
	}
	return Money(whole*moneyScale + fractionValue), nil
}

func (m Money) String() string {
	whole := int64(m) / moneyScale
	fraction := int64(m) % moneyScale
	return fmt.Sprintf("%d.%04d", whole, fraction)
}

func (m Money) times(multiplier int64) (Money, bool) {
	if multiplier < 0 || int64(m) > (int64(^uint64(0)>>1))/multiplier {
		return 0, false
	}
	return Money(int64(m) * multiplier), true
}

type SourceProduct struct {
	CategoryID string
	BreedID    string
}

type SourceRegion struct {
	ProvinceID string
	CityID     string
	DistrictID string
	AreaKey    string
}

type SourceRecord struct {
	Key         string
	CategoryID  string
	BreedID     string
	ProvinceID  string
	CityID      string
	DistrictID  string
	AreaKey     string
	MarketName  string
	MinPrice    string
	MaxPrice    string
	Average     string
	Unit        string
	ObservedAt  time.Time
	SampleCount *int64
}

type MappingLookup interface {
	ProductID(SourceProduct) (uuid.UUID, bool)
	RegionID(SourceRegion) (uuid.UUID, bool)
}

type AcceptedObservation struct {
	SourceRecordKey string
	BusinessDate    BusinessDate
	ObservedAt      time.Time
	ProductID       uuid.UUID
	RegionID        uuid.UUID
	MarketName      string
	MinPrice        Money
	MaxPrice        Money
	AveragePrice    Money
	Unit            string
	SampleCount     *int64
}

type RejectedRecord struct {
	SourceRecordKey string
	Code            string
}

type NormalizeResult struct {
	Observation *AcceptedObservation
	Rejection   *RejectedRecord
}

type DailySummary struct {
	ProductID           uuid.UUID
	RegionID            uuid.UUID
	BusinessDate        BusinessDate
	MinPrice            Money
	MaxPrice            Money
	AveragePrice        Money
	ObservationCount    int
	WeightedSampleCount int64
}

const (
	RejectUnknownProductMapping = "unknown_product_mapping"
	RejectUnknownRegionMapping  = "unknown_region_mapping"
	RejectInvalidUnit           = "invalid_unit"
	RejectInvalidPrice          = "invalid_price"
	RejectInvalidTimestamp      = "invalid_timestamp"
)

// Normalize converts a source-neutral record to the public CNY/kg model.
func Normalize(record SourceRecord, businessDate BusinessDate, mappings MappingLookup) NormalizeResult {
	reject := func(code string) NormalizeResult {
		return NormalizeResult{Rejection: &RejectedRecord{SourceRecordKey: record.Key, Code: code}}
	}
	if strings.TrimSpace(record.Key) == "" || strings.TrimSpace(record.MarketName) == "" {
		return reject(RejectInvalidPrice)
	}
	if record.ObservedAt.IsZero() {
		return reject(RejectInvalidTimestamp)
	}
	if _, err := ParseBusinessDate(businessDate.String()); err != nil {
		return reject(RejectInvalidTimestamp)
	}
	if mappings == nil {
		return reject(RejectUnknownProductMapping)
	}
	productID, ok := mappings.ProductID(SourceProduct{CategoryID: record.CategoryID, BreedID: record.BreedID})
	if !ok || productID == uuid.Nil {
		return reject(RejectUnknownProductMapping)
	}
	regionID, ok := mappings.RegionID(SourceRegion{ProvinceID: record.ProvinceID, CityID: record.CityID, DistrictID: record.DistrictID, AreaKey: record.AreaKey})
	if !ok || regionID == uuid.Nil {
		return reject(RejectUnknownRegionMapping)
	}
	minPrice, err := ParseMoney(record.MinPrice)
	if err != nil {
		return reject(RejectInvalidPrice)
	}
	maxPrice, err := ParseMoney(record.MaxPrice)
	if err != nil {
		return reject(RejectInvalidPrice)
	}
	averagePrice, err := ParseMoney(record.Average)
	if err != nil || minPrice > maxPrice || averagePrice < minPrice || averagePrice > maxPrice {
		return reject(RejectInvalidPrice)
	}
	if normalizedMin, normalizedMax, normalizedAverage, ok := normalizeUnit(minPrice, maxPrice, averagePrice, record.Unit); ok {
		return NormalizeResult{Observation: &AcceptedObservation{
			SourceRecordKey: record.Key,
			BusinessDate:    businessDate,
			ObservedAt:      record.ObservedAt.UTC(),
			ProductID:       productID,
			RegionID:        regionID,
			MarketName:      strings.TrimSpace(record.MarketName),
			MinPrice:        normalizedMin,
			MaxPrice:        normalizedMax,
			AveragePrice:    normalizedAverage,
			Unit:            "CNY/kg",
			SampleCount:     record.SampleCount,
		}}
	}
	return reject(RejectInvalidUnit)
}

func normalizeUnit(minPrice, maxPrice, averagePrice Money, unit string) (Money, Money, Money, bool) {
	switch strings.ToLower(strings.TrimSpace(unit)) {
	case "元/公斤", "元/kg", "cny/kg":
		return minPrice, maxPrice, averagePrice, true
	case "元/斤":
		minPrice, minOK := minPrice.times(2)
		maxPrice, maxOK := maxPrice.times(2)
		averagePrice, averageOK := averagePrice.times(2)
		return minPrice, maxPrice, averagePrice, minOK && maxOK && averageOK
	default:
		return 0, 0, 0, false
	}
}

// BuildSummaries produces deterministic daily points grouped by product, region,
// and business date. A group is weighted only when every sample count is positive.
func BuildSummaries(observations []AcceptedObservation) ([]DailySummary, error) {
	type key struct {
		productID uuid.UUID
		regionID  uuid.UUID
		date      BusinessDate
	}
	groups := make(map[key][]AcceptedObservation)
	for _, observation := range observations {
		if observation.ProductID == uuid.Nil || observation.RegionID == uuid.Nil {
			return nil, fmt.Errorf("build summary: product and region are required")
		}
		if _, err := ParseBusinessDate(observation.BusinessDate.String()); err != nil {
			return nil, fmt.Errorf("build summary: %w", err)
		}
		groups[key{observation.ProductID, observation.RegionID, observation.BusinessDate}] = append(groups[key{observation.ProductID, observation.RegionID, observation.BusinessDate}], observation)
	}
	keys := make([]key, 0, len(groups))
	for groupKey := range groups {
		keys = append(keys, groupKey)
	}
	sort.Slice(keys, func(left, right int) bool {
		if keys[left].date != keys[right].date {
			return keys[left].date < keys[right].date
		}
		if keys[left].productID != keys[right].productID {
			return keys[left].productID.String() < keys[right].productID.String()
		}
		return keys[left].regionID.String() < keys[right].regionID.String()
	})

	summaries := make([]DailySummary, 0, len(keys))
	for _, groupKey := range keys {
		group := groups[groupKey]
		summary := DailySummary{
			ProductID: groupKey.productID, RegionID: groupKey.regionID, BusinessDate: groupKey.date,
			MinPrice: group[0].MinPrice, MaxPrice: group[0].MaxPrice, ObservationCount: len(group),
		}
		weighted := true
		for _, observation := range group {
			if observation.MinPrice < summary.MinPrice {
				summary.MinPrice = observation.MinPrice
			}
			if observation.MaxPrice > summary.MaxPrice {
				summary.MaxPrice = observation.MaxPrice
			}
			if observation.SampleCount == nil || *observation.SampleCount <= 0 {
				weighted = false
			} else {
				if *observation.SampleCount > maxInt64-summary.WeightedSampleCount {
					return nil, fmt.Errorf("build summary: weighted sample count is out of range")
				}
				summary.WeightedSampleCount += *observation.SampleCount
			}
		}
		if weighted {
			average, err := weightedAverage(group)
			if err != nil {
				return nil, err
			}
			summary.AveragePrice = average
		} else {
			summary.WeightedSampleCount = 0
			average, err := simpleAverage(group)
			if err != nil {
				return nil, err
			}
			summary.AveragePrice = average
		}
		summaries = append(summaries, summary)
	}
	return summaries, nil
}

func weightedAverage(observations []AcceptedObservation) (Money, error) {
	var sum, total big.Int
	for _, observation := range observations {
		if observation.SampleCount == nil || *observation.SampleCount <= 0 {
			return 0, fmt.Errorf("weighted average: sample count is required")
		}
		var value, samples big.Int
		value.SetInt64(int64(observation.AveragePrice))
		samples.SetInt64(*observation.SampleCount)
		value.Mul(&value, &samples)
		sum.Add(&sum, &value)
		total.Add(&total, &samples)
	}
	return roundedQuotient(&sum, &total)
}

func simpleAverage(observations []AcceptedObservation) (Money, error) {
	var sum big.Int
	for _, observation := range observations {
		sum.Add(&sum, big.NewInt(int64(observation.AveragePrice)))
	}
	return roundedQuotient(&sum, big.NewInt(int64(len(observations))))
}

func roundedQuotient(numerator, denominator *big.Int) (Money, error) {
	if denominator.Sign() <= 0 {
		return 0, fmt.Errorf("average denominator must be positive")
	}
	var quotient, remainder, doubledRemainder big.Int
	quotient.QuoRem(numerator, denominator, &remainder)
	doubledRemainder.Lsh(&remainder, 1)
	if doubledRemainder.Cmp(denominator) >= 0 {
		quotient.Add(&quotient, big.NewInt(1))
	}
	if !quotient.IsInt64() {
		return 0, fmt.Errorf("average is out of range")
	}
	return Money(quotient.Int64()), nil
}

// ValidatePublication enforces the accepted-record and exact rejection-ratio
// gate before a validated batch can become public.
func ValidatePublication(counts BatchCounts, rejectRatioMaxBasisPoints uint16) error {
	if counts.Fetched <= 0 || counts.Accepted <= 0 || counts.Rejected < 0 || counts.Accepted+counts.Rejected != counts.Fetched {
		return fmt.Errorf("%w: invalid record counts", ErrPublicationNotReady)
	}
	if rejectRatioMaxBasisPoints > 2_000 {
		return fmt.Errorf("%w: rejection limit exceeds 20 percent", ErrPublicationNotReady)
	}
	var rejected, fetched, limit, left, right big.Int
	rejected.SetInt64(int64(counts.Rejected))
	fetched.SetInt64(int64(counts.Fetched))
	limit.SetInt64(int64(rejectRatioMaxBasisPoints))
	left.Mul(&rejected, big.NewInt(10_000))
	right.Mul(&fetched, &limit)
	if left.Cmp(&right) > 0 {
		return fmt.Errorf("%w: rejection ratio exceeds configured limit", ErrPublicationNotReady)
	}
	return nil
}
