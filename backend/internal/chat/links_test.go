package chat

import (
	"strings"
	"testing"
)

var streamCases = []string{
	"推荐 https://fake.example/blog ，还有 https://ok.example/feed 咕",
	"看[假博客](https://fake.example/x)和[真博客](https://ok.example/feed)。",
	"链接在句尾https://fake.example/a",
	"[注] 这里没有链接，只是方括号",
	"[一半的链接](https://fake.exa",
	"中文里夹着https://fake.example/路径，后面还有字",
}

// However the text is split into pieces, the filtered result equals cleaning
// the whole text at once, and no piece the user sees ever contains any part
// of a made-up URL.
func TestLinkFilterStreaming(t *testing.T) {
	known := map[string]bool{"https://ok.example/feed": true}
	for _, text := range streamCases {
		want := removeLinks(text, known)
		runes := []rune(text)
		for size := 1; size <= 7; size++ {
			var seen []string
			f := newLinkFilter(known, func(s string) { seen = append(seen, s) })
			for i := 0; i < len(runes); i += size {
				f.Write(string(runes[i:min(i+size, len(runes))]))
			}
			got := f.Close()
			if got != want {
				t.Errorf("pieces of %d: got %q, want %q", size, got, want)
			}
			if strings.Join(seen, "") != got {
				t.Errorf("streamed %q but kept %q", strings.Join(seen, ""), got)
			}
			for _, s := range seen {
				if strings.Contains(s, "fake") || strings.Contains(s, "https://fa") {
					t.Errorf("user saw part of a made-up link: %q (text %q, pieces of %d)", s, text, size)
				}
			}
		}
	}
}

// Close ends one message; the next message starts empty.
func TestLinkFilterCloseResets(t *testing.T) {
	f := newLinkFilter(map[string]bool{}, nil)
	f.Write("第一条")
	if got := f.Close(); got != "第一条" {
		t.Fatalf("got %q", got)
	}
	f.Write("第二条")
	if got := f.Close(); got != "第二条" {
		t.Fatalf("got %q", got)
	}
}
