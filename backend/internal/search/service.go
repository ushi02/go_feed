package search

import (
	"context"
	"strings"
	"unicode/utf8"
)

const (
	resultLimit = 30
	maxTerms    = 8
)

type Service struct{ repo *Repo }

func NewService(repo *Repo) *Service { return &Service{repo: repo} }

func (s *Service) Search(ctx context.Context, userID uint64, q string) ([]Result, error) {
	query := booleanQuery(q)
	if query == "" {
		return nil, ErrEmptyQuery
	}
	return s.repo.Search(ctx, userID, query, resultLimit)
}

// booleanQuery turns user input into a MySQL boolean-mode query where every
// word must match: `鲸鱼 海豚` → `+"鲸鱼" +"海豚"`. With the ngram parser a
// quoted word is matched as a phrase of 2-character tokens. A single character
// is shorter than a token, so it becomes a prefix search (`+山*`). Operator
// characters are removed so user input can never change the query's meaning.
func booleanQuery(q string) string {
	clean := strings.Map(func(r rune) rune {
		if strings.ContainsRune(`+-<>()~*"@`, r) {
			return ' '
		}
		return r
	}, q)
	var terms []string
	for _, w := range strings.Fields(clean) {
		if len(terms) == maxTerms {
			break
		}
		if utf8.RuneCountInString(w) == 1 {
			terms = append(terms, "+"+w+"*")
		} else {
			terms = append(terms, `+"`+w+`"`)
		}
	}
	return strings.Join(terms, " ")
}
