-- +goose Up
-- search_text is title + summary, kept in sync by MySQL (STORED generated column).
-- The ngram parser splits Chinese text into 2-character tokens, so words can be
-- found without a Chinese word segmenter. Full article text is not indexed.
ALTER TABLE entries ADD COLUMN search_text TEXT
  GENERATED ALWAYS AS (CONCAT(title, '\n', COALESCE(summary, ''))) STORED;
ALTER TABLE entries ADD FULLTEXT INDEX ft_entries_search (search_text) WITH PARSER ngram;

-- +goose Down
ALTER TABLE entries DROP INDEX ft_entries_search;
ALTER TABLE entries DROP COLUMN search_text;
