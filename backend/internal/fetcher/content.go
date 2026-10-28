package fetcher

import (
	"bytes"
	"net/url"
	"strings"

	"github.com/microcosm-cc/bluemonday"
	"golang.org/x/net/html"
)

// CleanHTML resolves relative URLs before sanitizing untrusted feed content.
func CleanHTML(content, baseURL string) string {
	base, err := url.Parse(baseURL)
	if err != nil {
		return bluemonday.UGCPolicy().Sanitize(content)
	}
	root, err := html.Parse(strings.NewReader("<div>" + content + "</div>"))
	if err != nil {
		return bluemonday.UGCPolicy().Sanitize(content)
	}
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode {
			for i, a := range n.Attr {
				if a.Key != "href" && a.Key != "src" {
					continue
				}
				u, err := base.Parse(a.Val)
				if err == nil && (u.Scheme == "http" || u.Scheme == "https") {
					n.Attr[i].Val = u.String()
				} else {
					n.Attr[i].Val = ""
				}
			}
			if n.Data == "a" {
				n.Attr = append(n.Attr, html.Attribute{Key: "rel", Val: "noopener noreferrer"})
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(root)
	var buf bytes.Buffer
	_ = html.Render(&buf, root)
	policy := bluemonday.UGCPolicy()
	policy.AllowAttrs("rel").OnElements("a")
	// Feed links point to other sites; open them in a new tab instead of leaving the reader.
	policy.AddTargetBlankToFullyQualifiedLinks(true)
	return policy.Sanitize(buf.String())
}

// PlainText extracts the visible text of feed HTML (entities decoded, script
// and style dropped, whitespace collapsed) and cuts it to limit runes. Summaries
// are stored this way so the search index sees words, not markup.
func PlainText(content string, limit int) string {
	z := html.NewTokenizer(strings.NewReader(content))
	var b strings.Builder
	skip := 0               // depth inside <script> or <style>
	for b.Len() < limit*4 { // UTF-8 runes are at most 4 bytes
		switch z.Next() {
		case html.ErrorToken:
			return cut(strings.Join(strings.Fields(b.String()), " "), limit)
		case html.StartTagToken:
			if name, _ := z.TagName(); string(name) == "script" || string(name) == "style" {
				skip++
			}
		case html.EndTagToken:
			if name, _ := z.TagName(); (string(name) == "script" || string(name) == "style") && skip > 0 {
				skip--
			}
		case html.TextToken:
			if skip == 0 {
				b.Write(z.Text())
				b.WriteByte(' ')
			}
		}
	}
	return cut(strings.Join(strings.Fields(b.String()), " "), limit)
}

func cut(s string, limit int) string {
	r := []rune(s)
	if len(r) > limit {
		return string(r[:limit])
	}
	return s
}
