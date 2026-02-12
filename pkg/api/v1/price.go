package v1

import (
	"time"

	"github.com/marmotedu/component-base/pkg/validation"
	"github.com/marmotedu/component-base/pkg/validation/field"
	"gorm.io/gorm"
)

type Price struct {
	ID                uint64    `json:"id,omitempty" gorm:"primary_key;AUTO_INCREMENT;column:id"`
	CreatedAt         time.Time `json:"createdAt,omitempty" gorm:"column:createdAt"`
	UpdatedAt         time.Time `json:"updatedAt,omitempty" gorm:"column:updatedAt"`
	FirstCateID       uint64    `gorm:"column:firstCateId"`
	SecondCateID      uint64    `gorm:"column:secondCateId"`
	CateID            uint64    `gorm:"column:cateId;index"`
	CateName          string    `gorm:"column:cateName;type:varchar(100)"`
	BreedName         string    `gorm:"column:breedName;type:varchar(100)"`
	MinPrice          float64   `gorm:"column:minPrice;type:decimal(10,2)"`
	MaxPrice          float64   `gorm:"column:maxPrice;type:decimal(10,2)"`
	AvgPrice          float64   `gorm:"column:avgPrice;type:decimal(10,2)"`
	WeightingAvgPrice float64   `gorm:"column:weightingAvgPrice;type:decimal(10,2)"`
	UpDownPrice       float64   `gorm:"column:upDownPrice;type:decimal(10,2)"`
	Increase          float64   `gorm:"column:increase;type:decimal(10,4)"`
	Unit              string    `gorm:"column:unit;type:varchar(20)"`
	AddressDetail     string    `gorm:"column:addressDetail;type:varchar(200)"`
	ProvinceID        uint32    `gorm:"column:provinceId"`
	CityID            uint32    `gorm:"column:cityId"`
	AreaID            uint32    `gorm:"column:areaId"`
	StatisNum         uint32    `gorm:"column:statisNum"`
	SourceType        string    `gorm:"column:sourceType;type:varchar(20)"`
	Trend             int8      `gorm:"column:trend"`
	TraceID           string    `gorm:"column:traceId;type:varchar(64)"`
}

type PriceList struct {
	// Standard list metadata.
	// +optional
	TotalCount int64 `json:"totalCount,omitempty"`

	Items []*Price `json:"items,omitempty"`
}

func (p *PriceList) TableName() string {
	return "price"
}

func (p *Price) AfterCreate(tx *gorm.DB) error {
	// TODO:
	return nil
}

// Validate validates that a price object is valid.
func (p *Price) Validate() field.ErrorList {
	val := validation.NewValidator(p)

	return val.Validate()
}
