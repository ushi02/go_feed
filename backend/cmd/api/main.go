package main

import (
	"log"
	"os"

	"github.com/ushi02/go_feed/backend/internal/db"
	apihttp "github.com/ushi02/go_feed/backend/internal/http"
)

func main() {
	database, err := db.NewDB()
	if err != nil {
		log.Fatalf("connect mysql: %v", err)
	}
	if err := db.Migrate(database); err != nil {
		log.Fatalf("migrate database: %v", err)
	}
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	if err := apihttp.NewRouter(database).Run(":" + port); err != nil {
		log.Fatalf("run server: %v", err)
	}
}
