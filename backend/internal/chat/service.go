package chat

import (
	"context"

	"github.com/ushi02/go_feed/backend/internal/llm"
)

type Service struct {
	llm      llm.Client
	subs     Subscriptions
	searcher Searcher
}

func NewService(client llm.Client, subs Subscriptions, searcher Searcher) *Service {
	return &Service{llm: client, subs: subs, searcher: searcher}
}

// Reply runs one turn: the model may call the user's tools, then answers.
// It returns the new messages (tool calls, tool results, the answer). Links
// in the answer that no tool returned and the user did not write are removed
// before anyone sees them.
func (s *Service) Reply(ctx context.Context, userID uint64, history []llm.Message) ([]llm.Message, error) {
	messages := append([]llm.Message{{Role: llm.RoleSystem, Content: systemPrompt}}, history...)
	produced, err := RunTools(ctx, s.llm, messages, NewTools(userID, s.subs, s.searcher))
	if n := len(produced); n > 0 && produced[n-1].Role == llm.RoleAssistant && len(produced[n-1].ToolCalls) == 0 {
		known := knownURLs(append(append([]llm.Message(nil), history...), produced...))
		produced[n-1].Content = removeLinks(produced[n-1].Content, known)
	}
	return produced, err
}
