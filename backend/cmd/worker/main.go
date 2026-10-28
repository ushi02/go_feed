package main

import (
	"context"
	"log"
	"os/signal"
	"syscall"

	"github.com/ushi02/go_feed/backend/internal/db"
	"github.com/ushi02/go_feed/backend/internal/entry"
	"github.com/ushi02/go_feed/backend/internal/subscription"
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
	pool := subscription.NewService(subscription.NewRepo(database), entry.NewRepo(database))
	imported, skipped, err := pool.ImportPool(ctx)
	if err != nil {
		log.Fatalf("import blog pool: %v", err)
	}
	log.Printf("blog pool: %d feeds imported, %d rows without a valid feed URL skipped", imported, skipped)
	worker.NewWorker(database).Run(ctx)
}
