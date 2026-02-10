package mysql

import (
	"context"

	"github.com/Y1le/agri-price-crawler/internal/pkg/code"
	v1 "github.com/Y1le/agri-price-crawler/pkg/api/v1"
	metav1 "github.com/marmotedu/component-base/pkg/meta/v1"
	"github.com/marmotedu/errors"
	"gorm.io/gorm"
)

type Subscribes struct {
	db *gorm.DB
}

func newSubscribes(db *gorm.DB) *Subscribes {
	return &Subscribes{db: db}
}

func (s *Subscribes) Create(ctx context.Context, subscribe *v1.Subscribe, opts metav1.CreateOptions) error {
	return s.db.WithContext(ctx).Create(subscribe).Error
}

func (s *Subscribes) Delete(ctx context.Context, email string, opts metav1.DeleteOptions) error {
	if opts.Unscoped {
		s.db = s.db.Unscoped()
	}
	err := s.db.Where("email = ?", email).Delete(&v1.Subscribe{}).Error
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return errors.Errorf("%d: %s", code.ErrDatabase, err.Error())
	}
	return nil
}

func (s *Subscribes) List(ctx context.Context, opts metav1.ListOptions) ([]*v1.Subscribe, error) {
	var subscribes []*v1.Subscribe
	d := s.db.Find(&subscribes)
	return subscribes, d.Error
}
