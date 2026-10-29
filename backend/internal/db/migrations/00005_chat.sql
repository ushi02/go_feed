-- +goose Up
-- One row per chat. The page shows only the newest one; older ones are kept
-- so a history list can be added later without a migration.
CREATE TABLE conversations (
  id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
  user_id BIGINT UNSIGNED NOT NULL,
  title VARCHAR(255) NOT NULL DEFAULT '',
  created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
  KEY idx_conversations_user (user_id, id),
  CONSTRAINT fk_conversations_user FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- Messages are stored exactly as sent to and received from the model,
-- including tool calls and tool results, so a conversation can be replayed.
-- user_id is copied from the conversation so every query can filter on it
-- directly, and so "messages this user sent today" is one index range.
CREATE TABLE messages (
  id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
  conversation_id BIGINT UNSIGNED NOT NULL,
  user_id BIGINT UNSIGNED NOT NULL,
  role VARCHAR(16) NOT NULL,
  content MEDIUMTEXT NOT NULL,
  tool_calls JSON NULL,
  tool_call_id VARCHAR(255) NOT NULL DEFAULT '',
  -- The reply stopped early (the page was closed, or the model failed);
  -- content holds what was generated until then.
  incomplete BOOLEAN NOT NULL DEFAULT FALSE,
  created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
  KEY idx_messages_conversation (conversation_id, id),
  KEY idx_messages_user_role_time (user_id, role, created_at),
  CONSTRAINT fk_messages_conversation FOREIGN KEY (conversation_id) REFERENCES conversations(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- +goose Down
DROP TABLE messages;
DROP TABLE conversations;
