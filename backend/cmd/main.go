package main

import (
	"fmt"
	"log"

	_interface "github.com/yui666a/rakuten-api-leverage-exchange/backend/interface"
	"github.com/yui666a/rakuten-api-leverage-exchange/backend/interface/controllers"
	"github.com/yui666a/rakuten-api-leverage-exchange/backend/infrastructure/api"
	"github.com/yui666a/rakuten-api-leverage-exchange/backend/infrastructure/config"
	"github.com/yui666a/rakuten-api-leverage-exchange/backend/usecase"
)

func main() {
	// Load configuration
	cfg := config.Load()

	// Initialize repositories
	rakutenClient := api.NewRakutenClient(cfg.Rakuten.BaseURL, cfg.Rakuten.APIKey)
	orderRepo := api.NewInMemoryOrderRepository()
	accountRepo := api.NewInMemoryAccountRepository()

	// Initialize usecases
	marketUsecase := usecase.NewMarketUsecase(rakutenClient)
	orderUsecase := usecase.NewOrderUsecase(orderRepo, accountRepo)

	// Initialize controllers
	marketController := controllers.NewMarketController(marketUsecase)
	orderController := controllers.NewOrderController(orderUsecase)
	accountController := controllers.NewAccountController(accountRepo)

	// Setup router
	router := _interface.NewRouter(marketController, orderController, accountController)
	engine := router.SetupRoutes()

	// Start server
	addr := fmt.Sprintf("%s:%s", cfg.Server.Host, cfg.Server.Port)
	log.Printf("Starting server on %s", addr)
	if err := engine.Run(addr); err != nil {
		log.Fatalf("Failed to start server: %v", err)
	}
}
