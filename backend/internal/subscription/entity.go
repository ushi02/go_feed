package subscription

import (
	"errors"
	"time"
)

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
	InPool        bool       `json:"in_pool"`
	DisabledAt    *time.Time `json:"disabled_at"`
}

type Folder struct {
	ID     uint64 `json:"id"`
	UserID uint64 `json:"-"`
	Name   string `json:"name"`
}

type Subscription struct {
	ID       uint64  `json:"id"`
	UserID   uint64  `json:"-"`
	FeedID   uint64  `json:"feed_id"`
	FolderID *uint64 `json:"folder_id"`
	Feed     Feed    `gorm:"foreignKey:FeedID" json:"feed"`
}

// ImportItem is one feed read from an OPML file.
type ImportItem struct{ URL, Folder string }

var (
	ErrNotFound          = errors.New("not found")
	ErrAlreadySubscribed = errors.New("already subscribed")
	ErrInvalidFolder     = errors.New("invalid folder")
	ErrRefreshedRecently = errors.New("feed was refreshed recently")
)

// InvalidURLError carries the reason a feed URL was rejected, shown to the user as is.
type InvalidURLError struct{ Err error }

func (e InvalidURLError) Error() string { return e.Err.Error() }
