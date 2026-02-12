package middleware

import (
	"net/http"

	"github.com/Y1le/agri-price-crawler/internal/craw/store"
	"github.com/Y1le/agri-price-crawler/internal/pkg/code"
	"github.com/gin-gonic/gin"
	"github.com/marmotedu/component-base/pkg/core"
	metav1 "github.com/marmotedu/component-base/pkg/meta/v1"
	"github.com/marmotedu/errors"
)

// Validation make sure users have the right resource permission and operation.
func Validation() gin.HandlerFunc {
	return func(c *gin.Context) {
		if err := isAdmin(c); err != nil {
			switch c.FullPath() {
			case "/v1/users":
				if c.Request.Method != http.MethodPost {
					core.WriteResponse(c, errors.Errorf("%d: %s", code.ErrPermissionDenied, ""), nil)
					c.Abort()

					return
				}
			case "/v1/users/:name", "/v1/users/:name/change_password":
				username := c.GetString("username")
				if c.Request.Method == http.MethodDelete ||
					(c.Request.Method != http.MethodDelete && username != c.Param("name")) {
					core.WriteResponse(c, errors.Errorf("%d: %s", code.ErrPermissionDenied, ""), nil)
					c.Abort()

					return
				}
			default:
			}
		}

		c.Next()
	}
}

// isAdmin make sure the user is administrator.
// It returns a `github.com/marmotedu/errors.withCode` error.
func isAdmin(c *gin.Context) error {
	username := c.GetString(UsernameKey)
	user, err := store.Client().Users().Get(c, username, metav1.GetOptions{})
	if err != nil {
		return errors.Errorf("%d: %s", code.ErrDatabase, err.Error())
	}

	if user.IsAdmin != 1 {
		return errors.Errorf("%d: %s", code.ErrPermissionDenied, "user %s is not a administrator", username)
	}

	return nil
}
