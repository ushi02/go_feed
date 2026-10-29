package llm

import (
	"context"
	"errors"
	"sync"
)

// Fake is a Client for tests. It returns Replies in order and records every
// request it receives, so a test can check what was sent to the model.
type Fake struct {
	mu       sync.Mutex
	Replies  []Message
	Requests []Request
}

func (f *Fake) Chat(ctx context.Context, req Request, onText func(string)) (Message, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	// Copy the messages: the caller keeps appending to its slice.
	req.Messages = append([]Message(nil), req.Messages...)
	f.Requests = append(f.Requests, req)
	if len(f.Replies) == 0 {
		return Message{}, errors.New("fake llm: no reply left")
	}
	reply := f.Replies[0]
	f.Replies = f.Replies[1:]
	// Stream the text in two pieces, so callers see more than one delta.
	if onText != nil && reply.Content != "" {
		r := []rune(reply.Content)
		onText(string(r[:len(r)/2]))
		onText(string(r[len(r)/2:]))
	}
	return reply, nil
}
