package entry

import (
	"errors"
	"time"
)

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

// UserEntry is one entry delivered to one user (fan-out on write).
type UserEntry struct {
	UserID    uint64
	EntryID   uint64
	ReadAt    *time.Time
	StarredAt *time.Time
}

// EntryView is an entry as one user sees it in the list.
type EntryView struct {
	ID          uint64     `json:"id"`
	FeedID      uint64     `json:"feed_id"`
	FeedTitle   string     `json:"feed_title"`
	Title       string     `json:"title"`
	URL         string     `json:"url"`
	SortAt      time.Time  `json:"sort_at"`
	PublishedAt *time.Time `json:"published_at"`
	Read        bool       `json:"read"`
	StarredAt   *time.Time `json:"starred_at"`
}

// EntryDetail is one entry with its body, for the reader view.
type EntryDetail struct {
	EntryView
	Content string `json:"content"`
}

type ListQuery struct {
	UserID         uint64
	FolderID       *uint64
	SubscriptionID *uint64
	Filter         string // "", "all", "unread" or "starred"
	CursorTime     *time.Time
	CursorID       uint64
	Limit          int
}

// NewSubscriptionBacklog is how many recent entries a new subscriber starts with.
const NewSubscriptionBacklog = 20

var ErrNotFound = errors.New("not found")
