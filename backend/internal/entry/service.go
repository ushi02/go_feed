package entry

import (
	"context"
	"time"

	"gorm.io/gorm"
)

type Service struct{ repo *Repo }

func NewService(repo *Repo) *Service { return &Service{repo: repo} }

// List returns one page of the user's entries and whether there is another page.
func (s *Service) List(ctx context.Context, q ListQuery) ([]EntryView, bool, error) {
	limit := q.Limit
	q.Limit = limit + 1
	entries, err := s.repo.List(ctx, q)
	if err != nil {
		return nil, false, err
	}
	if len(entries) > limit {
		return entries[:limit], true, nil
	}
	return entries, false, nil
}

func (s *Service) Get(ctx context.Context, userID, entryID uint64) (EntryDetail, error) {
	return s.repo.Get(ctx, userID, entryID)
}

func (s *Service) UnreadCounts(ctx context.Context, userID uint64) (map[uint64]int64, error) {
	return s.repo.UnreadCounts(ctx, userID)
}

// UpdateState changes read and/or starred on an entry delivered to the user.
func (s *Service) UpdateState(ctx context.Context, userID, entryID uint64, read, starred *bool) error {
	ok, err := s.repo.Delivered(ctx, userID, entryID)
	if err != nil {
		return err
	}
	if !ok {
		return ErrNotFound
	}
	now := time.Now().UTC()
	updates := map[string]any{}
	if read != nil {
		if *read {
			updates["read_at"] = gorm.Expr("COALESCE(read_at, ?)", now)
		} else {
			updates["read_at"] = nil
		}
	}
	if starred != nil {
		if *starred {
			updates["starred_at"] = now
		} else {
			updates["starred_at"] = nil
		}
	}
	return s.repo.UpdateState(ctx, userID, entryID, updates)
}

func (s *Service) MarkRead(ctx context.Context, userID uint64, subscriptionID, folderID *uint64) error {
	if subscriptionID != nil {
		ok, err := s.repo.SubscriptionExists(ctx, userID, *subscriptionID)
		if err != nil {
			return err
		}
		if !ok {
			return ErrNotFound
		}
	}
	return s.repo.MarkRead(ctx, userID, subscriptionID, folderID, time.Now().UTC())
}
