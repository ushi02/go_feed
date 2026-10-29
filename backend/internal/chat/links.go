package chat

import (
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

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
// Those are the only links the model's answer may keep. A failed tool result
// does not count: error messages quote the URL that failed.
func knownURLs(messages []llm.Message) map[string]bool {
	known := map[string]bool{}
	for _, m := range messages {
		if m.Role != llm.RoleTool && m.Role != llm.RoleUser || m.Role == llm.RoleTool && strings.HasPrefix(m.Content, errorPrefix) {
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

// linkFilter removes made-up links from streamed text. A URL may arrive split
// over several pieces ("https://fa" then "ke.blog"), so text is held back
// until every link in it is complete; then that part is cleaned with
// removeLinks and passed on. Cutting only where no link can be open makes the
// result the same as cleaning the whole text at once.
type linkFilter struct {
	known   map[string]bool
	onText  func(string)
	pending string
	out     strings.Builder
}

func newLinkFilter(known map[string]bool, onText func(string)) *linkFilter {
	return &linkFilter{known: known, onText: onText}
}

func (f *linkFilter) Write(piece string) {
	f.pending += piece
	if cut := safeCut(f.pending); cut > 0 {
		f.release(f.pending[:cut])
		f.pending = f.pending[cut:]
	}
}

func (f *linkFilter) Close() string {
	f.release(f.pending)
	f.pending = ""
	text := f.out.String()
	f.out.Reset()
	return text
}

func (f *linkFilter) release(s string) {
	s = removeLinks(s, f.known)
	f.out.WriteString(s)
	if f.onText != nil && s != "" {
		f.onText(s)
	}
}

// urlEnd lists the characters a bare URL cannot contain (see bareURL).
const urlEnd = `<>"'()[]{}，。、；：！？（）【】《》「」`

// safeCut returns how much of s can be released: up to the last character
// that ends any URL, but not past the start of an unfinished markdown link.
func safeCut(s string) int {
	cut := 0
	for i, r := range s {
		if unicode.IsSpace(r) || strings.ContainsRune(urlEnd, r) {
			cut = i + utf8.RuneLen(r)
		}
	}
	if b := openLink(s); b >= 0 && b < cut {
		cut = b
	}
	return cut
}

// openLink returns where an unfinished markdown link starts in s, or -1:
// "[text", "[text]" (which may go on with "(url)") or "[text](url".
func openLink(s string) int {
	b := strings.LastIndex(s, "[")
	if b < 0 {
		return -1
	}
	rest := s[b:]
	end := strings.Index(rest, "]")
	switch {
	case end < 0, end == len(rest)-1:
		return b
	case rest[end+1] == '(' && !strings.Contains(rest[end:], ")"):
		return b
	}
	return -1
}
