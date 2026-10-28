package subscription

import (
	"context"
	_ "embed"
	"encoding/csv"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/ushi02/go_feed/backend/internal/fetcher"
)

// poolCSV is a snapshot of blogs-original.csv from
// https://github.com/timqian/chinese-independent-blogs (MIT, see pool/LICENSE).
// Columns: Introduction, Address, RSS feed, tags.
//
//go:embed pool/blogs-original.csv
var poolCSV string

// parsePool returns one Feed per row with a valid RSS URL. Rows without a
// feed URL cannot be subscribed to, so they are skipped.
func parsePool(r io.Reader) (feeds []Feed, skipped int, err error) {
	cr := csv.NewReader(r)
	cr.TrimLeadingSpace = true
	cr.FieldsPerRecord = -1
	rows, err := cr.ReadAll()
	if err != nil {
		return nil, 0, err
	}
	if len(rows) == 0 {
		return nil, 0, fmt.Errorf("empty blog pool CSV")
	}
	now := time.Now().UTC()
	for _, row := range rows[1:] {
		if len(row) < 3 {
			skipped++
			continue
		}
		canonical, err := fetcher.Canonical(row[2])
		if err != nil {
			skipped++
			continue
		}
		feeds = append(feeds, Feed{
			URL: canonical, URLHash: fetcher.Hash(canonical),
			Title: truncate(strings.TrimSpace(row[0]), 512), SiteURL: strings.TrimSpace(row[1]),
			NextFetchAt: now, InPool: true,
		})
	}
	return feeds, skipped, nil
}

// ImportPool loads the embedded blog pool into feeds. It is idempotent, so the
// Worker runs it on every start.
func (s *Service) ImportPool(ctx context.Context) (imported, skipped int, err error) {
	feeds, skipped, err := parsePool(strings.NewReader(poolCSV))
	if err != nil {
		return 0, 0, err
	}
	return len(feeds), skipped, s.repo.UpsertPoolFeeds(ctx, feeds)
}
