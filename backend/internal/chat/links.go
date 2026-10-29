package chat

import (
	"regexp"
	"strings"

	"github.com/ushi02/go_feed/backend/internal/llm"
)

var (
	markdownLink = regexp.MustCompile(`\[([^\]]*)\]\((https?://[^)\s]+)\)`)
	// A bare URL ends at whitespace, quotes, brackets or Chinese punctuation.
	bareURL = regexp.MustCompile(`https?://[^\s<>"'()\[\]{}，。、；：！？（）【】《》「」]+`)
)

func urlKey(u string) string {
	return strings.TrimRight(strings.TrimRight(u, ".,;:!?"), "/")
}

// knownURLs collects every URL that came from a tool result or from the user.
// Those are the only links the model's answer may keep.
func knownURLs(messages []llm.Message) map[string]bool {
	known := map[string]bool{}
	for _, m := range messages {
		if m.Role != llm.RoleTool && m.Role != llm.RoleUser {
			continue
		}
		for _, u := range bareURL.FindAllString(m.Content, -1) {
			known[urlKey(u)] = true
		}
	}
	return known
}

// removeLinks deletes links the model made up: any URL not in known. A
// markdown link keeps its text and loses the address.
func removeLinks(text string, known map[string]bool) string {
	text = markdownLink.ReplaceAllStringFunc(text, func(m string) string {
		parts := markdownLink.FindStringSubmatch(m)
		if known[urlKey(parts[2])] {
			return m
		}
		return parts[1]
	})
	return bareURL.ReplaceAllStringFunc(text, func(u string) string {
		if known[urlKey(u)] {
			return u
		}
		return ""
	})
}
