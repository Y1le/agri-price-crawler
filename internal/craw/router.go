package craw

import (
	"github.com/Y1le/agri-price-crawler/internal/craw/controller/hnprice"
	"github.com/Y1le/agri-price-crawler/internal/craw/controller/subscribe"
	"github.com/Y1le/agri-price-crawler/internal/craw/controller/user"
	"github.com/Y1le/agri-price-crawler/internal/craw/store/mysql"
	"github.com/gin-gonic/gin"
)

func initRouter(g *gin.Engine) {
	installMiddleware(g)
	installController(g)
}

func installMiddleware(g *gin.Engine) {
}

func installController(g *gin.Engine) *gin.Engine {
	// Middlewares.
	// jwtStrategy, _ := newJWTAuth().(auth.JWTStrategy)
	// g.POST("/login", jwtStrategy.LoginHandler)
	// g.POST("/logout", jwtStrategy.LogoutHandler)
	// // Refresh time can be longer than token timeout
	// g.POST("/refresh", jwtStrategy.RefreshHandler)

	// auto := newAutoAuth()
	// g.NoRoute(auto.AuthFunc(), func(c *gin.Context) {
	// 	core.WriteResponse(c, errors.WithCode(code.ErrPageNotFound, "Page not found."), nil)
	// })

	// v1 handlers, requiring authentication
	storeIns, _ := mysql.GetMySQLFactoryOr(nil)
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
			// userv1.Use(auto.AuthFunc(), middleware.Validation())
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

			subscribev1.POST("", subscribeController.Create)
			subscribev1.DELETE(":email", subscribeController.Delete)
		}
	}

	return g
}
