package store

import (
	"context"

	v1 "github.com/Y1le/agri-price-crawler/pkg/api/v1"
	metav1 "github.com/marmotedu/component-base/pkg/meta/v1"
)

type SubscribeStore interface {
	Create(ctx context.Context, subscribe *v1.Subscribe, opts metav1.CreateOptions) error
	Delete(ctx context.Context, email string, opts metav1.DeleteOptions) error
	List(ctx context.Context, opts metav1.ListOptions) ([]*v1.Subscribe, error)
}
