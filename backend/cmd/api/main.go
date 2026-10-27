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
	router := apihttp.NewRouter(database)
	// STATIC_DIR points at the built frontend; empty in local Vite development.
	if dir := os.Getenv("STATIC_DIR"); dir != "" {
		apihttp.ServeFrontend(router, dir)
	}
	if err := router.Run(":" + port); err != nil {
		log.Fatalf("run server: %v", err)
	}
}
