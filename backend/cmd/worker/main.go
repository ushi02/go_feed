package main

import (
	"context"
	"log"
	"os/signal"
	"syscall"

	"github.com/ushi02/go_feed/backend/internal/db"
	"github.com/ushi02/go_feed/backend/internal/worker"
)

func main() {
	database, err := db.NewDB()
	if err != nil {
		log.Fatalf("connect mysql: %v", err)
	}
	if err := db.Migrate(database); err != nil {
		log.Fatalf("migrate database: %v", err)
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	worker.NewWorker(database).Run(ctx)
}
