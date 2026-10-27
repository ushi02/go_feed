package subscription

import "time"

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
