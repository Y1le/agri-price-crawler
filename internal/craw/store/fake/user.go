package fake

import (
	"context"
	"strings"

	"github.com/Y1le/agri-price-crawler/internal/pkg/code"
	"github.com/Y1le/agri-price-crawler/internal/pkg/util/gormutil"
	reflectutil "github.com/Y1le/agri-price-crawler/internal/pkg/util/reflect"
	v1 "github.com/Y1le/agri-price-crawler/pkg/api/v1"
	stringutil "github.com/Y1le/agri-price-crawler/pkg/util/stringutil"
	"github.com/marmotedu/component-base/pkg/fields"
	metav1 "github.com/marmotedu/component-base/pkg/meta/v1"
	"github.com/marmotedu/errors"
)

type users struct {
	ds *datastore
}

func newUsers(ds *datastore) *users {
	return &users{ds: ds}
}

// Create creates a new user account.
func (u *users) Create(ctx context.Context, user *v1.User, opts metav1.CreateOptions) error {
	u.ds.Lock()
	defer u.ds.Unlock()

	for _, u := range u.ds.users {
		if u.Name == user.Name {
			return errors.WithCode(code.ErrUserAlreadyExist, "user already exist")
		}
	}

	if len(u.ds.users) > 0 {
		user.ID = u.ds.users[len(u.ds.users)-1].ID + 1
	}
	u.ds.users = append(u.ds.users, user)

	return nil
}

// Update updates an user account informateion.
func (u *users) Update(ctx context.Context, user *v1.User, opts metav1.UpdateOptions) error {
	u.ds.Lock()
	defer u.ds.Unlock()

	for _, u := range u.ds.users {
		if u.Name == user.Name {
			if _, err := reflectutil.CopyObj(user, u, nil); err != nil {
				return errors.Wrap(err, "copy user failed")
			}
		}
	}

	return nil
}

// Delete deletes an user account.
func (u *users) Delete(ctx context.Context, name string, opts metav1.DeleteOptions) error {
	u.ds.Lock()
	defer u.ds.Unlock()

	users := u.ds.users
	u.ds.users = make([]*v1.User, 0)
	userExist := false
	for _, user := range users {
		if user.Name == name {
			userExist = true
			continue
		}

		u.ds.users = append(u.ds.users, user)
	}
	if !userExist {
		return errors.WithCode(code.ErrUserNotExist, "user not exist")
	}
	return nil
}

// DeleteCollection batch deletes user accounts.
func (u *users) DeleteCollection(ctx context.Context, usernames []string, opts metav1.DeleteOptions) error {
	u.ds.Lock()
	defer u.ds.Unlock()

	users := u.ds.users
	u.ds.users = make([]*v1.User, 0)
	ExistUser := make([]string, 0)
	for _, user := range users {
		if stringutil.StringIn(user.Name, usernames) {
			ExistUser = append(ExistUser, user.Name)
			continue
		}
	}
	if len(ExistUser) != len(usernames) {
		u.ds.users = users
		for _, name := range usernames {
			if !stringutil.StringIn(name, ExistUser) {
				return errors.WithCode(code.ErrUserNotExist, "user not exist")
			}
		}
	}
	return nil
}

// Get return an user by the user identifier.
func (u *users) Get(ctx context.Context, name string, opts metav1.GetOptions) (*v1.User, error) {
	u.ds.RLock()
	defer u.ds.RUnlock()

	for _, user := range u.ds.users {
		if user.Name == name {
			return user, nil
		}
	}
	return nil, errors.WithCode(code.ErrUserNotExist, "user not exist")
}

// List return all user accounts.
func (u *users) List(ctx context.Context, opts metav1.ListOptions) (*v1.UserList, error) {
	u.ds.RLock()
	defer u.ds.RUnlock()

	ol := gormutil.Unpointer(opts.Offset, opts.Limit)
	selector, _ := fields.ParseSelector(opts.FieldSelector)
	name, _ := selector.RequiresExactMatch("name")

	users := make([]*v1.User, 0)
	i := 0
	for _, user := range u.ds.users {
		if i == ol.Limit {
			break
		}
		if !strings.Contains(user.Name, name) {
			continue
		}
		users = append(users, user)
		i++
	}

	return &v1.UserList{
		ListMeta: metav1.ListMeta{
			TotalCount: int64(len(u.ds.users)),
		},
		Items: users,
	}, nil
}
