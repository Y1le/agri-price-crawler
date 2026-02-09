package v1

import (
	metav1 "github.com/marmotedu/component-base/pkg/meta/v1"
	"github.com/marmotedu/component-base/pkg/validation/field"
	"gorm.io/gorm"
)

// User represents a user restful resource. It is also used as gorm model.
type Subscribe struct {

	// May add TypeMeta in the future.
	// metav1.TypeMeta `json:",inline"`

	// Standard object's metadata.
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// Required: true
	Email string `json:"email" gorm:"column:email" validate:"required,email,min=1,max=100"`

	City string `json:"city" gorm:"column:city" validate:"required,min=1,max=100"`
}

// UserList is the whole list of all users which have been stored in stroage.
type SubscribeList struct {
	// May add TypeMeta in the future.
	// metav1.TypeMeta `json:",inline"`

	// Standard list metadata.
	// +optional
	metav1.ListMeta `json:",inline"`

	Items []*Subscribe `json:"items"`
}

// TableName maps to mysql table name.
func (s *Subscribe) TableName() string {
	return "subscribe"
}

// AfterCreate run after create database record.
func (s *Subscribe) AfterCreate(tx *gorm.DB) error {
	//TODO: s.CityID = idutil.GetCityID(s.City)
	return tx.Save(s).Error
}

// Validate validates that a subscribe object is valid.
func (s *Subscribe) Validate() field.ErrorList {
	//TODO：email格式正确

	return nil
}
