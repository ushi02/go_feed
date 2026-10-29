package fetcher

import (
	"strings"
	"testing"
)

func TestPlainText(t *testing.T) {
	tests := []struct {
		name, in string
		limit    int
		want     string
	}{
		{"tags and entities", `<p>&ldquo;你好&rdquo;</p><p>world &amp; go</p>`, 100, "“你好” world & go"},
		{"block tags keep words apart", `<p>foo</p><p>bar</p>`, 100, "foo bar"},
		{"script and style dropped", `<style>p{}</style>正文<script>alert(1)</script>`, 100, "正文"},
		{"whitespace collapsed", "a \n\n\t b", 100, "a b"},
		{"cut by runes, not bytes", "一二三四五", 3, "一二三"},
		{"plain text unchanged", "没有标签", 100, "没有标签"},
	}
	for _, tt := range tests {
		if got := PlainText(tt.in, tt.limit); got != tt.want {
			t.Errorf("%s: PlainText = %q, want %q", tt.name, got, tt.want)
		}
	}
	long := strings.Repeat("<p>字</p>", 10000)
	if got := []rune(PlainText(long, 500)); len(got) != 500 {
		t.Errorf("long input: got %d runes, want 500", len(got))
	}
}
