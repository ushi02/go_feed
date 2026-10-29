package subscription

import (
	"context"
	"time"

	"github.com/ushi02/go_feed/backend/internal/entry"
	"github.com/ushi02/go_feed/backend/internal/fetcher"
	"gorm.io/gorm"
)

type Service struct {
	repo    *Repo
	entries *entry.Repo
}

func NewService(repo *Repo, entries *entry.Repo) *Service {
	return &Service{repo: repo, entries: entries}
}

func (s *Service) Discover(ctx context.Context, raw string) ([]fetcher.Candidate, error) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	return fetcher.Discover(ctx, raw)
}

// maxVerifyCandidates bounds the fetches one Verify may make.
const maxVerifyCandidates = 3

// Verify finds the feed behind raw (a homepage or a feed URL) and proves it
// exists by fetching and parsing it. Discover alone is not enough: a page can
// advertise a feed link that is broken.
func (s *Service) Verify(ctx context.Context, userID uint64, raw string) (Verified, error) {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	candidates, err := fetcher.Discover(ctx, raw)
	if err != nil {
		return Verified{}, err
	}
	for i, c := range candidates {
		if i == maxVerifyCandidates {
			break
		}
		res, err := fetcher.Fetch(ctx, c.URL, "", "")
		if err != nil || res.Feed == nil {
			continue
		}
		v := Verified{FeedURL: res.FinalURL, Title: res.Feed.Title}
		if v.Title == "" {
			v.Title = c.Title
		}
		// The link comes from the feed's own content; keep it only if it is a plain web address.
		if site, err := fetcher.Canonical(res.Feed.Link); err == nil {
			v.SiteURL = site
		}
		v.Subscribed, err = s.repo.SubscribedToURL(ctx, userID, v.FeedURL)
		return v, err
	}
	return Verified{}, ErrNoFeed
}

func (s *Service) ListFolders(ctx context.Context, userID uint64) ([]Folder, error) {
	return s.repo.ListFolders(ctx, userID)
}

func (s *Service) CreateFolder(ctx context.Context, userID uint64, name string) (Folder, error) {
	f := Folder{UserID: userID, Name: name}
	return f, s.repo.CreateFolder(ctx, &f)
}

func (s *Service) RenameFolder(ctx context.Context, userID, id uint64, name string) error {
	ok, err := s.repo.RenameFolder(ctx, userID, id, name)
	if err == nil && !ok {
		return ErrNotFound
	}
	return err
}

func (s *Service) DeleteFolder(ctx context.Context, userID, id uint64) error {
	ok, err := s.repo.DeleteFolder(ctx, userID, id)
	if err == nil && !ok {
		return ErrNotFound
	}
	return err
}

func (s *Service) validFolder(ctx context.Context, userID uint64, folderID *uint64) error {
	if folderID == nil {
		return nil
	}
	ok, err := s.repo.FolderExists(ctx, userID, *folderID)
	if err == nil && !ok {
		return ErrInvalidFolder
	}
	return err
}

func (s *Service) ListSubscriptions(ctx context.Context, userID uint64) ([]Subscription, error) {
	return s.repo.ListSubscriptions(ctx, userID)
}

// Subscribe adds a feed for the user. If the feed was fetched before, the user
// immediately gets its most recent entries; otherwise the first fetch delivers them.
func (s *Service) Subscribe(ctx context.Context, userID uint64, rawURL string, folderID *uint64) (Subscription, error) {
	if err := s.validFolder(ctx, userID, folderID); err != nil {
		return Subscription{}, err
	}
	canonical, err := fetcher.Canonical(rawURL)
	if err != nil {
		return Subscription{}, InvalidURLError{err}
	}
	var sub Subscription
	err = s.repo.Transaction(ctx, func(tx *gorm.DB) error {
		subs, entries := s.repo.WithTx(tx), s.entries.WithTx(tx)
		feed, err := subs.LockFeed(ctx, canonical)
		if err != nil {
			return err
		}
		already, err := subs.IsSubscribed(ctx, userID, feed.ID)
		if err != nil {
			return err
		}
		if already {
			return ErrAlreadySubscribed
		}
		subscribers, err := subs.CountSubscribers(ctx, feed.ID)
		if err != nil {
			return err
		}
		sub = Subscription{UserID: userID, FeedID: feed.ID, FolderID: folderID}
		created, err := add(ctx, subs, entries, &sub, feed)
		if err != nil {
			return err
		}
		if !created {
			return ErrAlreadySubscribed
		}
		// Fetch now if nobody was reading it yet, or if it was judged dead.
		if subscribers == 0 || feed.DisabledAt != nil {
			return subs.ScheduleNow(ctx, feed.ID)
		}
		return nil
	})
	return sub, err
}

// add creates the subscription and, for a feed fetched before, delivers its backlog.
func add(ctx context.Context, subs *Repo, entries *entry.Repo, sub *Subscription, feed Feed) (bool, error) {
	created, err := subs.CreateSubscription(ctx, sub)
	if err != nil || !created {
		return created, err
	}
	if feed.Initialized {
		return true, entries.Backfill(ctx, feed.ID, &sub.UserID)
	}
	return true, nil
}

func (s *Service) Move(ctx context.Context, userID, id uint64, folderID *uint64) error {
	if err := s.validFolder(ctx, userID, folderID); err != nil {
		return err
	}
	ok, err := s.repo.MoveSubscription(ctx, userID, id, folderID)
	if err == nil && !ok {
		return ErrNotFound
	}
	return err
}

func (s *Service) Unsubscribe(ctx context.Context, userID, id uint64) error {
	return s.repo.Transaction(ctx, func(tx *gorm.DB) error {
		subs := s.repo.WithTx(tx)
		sub, err := subs.FindSubscription(ctx, userID, id)
		if err != nil {
			return err
		}
		if err := s.entries.WithTx(tx).RemoveFeed(ctx, userID, sub.FeedID); err != nil {
			return err
		}
		return subs.DeleteSubscription(ctx, &sub)
	})
}

// Refresh asks the Worker to fetch now. The cooldown is per feed, so one user's
// clicks cannot make a feed shared by many subscribers fetch over and over.
func (s *Service) Refresh(ctx context.Context, userID, id uint64) error {
	sub, err := s.repo.FindSubscription(ctx, userID, id)
	if err != nil {
		return err
	}
	ok, err := s.repo.ScheduleIfIdle(ctx, sub.FeedID, time.Now().UTC().Add(-5*time.Minute))
	if err == nil && !ok {
		return ErrRefreshedRecently
	}
	return err
}

// Import subscribes to every feed in an OPML file. Bad URLs and feeds the user
// already has are counted as skipped.
func (s *Service) Import(ctx context.Context, userID uint64, items []ImportItem) (added, skipped int, err error) {
	err = s.repo.Transaction(ctx, func(tx *gorm.DB) error {
		subs, entries := s.repo.WithTx(tx), s.entries.WithTx(tx)
		folderIDs := map[string]*uint64{}
		for _, item := range items {
			canonical, err := fetcher.Canonical(item.URL)
			if err != nil {
				skipped++
				continue
			}
			var folderID *uint64
			if item.Folder != "" {
				if id, ok := folderIDs[item.Folder]; ok {
					folderID = id
				} else {
					id, err := subs.FolderID(ctx, userID, item.Folder)
					if err != nil {
						return err
					}
					folderID = &id
					folderIDs[item.Folder] = folderID
				}
			}
			feed, err := subs.LockFeed(ctx, canonical)
			if err != nil {
				return err
			}
			created, err := add(ctx, subs, entries, &Subscription{UserID: userID, FeedID: feed.ID, FolderID: folderID}, feed)
			if err != nil {
				return err
			}
			if !created {
				skipped++
				continue
			}
			added++
			if err := subs.ScheduleNow(ctx, feed.ID); err != nil {
				return err
			}
		}
		return nil
	})
	return added, skipped, err
}

func (s *Service) Export(ctx context.Context, userID uint64) ([]Subscription, []Folder, error) {
	subs, err := s.repo.ListSubscriptions(ctx, userID)
	if err != nil {
		return nil, nil, err
	}
	folders, err := s.repo.ListFolders(ctx, userID)
	return subs, folders, err
}
