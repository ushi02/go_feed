package http_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ushi02/go_feed/backend/internal/fetcher"
	"github.com/ushi02/go_feed/backend/internal/subscription"
	"gorm.io/gorm"
)

// TestImportPoolIdempotent imports the blog pool twice inside a transaction
// (rolled back so other tests do not see 1400 extra feeds) and checks that no
// duplicates appear and an existing feed keeps its title.
func TestImportPoolIdempotent(t *testing.T) {
	skipShort(t)
	errRollback := errors.New("rollback")
	err := database.Transaction(func(tx *gorm.DB) error {
		// A feed someone already subscribed to, which is also in the pool.
		url := "http://feeds.feedburner.com/ruanyifeng"
		existing := subscription.Feed{URL: url, URLHash: fetcher.Hash(url), Title: "Fetched title", NextFetchAt: time.Now().UTC()}
		if err := tx.Create(&existing).Error; err != nil {
			t.Fatal(err)
		}
		svc := subscription.NewService(subscription.NewRepo(tx), nil)
		imported, _, err := svc.ImportPool(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		var first int64
		tx.Model(&subscription.Feed{}).Where("in_pool").Count(&first)
		if _, _, err := svc.ImportPool(context.Background()); err != nil {
			t.Fatal(err)
		}
		var second, total int64
		tx.Model(&subscription.Feed{}).Where("in_pool").Count(&second)
		tx.Model(&subscription.Feed{}).Count(&total)
		t.Logf("imported %d rows, %d pool feeds", imported, second)
		if first < 1400 || second != first {
			t.Fatalf("pool feeds: first import %d, second import %d", first, second)
		}
		var got subscription.Feed
		tx.Where("id = ?", existing.ID).First(&got)
		if !got.InPool || got.Title != "Fetched title" {
			t.Fatalf("existing feed after import: in_pool=%v title=%q", got.InPool, got.Title)
		}
		return errRollback
	})
	if !errors.Is(err, errRollback) {
		t.Fatal(err)
	}
}
