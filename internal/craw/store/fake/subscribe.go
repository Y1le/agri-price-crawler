package fake

import (
	"context"

	metav1 "github.com/marmotedu/component-base/pkg/meta/v1"
	"github.com/marmotedu/errors"

	"github.com/Y1le/agri-price-crawler/internal/pkg/code"
	v1 "github.com/Y1le/agri-price-crawler/pkg/api/v1"
)

type subscribes struct {
	ds *datastore
}

func newSubscribes(ds *datastore) *subscribes {
	return &subscribes{ds}
}

// Create creates a new subscribe.
func (s *subscribes) Create(ctx context.Context, subscribe *v1.Subscribe, opts metav1.CreateOptions) error {
	s.ds.Lock()
	defer s.ds.Unlock()

	for _, item := range s.ds.subscribes {
		if item.Email == subscribe.Email {
			return errors.WithCode(code.ErrUserAlreadyExist, "record already exist")
		}
	}
	if len(s.ds.subscribes) > 0 {
		subscribe.ID = s.ds.subscribes[len(s.ds.subscribes)-1].ID + 1
	}
	s.ds.subscribes = append(s.ds.subscribes, subscribe)
	return nil
}

// Update updates the specified subscribe.
func (s *subscribes) List(ctx context.Context, opts metav1.ListOptions) ([]*v1.Subscribe, error) {
	s.ds.Lock()
	defer s.ds.Unlock()
	return s.ds.subscribes, nil
}

// Delete deletes the specified subscribe.
func (s *subscribes) Delete(ctx context.Context, email string, opts metav1.DeleteOptions) error {
	s.ds.Lock()
	defer s.ds.Unlock()

	for i, item := range s.ds.subscribes {
		if item.Email == email {
			s.ds.subscribes = append(s.ds.subscribes[:i], s.ds.subscribes[i+1:]...)
			return nil
		}
	}

	return errors.WithCode(code.ErrUserNotExist, "record not exist")
}
