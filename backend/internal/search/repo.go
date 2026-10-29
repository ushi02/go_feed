package search

import (
	"context"

	"gorm.io/gorm"
)

type Repo struct{ db *gorm.DB }

func NewRepo(db *gorm.DB) *Repo { return &Repo{db: db} }

// Search runs a FULLTEXT boolean query over entries.search_text. A user sees
// entries from live pool feeds plus their own entries (feeds they subscribe
// to, or starred ones), never another user's private subscriptions.
func (r *Repo) Search(ctx context.Context, userID uint64, query string, limit int) ([]Result, error) {
	results := []Result{}
	err := r.db.WithContext(ctx).Raw(`
SELECT e.id AS entry_id, e.title, e.url, e.summary, e.sort_at,
       f.id AS feed_id, f.title AS feed_title, f.url AS feed_url,
       EXISTS (SELECT 1 FROM subscriptions s WHERE s.user_id = ? AND s.feed_id = f.id) AS subscribed
FROM entries e
JOIN feeds f ON f.id = e.feed_id
LEFT JOIN user_entries ue ON ue.entry_id = e.id AND ue.user_id = ?
WHERE MATCH(e.search_text) AGAINST (? IN BOOLEAN MODE)
  AND ((f.in_pool AND f.disabled_at IS NULL) OR ue.user_id IS NOT NULL)
ORDER BY MATCH(e.search_text) AGAINST (? IN BOOLEAN MODE) DESC, e.sort_at DESC, e.id DESC
LIMIT ?`, userID, userID, query, query, limit).Scan(&results).Error
	return results, err
}
