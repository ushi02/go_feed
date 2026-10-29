package chat

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"unicode/utf8"

	"github.com/ushi02/go_feed/backend/internal/llm"
)

const (
	// historyLimit is how many stored messages are sent to the model each
	// turn. Older ones are still saved and shown, but the model forgets them,
	// so a long chat does not get slower and more expensive every turn.
	historyLimit = 20
	// shownLimit is how many messages the page loads.
	shownLimit    = 200
	maxInputRunes = 1000
	titleRunes    = 50
)

type Service struct {
	repo     *Repo
	llm      llm.Client // nil when no API key is set; the chat then reports ErrNotConfigured
	subs     Subscriptions
	searcher Searcher
}

func NewService(repo *Repo, client llm.Client, subs Subscriptions, searcher Searcher) *Service {
	return &Service{repo: repo, llm: client, subs: subs, searcher: searcher}
}

// Items returns the user's current conversation as the page shows it.
func (s *Service) Items(ctx context.Context, userID uint64) ([]Item, error) {
	conv, err := s.repo.Latest(ctx, userID)
	if errors.Is(err, errNoConversation) {
		return []Item{}, nil
	}
	if err != nil {
		return nil, err
	}
	stored, err := s.repo.RecentMessages(ctx, userID, conv.ID, shownLimit)
	if err != nil {
		return nil, err
	}
	return items(stored), nil
}

// NewConversation starts over: the page shows an empty chat, and the model
// no longer sees the old messages.
func (s *Service) NewConversation(ctx context.Context, userID uint64) error {
	return s.repo.CreateConversation(ctx, &Conversation{UserID: userID})
}

// Send stores the user's message, streams Gugu's reply through ev and stores
// the reply too. If the page is closed mid-reply, the part generated so far is
// still stored and marked incomplete.
func (s *Service) Send(ctx context.Context, userID uint64, text string, ev Events) error {
	text = strings.TrimSpace(text)
	if text == "" || utf8.RuneCountInString(text) > maxInputRunes {
		return ErrInvalidMessage
	}
	if s.llm == nil {
		return ErrNotConfigured
	}
	conv, err := s.repo.Latest(ctx, userID)
	if errors.Is(err, errNoConversation) {
		conv = Conversation{UserID: userID}
		err = s.repo.CreateConversation(ctx, &conv)
	}
	if err != nil {
		return err
	}
	if conv.Title == "" {
		if err := s.repo.SetTitle(ctx, userID, conv.ID, cut(text, titleRunes)); err != nil {
			return err
		}
	}
	if err := s.repo.AddMessages(ctx, []Message{{ConversationID: conv.ID, UserID: userID, Role: llm.RoleUser, Content: text}}); err != nil {
		return err
	}
	stored, err := s.repo.RecentMessages(ctx, userID, conv.ID, historyLimit)
	if err != nil {
		return err
	}
	produced, replyErr := s.Reply(ctx, userID, window(toLLM(stored)), ev)
	save := make([]Message, len(produced))
	for i, m := range produced {
		save[i] = fromLLM(conv.ID, userID, m)
	}
	if n := len(save); replyErr != nil && n > 0 && save[n-1].Role == llm.RoleAssistant && save[n-1].ToolCalls == nil {
		save[n-1].Incomplete = true
	}
	// The request context is cancelled when the page closes, but what was
	// generated until then must still be saved.
	if err := s.repo.AddMessages(context.WithoutCancel(ctx), save); err != nil {
		return err
	}
	return replyErr
}

// window drops messages from the front until it starts with a user message,
// so no tool result is sent without the tool call it answers; the model API
// rejects that.
func window(ms []llm.Message) []llm.Message {
	for i, m := range ms {
		if m.Role == llm.RoleUser {
			return ms[i:]
		}
	}
	return nil
}

func toLLM(stored []Message) []llm.Message {
	out := make([]llm.Message, len(stored))
	for i, m := range stored {
		out[i] = llm.Message{Role: m.Role, Content: m.Content, ToolCallID: m.ToolCallID}
		if m.ToolCalls != nil {
			_ = json.Unmarshal(m.ToolCalls, &out[i].ToolCalls) // written by fromLLM, so valid
		}
	}
	return out
}

func fromLLM(conversationID, userID uint64, m llm.Message) Message {
	stored := Message{ConversationID: conversationID, UserID: userID, Role: m.Role, Content: m.Content, ToolCallID: m.ToolCallID}
	if len(m.ToolCalls) > 0 {
		stored.ToolCalls, _ = json.Marshal(m.ToolCalls)
	}
	return stored
}

// items turns stored messages into what the page shows. Tool calls and
// search results stay hidden; verified proposals become cards.
func items(stored []Message) []Item {
	out := []Item{}
	proposals := map[string]bool{}
	for i, m := range toLLM(stored) {
		switch {
		case m.Role == llm.RoleUser:
			out = append(out, Item{ID: stored[i].ID, Kind: "user", Text: m.Content})
		case m.Role == llm.RoleAssistant:
			noteProposals(m, proposals)
			if m.Content != "" {
				out = append(out, Item{ID: stored[i].ID, Kind: "assistant", Text: m.Content, Incomplete: stored[i].Incomplete})
			}
		default:
			if c, ok := cardOf(m, proposals); ok {
				out = append(out, Item{ID: stored[i].ID, Kind: "card", Card: &c})
			}
		}
	}
	return out
}

// Events receives a reply while it is being made. Either field may be nil.
type Events struct {
	Text func(piece string) // answer text, already cleaned of made-up links
	Card func(Card)         // a blog that just passed verification
}

// Reply runs one turn: the model may call the user's tools, then answers.
// It returns the new messages (tool calls, tool results, the answer). Links
// in the answer that no tool returned and the user did not write are removed
// while streaming, before anyone sees them.
func (s *Service) Reply(ctx context.Context, userID uint64, history []llm.Message, ev Events) ([]llm.Message, error) {
	known := knownURLs(history)
	tools := NewTools(userID, s.subs, s.searcher)
	for i := range tools {
		run, name := tools[i].Run, tools[i].Def.Name
		tools[i].Run = func(ctx context.Context, args json.RawMessage) (string, error) {
			out, err := run(ctx, args)
			if err != nil {
				return out, err
			}
			// A successful result is trusted from now on, also mid-turn.
			for k := range knownURLs([]llm.Message{{Role: llm.RoleTool, Content: out}}) {
				known[k] = true
			}
			if name == toolPropose && ev.Card != nil {
				var c Card
				if json.Unmarshal([]byte(out), &c) == nil {
					ev.Card(c)
				}
			}
			return out, nil
		}
	}
	messages := append([]llm.Message{{Role: llm.RoleSystem, Content: systemPrompt}}, history...)
	return RunTools(ctx, s.llm, messages, tools, newLinkFilter(known, ev.Text))
}
