package chat

import (
	"errors"
	"time"
)

type Conversation struct {
	ID        uint64
	UserID    uint64
	Title     string
	CreatedAt time.Time
}

// Message is one stored chat message, in the same shape it had when sent to
// or received from the model.
type Message struct {
	ID             uint64
	ConversationID uint64
	UserID         uint64
	Role           string
	Content        string
	ToolCalls      []byte // JSON; nil (NULL) when there are none
	ToolCallID     string
	Incomplete     bool
	CreatedAt      time.Time
}

// Item is one thing the chat page shows, in order: a user message, an answer
// from Gugu, or a verified blog card.
type Item struct {
	ID         uint64 `json:"id"`
	Kind       string `json:"kind"` // "user", "assistant" or "card"
	Text       string `json:"text,omitempty"`
	Card       *Card  `json:"card,omitempty"`
	Incomplete bool   `json:"incomplete,omitempty"`
}

var (
	ErrNotConfigured  = errors.New("chatbot is not configured")
	ErrInvalidMessage = errors.New("message is empty or too long")
	ErrDailyLimit     = errors.New("daily message limit reached")
	errNoConversation = errors.New("no conversation yet")
)
