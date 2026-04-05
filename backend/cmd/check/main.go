package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/joho/godotenv"
	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/infrastructure/rakuten"
)

func main() {
	_ = godotenv.Load()

	apiKey := os.Getenv("RAKUTEN_API_KEY")
	apiSecret := os.Getenv("RAKUTEN_API_SECRET")
	baseURL := "https://exchange.rakuten-wallet.co.jp"

	if apiKey == "" || apiSecret == "" {
		log.Fatal("RAKUTEN_API_KEY and RAKUTEN_API_SECRET must be set")
	}

	client := rakuten.NewRESTClient(baseURL, apiKey, apiSecret)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	assets, err := client.GetAssets(ctx)
	if err != nil {
		log.Fatalf("GetAssets failed: %v", err)
	}

	out, _ := json.MarshalIndent(assets, "", "  ")
	fmt.Println(string(out))
}
