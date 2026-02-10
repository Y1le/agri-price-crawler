package v1

import (
	"context"

	"github.com/Y1le/agri-price-crawler/internal/craw/store"
	"github.com/Y1le/agri-price-crawler/internal/pkg/code"
	v1 "github.com/Y1le/agri-price-crawler/pkg/api/v1"
	metav1 "github.com/marmotedu/component-base/pkg/meta/v1"
	"github.com/marmotedu/errors"
)

type UserSrv interface {
	Create(ctx context.Context, user *v1.User, opts metav1.CreateOptions) error
	Update(ctx context.Context, user *v1.User, opts metav1.UpdateOptions) error
	Delete(ctx context.Context, username string, opts metav1.DeleteOptions) error
	DeleteCollection(ctx context.Context, usernames []string, opts metav1.DeleteOptions) error
	ChangePassword(ctx context.Context, user *v1.User) error
	Get(ctx context.Context, username string, opts metav1.GetOptions) (*v1.User, error)
	List(ctx context.Context, opts metav1.ListOptions) (*v1.UserList, error)
}

type userService struct {
	store store.Factory
}

var _ UserSrv = (*userService)(nil)

func newUser(srv *service) *userService {
	return &userService{store: srv.store}
}

func (u *userService) List(ctx context.Context, opts metav1.ListOptions) (*v1.UserList, error) {
	return u.store.Users().List(ctx, opts)
}

func (u *userService) DeleteCollection(ctx context.Context, usernames []string, opts metav1.DeleteOptions) error {
	if err := u.store.Users().DeleteCollection(ctx, usernames, opts); err != nil {
		return errors.Errorf("%d: %s", code.ErrDatabase, err.Error())
	}
	return nil
}

func (u *userService) Delete(ctx context.Context, username string, opts metav1.DeleteOptions) error {
	if err := u.store.Users().Delete(ctx, username, opts); err != nil {
		return errors.Errorf("%d: %s", code.ErrDatabase, err.Error())
	}
	return nil
}

func (u *userService) Get(ctx context.Context, username string, opts metav1.GetOptions) (*v1.User, error) {
	user, err := u.store.Users().Get(ctx, username, opts)
	if err != nil {
		return nil, err
	}
	return user, nil
}

func (u *userService) Update(ctx context.Context, user *v1.User, opts metav1.UpdateOptions) error {
	if err := u.store.Users().Update(ctx, user, opts); err != nil {
		return errors.Errorf("%d: %s", code.ErrDatabase, err.Error())
	}
	return nil
}

func (u *userService) ChangePassword(ctx context.Context, user *v1.User) error {
	if err := u.store.Users().Update(ctx, user, metav1.UpdateOptions{}); err != nil {
		return errors.Errorf("%d: %s", code.ErrDatabase, err.Error())
	}

	return nil
}

func (u *userService) Create(ctx context.Context, user *v1.User, opts metav1.CreateOptions) error {
	if err := u.store.Users().Create(ctx, user, opts); err != nil {
		return errors.Errorf("%d: %s", code.ErrDatabase, err.Error())
	}
	return nil
}
