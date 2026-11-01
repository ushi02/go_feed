package subscription

import (
	"encoding/xml"
	"io"
	"net/http"
	"sort"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/ushi02/go_feed/backend/internal/httpx"
)

type outline struct {
	Text     string    `xml:"text,attr,omitempty"`
	Title    string    `xml:"title,attr,omitempty"`
	Type     string    `xml:"type,attr,omitempty"`
	XMLURL   string    `xml:"xmlUrl,attr,omitempty"`
	HTMLURL  string    `xml:"htmlUrl,attr,omitempty"`
	Children []outline `xml:"outline,omitempty"`
}

type opml struct {
	XMLName xml.Name `xml:"opml"`
	Version string   `xml:"version,attr"`
	Body    struct {
		Outlines []outline `xml:"outline"`
	} `xml:"body"`
}

func flatten(outlines []outline, path string, result *[]ImportItem) {
	for _, o := range outlines {
		if o.XMLURL != "" {
			*result = append(*result, ImportItem{URL: o.XMLURL, Folder: path})
			continue
		}
		name := strings.TrimSpace(o.Text)
		if name == "" {
			name = strings.TrimSpace(o.Title)
		}
		if name != "" {
			if path != "" {
				name = path + " / " + name
			}
			pathName := truncate(name, 160)
			flatten(o.Children, pathName, result)
		} else {
			flatten(o.Children, path, result)
		}
	}
}

func (h *Handler) ImportOPML(c *gin.Context) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 2<<20)
	f, _, err := c.Request.FormFile("file")
	if err != nil {
		httpx.FailCode(c, 400, "invalid_opml", "OPML file is required (max 2 MiB)")
		return
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, (1<<20)+1))
	if err != nil || len(data) > 1<<20 {
		httpx.FailCode(c, 400, "invalid_opml", "OPML exceeds 1 MiB")
		return
	}
	var doc opml
	if xml.Unmarshal(data, &doc) != nil || doc.XMLName.Local != "opml" {
		httpx.FailCode(c, 400, "invalid_opml", "invalid OPML")
		return
	}
	var items []ImportItem
	flatten(doc.Body.Outlines, "", &items)
	if len(items) > 500 {
		httpx.FailCode(c, 400, "invalid_opml", "OPML contains more than 500 feeds")
		return
	}
	added, skipped, err := h.svc.Import(c.Request.Context(), httpx.UserID(c), items)
	if err != nil {
		httpx.Fail(c, 500, "import failed")
		return
	}
	c.JSON(200, gin.H{"added": added, "skipped": skipped})
}

func (h *Handler) ExportOPML(c *gin.Context) {
	subs, folders, err := h.svc.Export(c.Request.Context(), httpx.UserID(c))
	if err != nil {
		httpx.Fail(c, 500, "database error")
		return
	}
	names := map[uint64]string{}
	for _, f := range folders {
		names[f.ID] = f.Name
	}
	groups := map[string][]outline{}
	for _, sub := range subs {
		name := ""
		if sub.FolderID != nil {
			name = names[*sub.FolderID]
		}
		title := sub.Feed.Title
		if title == "" {
			title = sub.Feed.URL
		}
		groups[name] = append(groups[name], outline{Text: title, Title: title, Type: "rss", XMLURL: sub.Feed.URL, HTMLURL: sub.Feed.SiteURL})
	}
	doc := opml{Version: "2.0"}
	keys := make([]string, 0, len(groups))
	for k := range groups {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if k == "" {
			doc.Body.Outlines = append(doc.Body.Outlines, groups[k]...)
		} else {
			doc.Body.Outlines = append(doc.Body.Outlines, outline{Text: k, Title: k, Children: groups[k]})
		}
	}
	data, err := xml.MarshalIndent(doc, "", "  ")
	if err != nil {
		httpx.Fail(c, 500, "export failed")
		return
	}
	c.Header("Content-Disposition", "attachment; filename=subscriptions.opml")
	c.Data(200, "text/xml; charset=utf-8", append([]byte(xml.Header), data...))
}

func truncate(s string, limit int) string {
	r := []rune(s)
	if len(r) > limit {
		return string(r[:limit])
	}
	return s
}
