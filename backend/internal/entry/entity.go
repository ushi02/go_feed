package entry

import "time"

type Entry struct {
	ID          uint64     `json:"id"`
	FeedID      uint64     `json:"feed_id"`
	GUIDHash    []byte     `json:"-"`
	Title       string     `json:"title"`
	URL         string     `json:"url"`
	Content     string     `json:"content"`
	Summary     string     `json:"summary"`
	PublishedAt *time.Time `json:"published_at"`
	SortAt      time.Time  `json:"sort_at"`
	CreatedAt   time.Time  `json:"created_at"`
}

type EntryState struct {
	UserID       uint64
	EntryID      uint64
	ReadOverride *bool
	StarredAt    *time.Time
}
