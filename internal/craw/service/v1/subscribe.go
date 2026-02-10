package v1

import (
	"context"

	store "github.com/Y1le/agri-price-crawler/internal/craw/store"
	"github.com/Y1le/agri-price-crawler/internal/pkg/code"
	v1 "github.com/Y1le/agri-price-crawler/pkg/api/v1"
	metav1 "github.com/marmotedu/component-base/pkg/meta/v1"
	"github.com/marmotedu/errors"
)

type SubscribeSrv interface {
	Create(ctx context.Context, subscribe *v1.Subscribe, opts metav1.CreateOptions) error
	Delete(ctx context.Context, email string, opts metav1.DeleteOptions) error
	List(ctx context.Context, opts metav1.ListOptions) ([]*v1.Subscribe, error)
}

type subscribeService struct {
	store store.Factory
}

var _ SubscribeSrv = (*subscribeService)(nil)

func newSubscribe(srv *service) *subscribeService {
	return &subscribeService{store: srv.store}
}

func (s *subscribeService) Create(ctx context.Context, subscribe *v1.Subscribe, opts metav1.CreateOptions) error {
	if err := s.store.Subscribes().Create(ctx, subscribe, opts); err != nil {
		return errors.Errorf("%d: %s", code.ErrDatabase, err.Error())
	}
	return nil
}

func (s *subscribeService) Delete(ctx context.Context, email string, opts metav1.DeleteOptions) error {
	if err := s.store.Subscribes().Delete(ctx, email, opts); err != nil {
		return errors.Errorf("%d: %s", code.ErrDatabase, err.Error())
	}
	return nil
}

func (s *subscribeService) List(ctx context.Context, opts metav1.ListOptions) ([]*v1.Subscribe, error) {
	return s.store.Subscribes().List(ctx, opts)
}
