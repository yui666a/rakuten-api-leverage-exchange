package _interface

import (
	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
	"github.com/yui666a/rakuten-api-leverage-exchange/backend/interface/controllers"
)

// Router sets up HTTP routes
type Router struct {
	marketController  *controllers.MarketController
	orderController   *controllers.OrderController
	accountController *controllers.AccountController
}

// NewRouter creates a new Router instance
func NewRouter(
	marketController *controllers.MarketController,
	orderController *controllers.OrderController,
	accountController *controllers.AccountController,
) *Router {
	return &Router{
		marketController:  marketController,
		orderController:   orderController,
		accountController: accountController,
	}
}

// SetupRoutes configures all application routes
func (r *Router) SetupRoutes() *gin.Engine {
	router := gin.Default()

	// CORS middleware
	config := cors.DefaultConfig()
	config.AllowOrigins = []string{"http://localhost:3000", "http://localhost:5173"}
	config.AllowMethods = []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"}
	config.AllowHeaders = []string{"Origin", "Content-Type", "Authorization"}
	router.Use(cors.New(config))

	// Health check endpoint
	router.GET("/health", func(c *gin.Context) {
		c.JSON(200, gin.H{
			"status": "ok",
		})
	})

	// API routes
	api := router.Group("/api")
	{
		// Market routes
		markets := api.Group("/markets")
		{
			markets.GET("", r.marketController.GetAllMarkets)
			markets.GET("/:symbol", r.marketController.GetMarket)
		}

		// Order routes
		orders := api.Group("/orders")
		{
			orders.POST("", r.orderController.CreateOrder)
			orders.GET("", r.orderController.GetOrders)
			orders.GET("/:id", r.orderController.GetOrder)
			orders.DELETE("/:id", r.orderController.CancelOrder)
		}

		// Account routes
		accounts := api.Group("/accounts")
		{
			accounts.GET("/:id", r.accountController.GetAccount)
		}
	}

	return router
}
