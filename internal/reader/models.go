package reader

import "time"

type User struct {
	ID           uint64    `json:"id"`
	Username     string    `json:"username"`
	PasswordHash string    `json:"-"`
	CreatedAt    time.Time `json:"created_at"`
}

type Session struct {
	TokenHash string
	UserID    uint64
	ExpiresAt time.Time
}

type Feed struct {
	ID            uint64     `json:"id"`
	URL           string     `json:"url"`
	URLHash       []byte     `json:"-"`
	Title         string     `json:"title"`
	SiteURL       string     `json:"site_url"`
	ETag          string     `gorm:"column:etag" json:"-"`
	LastModified  string     `json:"-"`
	NextFetchAt   time.Time  `json:"next_fetch_at"`
	LeaseUntil    *time.Time `json:"-"`
	LeaseToken    *string    `json:"-"`
	LastFetchedAt *time.Time `json:"last_fetched_at"`
	LastError     string     `json:"last_error"`
	FailureCount  int        `json:"-"`
	Initialized   bool       `json:"-"`
}

type Folder struct {
	ID     uint64 `json:"id"`
	UserID uint64 `json:"-"`
	Name   string `json:"name"`
}

type Subscription struct {
	ID                 uint64  `json:"id"`
	UserID             uint64  `json:"-"`
	FeedID             uint64  `json:"feed_id"`
	FolderID           *uint64 `json:"folder_id"`
	ReadThroughEntryID uint64  `json:"-"`
	PendingBaseline    bool    `json:"-"`
	Feed               Feed    `gorm:"foreignKey:FeedID" json:"feed"`
}

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
