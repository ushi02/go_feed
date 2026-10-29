package main

import (
	"log"
	"os"

	"github.com/ushi02/go_feed/backend/internal/db"
	apihttp "github.com/ushi02/go_feed/backend/internal/http"
	"github.com/ushi02/go_feed/backend/internal/llm"
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
	// The chatbot is optional: without a key the rest of the app still runs.
	var llmClient llm.Client
	if ds, err := llm.NewDeepSeekFromEnv(); err != nil {
		log.Printf("chatbot disabled: %v", err)
	} else {
		llmClient = ds
	}
	router := apihttp.NewRouter(database, llmClient)
	// STATIC_DIR points at the built frontend; empty in local Vite development.
	if dir := os.Getenv("STATIC_DIR"); dir != "" {
		apihttp.ServeFrontend(router, dir)
	}
	if err := router.Run(":" + port); err != nil {
		log.Fatalf("run server: %v", err)
	}
}
