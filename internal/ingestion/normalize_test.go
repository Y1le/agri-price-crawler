package ingestion_test

import (
	"errors"
	"testing"
	"time"

	"github.com/Y1le/agri-price-crawler/internal/ingestion"
	"github.com/google/uuid"
)

func TestNormalizeConvertsYuanPerJinUsingMappedProductAndRegion(t *testing.T) {
	productID := uuid.MustParse("20000000-0000-0000-0000-000000000001")
	regionID := uuid.MustParse("30000000-0000-0000-0000-000000000003")
	date, err := ingestion.ParseBusinessDate("2026-07-25")
	if err != nil {
		t.Fatal(err)
	}
	result := ingestion.Normalize(ingestion.SourceRecord{
		Key:        "row-1",
		CategoryID: "veg",
		BreedID:    "tomato",
		ProvinceID: "37",
		CityID:     "07",
		DistrictID: "83",
		MarketName: "寿光蔬菜市场",
		MinPrice:   "1.20",
		MaxPrice:   "1.60",
		Average:    "1.40",
		Unit:       "元/斤",
		ObservedAt: time.Date(2026, 7, 25, 3, 0, 0, 0, time.UTC),
	}, date, mappingStub{productID: productID, regionID: regionID})
	if result.Rejection != nil {
		t.Fatalf("Normalize() rejection = %+v", result.Rejection)
	}
	if result.Observation == nil || result.Observation.ProductID != productID || result.Observation.RegionID != regionID ||
		result.Observation.MinPrice.String() != "2.4000" || result.Observation.MaxPrice.String() != "3.2000" ||
		result.Observation.AveragePrice.String() != "2.8000" || result.Observation.Unit != "CNY/kg" {
		t.Fatalf("Normalize() observation = %+v", result.Observation)
	}
}

func TestBuildSummariesUsesWeightedAverageOnlyWhenEverySampleCountIsPositive(t *testing.T) {
	productID := uuid.MustParse("20000000-0000-0000-0000-000000000001")
	regionID := uuid.MustParse("30000000-0000-0000-0000-000000000003")
	date, err := ingestion.ParseBusinessDate("2026-07-25")
	if err != nil {
		t.Fatal(err)
	}
	two, four := int64(2), int64(4)
	summaries, err := ingestion.BuildSummaries([]ingestion.AcceptedObservation{
		observation(productID, regionID, date, "2", "4", "3", &two),
		observation(productID, regionID, date, "3", "5", "4", &four),
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(summaries) != 1 || summaries[0].MinPrice.String() != "2.0000" || summaries[0].MaxPrice.String() != "5.0000" ||
		summaries[0].AveragePrice.String() != "3.6667" || summaries[0].ObservationCount != 2 || summaries[0].WeightedSampleCount != 6 {
		t.Fatalf("weighted summary = %+v", summaries)
	}

	summaries, err = ingestion.BuildSummaries([]ingestion.AcceptedObservation{
		observation(productID, regionID, date, "2", "4", "3", &two),
		observation(productID, regionID, date, "3", "5", "4", nil),
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(summaries) != 1 || summaries[0].AveragePrice.String() != "3.5000" || summaries[0].WeightedSampleCount != 0 {
		t.Fatalf("simple summary = %+v", summaries)
	}
}

func TestNormalizeUsesStableRejectionCodesAndFivePercentGate(t *testing.T) {
	date, err := ingestion.ParseBusinessDate("2026-07-25")
	if err != nil {
		t.Fatal(err)
	}
	record := ingestion.SourceRecord{
		Key: "row-1", CategoryID: "veg", BreedID: "tomato", ProvinceID: "37", CityID: "07", DistrictID: "83",
		MarketName: "寿光蔬菜市场", MinPrice: "1", MaxPrice: "2", Average: "1.5", Unit: "元/kg",
		ObservedAt: time.Date(2026, 7, 25, 3, 0, 0, 0, time.UTC),
	}
	productID, regionID := uuid.New(), uuid.New()
	for name, check := range map[string]func(ingestion.SourceRecord, ingestion.MappingLookup) string{
		"unknown product": func(_ ingestion.SourceRecord, _ ingestion.MappingLookup) string {
			return ingestion.RejectUnknownProductMapping
		},
		"unknown region": func(_ ingestion.SourceRecord, _ ingestion.MappingLookup) string {
			return ingestion.RejectUnknownRegionMapping
		},
		"invalid unit": func(candidate ingestion.SourceRecord, _ ingestion.MappingLookup) string {
			candidate.Unit = "元/箱"
			return ingestion.Normalize(candidate, date, mappingStub{productID: productID, regionID: regionID}).Rejection.Code
		},
		"invalid price": func(candidate ingestion.SourceRecord, _ ingestion.MappingLookup) string {
			candidate.Average = "3"
			return ingestion.Normalize(candidate, date, mappingStub{productID: productID, regionID: regionID}).Rejection.Code
		},
		"invalid timestamp": func(candidate ingestion.SourceRecord, _ ingestion.MappingLookup) string {
			candidate.ObservedAt = time.Time{}
			return ingestion.Normalize(candidate, date, mappingStub{productID: productID, regionID: regionID}).Rejection.Code
		},
	} {
		t.Run(name, func(t *testing.T) {
			var got string
			switch name {
			case "unknown product":
				got = ingestion.Normalize(record, date, missingProductMapping{regionID: regionID}).Rejection.Code
			case "unknown region":
				got = ingestion.Normalize(record, date, missingRegionMapping{productID: productID}).Rejection.Code
			default:
				got = check(record, nil)
			}
			want := check(record, nil)
			if got != want {
				t.Fatalf("rejection = %q, want %q", got, want)
			}
		})
	}
	if err := ingestion.ValidatePublication(ingestion.BatchCounts{Fetched: 20, Accepted: 19, Rejected: 1}, 500); err != nil {
		t.Fatalf("exactly five percent was rejected: %v", err)
	}
	if err := ingestion.ValidatePublication(ingestion.BatchCounts{Fetched: 20, Accepted: 18, Rejected: 2}, 500); !errors.Is(err, ingestion.ErrPublicationNotReady) {
		t.Fatalf("over-five-percent error = %v", err)
	}
}

func observation(productID, regionID uuid.UUID, date ingestion.BusinessDate, min, max, average string, sampleCount *int64) ingestion.AcceptedObservation {
	minPrice, _ := ingestion.ParseMoney(min)
	maxPrice, _ := ingestion.ParseMoney(max)
	averagePrice, _ := ingestion.ParseMoney(average)
	return ingestion.AcceptedObservation{
		ProductID: productID, RegionID: regionID, BusinessDate: date,
		MinPrice: minPrice, MaxPrice: maxPrice, AveragePrice: averagePrice, SampleCount: sampleCount,
	}
}

type mappingStub struct {
	productID uuid.UUID
	regionID  uuid.UUID
}

type missingProductMapping struct{ regionID uuid.UUID }

func (m missingProductMapping) ProductID(ingestion.SourceProduct) (uuid.UUID, bool) {
	return uuid.Nil, false
}
func (m missingProductMapping) RegionID(ingestion.SourceRegion) (uuid.UUID, bool) {
	return m.regionID, true
}

type missingRegionMapping struct{ productID uuid.UUID }

func (m missingRegionMapping) ProductID(ingestion.SourceProduct) (uuid.UUID, bool) {
	return m.productID, true
}
func (m missingRegionMapping) RegionID(ingestion.SourceRegion) (uuid.UUID, bool) {
	return uuid.Nil, false
}

func (m mappingStub) ProductID(_ ingestion.SourceProduct) (uuid.UUID, bool) {
	return m.productID, true
}

func (m mappingStub) RegionID(_ ingestion.SourceRegion) (uuid.UUID, bool) {
	return m.regionID, true
}
