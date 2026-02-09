package v1

import (
	"github.com/marmotedu/component-base/pkg/validation"
	"github.com/marmotedu/component-base/pkg/validation/field"
	"gorm.io/gorm"
)

type Price struct {
	gorm.Model
	// ID                uint64    `gorm:"primarykey"`
	FirstCateID       uint64  `gorm:"column:first_cate_id"`
	SecondCateID      uint64  `gorm:"column:second_cate_id"`
	CateID            uint64  `gorm:"column:cate_id;index"`
	CateName          string  `gorm:"column:cate_name;type:varchar(100)"`
	BreedName         string  `gorm:"column:breed_name;type:varchar(100)"`
	MinPrice          float64 `gorm:"column:min_price;type:decimal(10,2)"`
	MaxPrice          float64 `gorm:"column:max_price;type:decimal(10,2)"`
	AvgPrice          float64 `gorm:"column:avg_price;type:decimal(10,2)"`
	WeightingAvgPrice float64 `gorm:"column:weighting_avg_price;type:decimal(10,2)"`
	UpDownPrice       float64 `gorm:"column:up_down_price;type:decimal(10,2)"`
	Increase          float64 `gorm:"column:increase;type:decimal(10,4)"`
	Unit              string  `gorm:"column:unit;type:varchar(20)"`
	AddressDetail     string  `gorm:"column:address_detail;type:varchar(200)"`
	ProvinceID        uint32  `gorm:"column:province_id"`
	CityID            uint32  `gorm:"column:city_id"`
	AreaID            uint32  `gorm:"column:area_id"`
	StatisNum         uint32  `gorm:"column:statis_num"`
	SourceType        string  `gorm:"column:sourse_type;type:varchar(20)"` // 保留原始字段名
	Trend             int8    `gorm:"column:trend"`
	TraceID           string  `gorm:"column:trace_id;type:varchar(64)"`
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
