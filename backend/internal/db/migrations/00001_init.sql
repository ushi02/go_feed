-- +goose Up
CREATE TABLE users (
  id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
  username VARCHAR(64) NOT NULL,
  password_hash VARCHAR(255) NOT NULL,
  created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
  UNIQUE KEY uq_users_username (username)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE sessions (
  token_hash CHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL PRIMARY KEY,
  user_id BIGINT UNSIGNED NOT NULL,
  expires_at DATETIME(6) NOT NULL,
  created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
  KEY idx_sessions_user (user_id),
  KEY idx_sessions_expiry (expires_at),
  CONSTRAINT fk_sessions_user FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE feeds (
  id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
  url TEXT NOT NULL,
  url_hash BINARY(32) NOT NULL,
  title VARCHAR(512) NOT NULL DEFAULT '',
  site_url TEXT NULL,
  etag VARCHAR(512) NOT NULL DEFAULT '',
  last_modified VARCHAR(512) NOT NULL DEFAULT '',
  next_fetch_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
  lease_until DATETIME(6) NULL,
  lease_token CHAR(32) CHARACTER SET ascii COLLATE ascii_bin NULL,
  last_fetched_at DATETIME(6) NULL,
  last_error TEXT NULL,
  failure_count INT NOT NULL DEFAULT 0,
  initialized BOOLEAN NOT NULL DEFAULT FALSE,
  created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
  UNIQUE KEY uq_feeds_url_hash (url_hash),
  KEY idx_feeds_due (next_fetch_at, lease_until)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE folders (
  id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
  user_id BIGINT UNSIGNED NOT NULL,
  name VARCHAR(160) NOT NULL,
  created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
  UNIQUE KEY uq_folders_user_name (user_id, name),
  CONSTRAINT fk_folders_user FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE subscriptions (
  id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
  user_id BIGINT UNSIGNED NOT NULL,
  feed_id BIGINT UNSIGNED NOT NULL,
  folder_id BIGINT UNSIGNED NULL,
  read_through_entry_id BIGINT UNSIGNED NOT NULL DEFAULT 0,
  pending_baseline BOOLEAN NOT NULL DEFAULT TRUE,
  created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
  UNIQUE KEY uq_subscriptions_user_feed (user_id, feed_id),
  KEY idx_subscriptions_folder (user_id, folder_id),
  KEY idx_subscriptions_feed (feed_id),
  CONSTRAINT fk_subscriptions_user FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE,
  CONSTRAINT fk_subscriptions_feed FOREIGN KEY (feed_id) REFERENCES feeds(id) ON DELETE CASCADE,
  CONSTRAINT fk_subscriptions_folder FOREIGN KEY (folder_id) REFERENCES folders(id) ON DELETE SET NULL
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE entries (
  id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
  feed_id BIGINT UNSIGNED NOT NULL,
  guid_hash BINARY(32) NOT NULL,
  title VARCHAR(512) NOT NULL DEFAULT '',
  url TEXT NULL,
  content MEDIUMTEXT NULL,
  summary TEXT NULL,
  published_at DATETIME(6) NULL,
  sort_at DATETIME(6) NOT NULL,
  created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
  UNIQUE KEY uq_entries_feed_guid (feed_id, guid_hash),
  KEY idx_entries_feed_id (feed_id, id),
  KEY idx_entries_sort (sort_at DESC, id DESC),
  CONSTRAINT fk_entries_feed FOREIGN KEY (feed_id) REFERENCES feeds(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE entry_states (
  user_id BIGINT UNSIGNED NOT NULL,
  entry_id BIGINT UNSIGNED NOT NULL,
  read_override TINYINT NULL,
  starred_at DATETIME(6) NULL,
  PRIMARY KEY (user_id, entry_id),
  KEY idx_entry_states_starred (user_id, starred_at),
  CONSTRAINT fk_entry_states_user FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE,
  CONSTRAINT fk_entry_states_entry FOREIGN KEY (entry_id) REFERENCES entries(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- +goose Down
DROP TABLE entry_states;
DROP TABLE entries;
DROP TABLE subscriptions;
DROP TABLE folders;
DROP TABLE feeds;
DROP TABLE sessions;
DROP TABLE users;
