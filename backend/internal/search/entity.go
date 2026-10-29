package search

import (
	"errors"
	"time"
)

// Result is one matching entry. Pool entries the user has no row for are not
// readable in the app, so the page links to the original URL instead.
type Result struct {
	EntryID    uint64    `json:"entry_id"`
	Title      string    `json:"title"`
	URL        string    `json:"url"`
	Summary    string    `json:"summary"`
	SortAt     time.Time `json:"sort_at"`
	FeedID     uint64    `json:"feed_id"`
	FeedTitle  string    `json:"feed_title"`
	FeedURL    string    `json:"feed_url"`
	Subscribed bool      `json:"subscribed"`
}

var ErrEmptyQuery = errors.New("empty query")
