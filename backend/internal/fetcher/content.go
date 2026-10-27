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
