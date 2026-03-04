package craw

import (
	"net/http"
	"path/filepath"

	"github.com/Y1le/agri-price-crawler/internal/craw/controller/hnprice"
	"github.com/Y1le/agri-price-crawler/internal/craw/controller/subscribe"
	"github.com/Y1le/agri-price-crawler/internal/craw/controller/user"
	"github.com/Y1le/agri-price-crawler/internal/craw/store/mysql"
	"github.com/Y1le/agri-price-crawler/internal/pkg/alert"
	"github.com/Y1le/agri-price-crawler/internal/pkg/code"
	"github.com/Y1le/agri-price-crawler/internal/pkg/middleware"
	"github.com/Y1le/agri-price-crawler/internal/pkg/middleware/auth"
	"github.com/gin-gonic/gin"
	"github.com/marmotedu/component-base/pkg/core"
	"github.com/marmotedu/errors"
)

func initRouter(g *gin.Engine, alertServer *alert.Server) {
	installMiddleware(g, alertServer)
	installStaticRouter(g)
	installController(g, alertServer)
}

func installMiddleware(g *gin.Engine, alertServer *alert.Server) {
	// Prometheus metrics middleware needs to be wrapped for gin
	if alertServer != nil {
		// Use a wrapper to integrate HTTP middleware with gin
		g.Use(func(c *gin.Context) {
			// Pass through middleware
			nextHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				c.Next()
			})
			alertServer.MetricsMiddleware()(nextHandler).ServeHTTP(c.Writer, c.Request)
		})
	}
}

func installStaticRouter(g *gin.Engine) {
	// 1. 配置静态文件目录（存放HTML/CSS/JS）
	g.Static("/static", "./static")
	// 2. 根路径返回主页面
	g.GET("/", func(c *gin.Context) {
		c.HTML(http.StatusOK, "index.html", nil)
	})
	// 3. 加载HTML模板（如果HTML放在templates目录）
	g.LoadHTMLFiles(filepath.Join("templates", "index.html"))
}

func installController(g *gin.Engine, alertServer *alert.Server) *gin.Engine {
	// Middlewares
	jwtStrategy, _ := newJWTAuth().(auth.JWTStrategy)
	g.POST("/login", jwtStrategy.LoginHandler)
	g.POST("/logout", jwtStrategy.LogoutHandler)
	// // Refresh time can be longer than token timeout
	g.POST("/refresh", jwtStrategy.RefreshHandler)

	auto := newAutoAuth()
	g.NoRoute(auto.AuthFunc(), func(c *gin.Context) {
		core.WriteResponse(c, errors.Errorf("%d: %s", code.ErrPageNotFound, "Page not found."), nil)
	})

	// v1 handlers, requiring authentication
	storeIns, err := mysql.GetMySQLFactoryOr(nil)
	if err != nil {
		panic(err)
	}
	v1 := g.Group("/v1")
	{

		// user RESTful resource
		hnpricev1 := v1.Group("/prices")
		{
			priceController := hnprice.NewHNPriceController(storeIns)

			hnpricev1.GET("", priceController.List)

		}

		// user RESTful resource
		userv1 := v1.Group("/users")
		{
			userController := user.NewUserController(storeIns)

			userv1.POST("", userController.Create)
			userv1.Use(auto.AuthFunc(), middleware.Validation())
			userv1.DELETE("", userController.DeleteCollection) // admin api
			userv1.DELETE(":name", userController.Delete)      // admin api
			userv1.PUT(":name/change-password", userController.ChangePassword)
			userv1.PUT(":name", userController.Update)
			userv1.GET("", userController.List)
			userv1.GET(":name", userController.Get) // admin api
		}

		subscribev1 := v1.Group("/subscribes")
		{
			subscribeController := subscribe.NewSubscribeController(storeIns)

			subscribev1.Use(auto.AuthFunc(), middleware.Validation())
			subscribev1.POST("", subscribeController.Create)
			subscribev1.DELETE(":email", subscribeController.Delete)
		}
	}

	// Add alert server metrics endpoint
	if alertServer != nil {
		g.GET("/health/alerts", func(c *gin.Context) {
			status := alertServer.GetStatus()
			c.JSON(http.StatusOK, status)
		})
	}

	return g
}
