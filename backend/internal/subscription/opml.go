package subscription

import (
	"encoding/xml"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/ushi02/go_feed/backend/internal/fetcher"
	"github.com/ushi02/go_feed/backend/internal/httpx"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
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

type importFeed struct{ URL, Folder string }

func flatten(outlines []outline, path string, result *[]importFeed) {
	for _, o := range outlines {
		if o.XMLURL != "" {
			*result = append(*result, importFeed{URL: o.XMLURL, Folder: path})
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

func (a *Handler) ImportOPML(c *gin.Context) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 2<<20)
	f, _, err := c.Request.FormFile("file")
	if err != nil {
		httpx.Fail(c, 400, "OPML file is required (max 2 MiB)")
		return
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, (1<<20)+1))
	if err != nil || len(data) > 1<<20 {
		httpx.Fail(c, 400, "OPML exceeds 1 MiB")
		return
	}
	var doc opml
	if xml.Unmarshal(data, &doc) != nil || doc.XMLName.Local != "opml" {
		httpx.Fail(c, 400, "invalid OPML")
		return
	}
	var items []importFeed
	flatten(doc.Body.Outlines, "", &items)
	if len(items) > 500 {
		httpx.Fail(c, 400, "OPML contains more than 500 feeds")
		return
	}
	added, skipped := 0, 0
	err = a.DB.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		folderIDs := map[string]*uint64{}
		for _, item := range items {
			canonical, err := fetcher.Canonical(item.URL)
			if err != nil {
				skipped++
				continue
			}
			var folderID *uint64
			if item.Folder != "" {
				if id, ok := folderIDs[item.Folder]; ok {
					folderID = id
				} else {
					folder := Folder{UserID: httpx.UserID(c), Name: item.Folder}
					if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&folder).Error; err != nil {
						return err
					}
					if err := tx.Where("user_id = ? AND name = ?", httpx.UserID(c), item.Folder).First(&folder).Error; err != nil {
						return err
					}
					folderID = &folder.ID
					folderIDs[item.Folder] = folderID
				}
			}
			feed := Feed{URL: canonical, URLHash: fetcher.Hash(canonical), NextFetchAt: time.Now().UTC()}
			if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&feed).Error; err != nil {
				return err
			}
			if err := tx.Where("url_hash = ?", feed.URLHash).First(&feed).Error; err != nil {
				return err
			}
			sub := Subscription{UserID: httpx.UserID(c), FeedID: feed.ID, FolderID: folderID, PendingBaseline: !feed.Initialized}
			if feed.Initialized {
				baseline, err := BaselineID(tx, feed.ID)
				if err != nil {
					return err
				}
				sub.ReadThroughEntryID = baseline
			}
			r := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&sub)
			if r.Error != nil {
				return r.Error
			}
			if r.RowsAffected == 0 {
				skipped++
			} else {
				added++
				if err := tx.Model(&Feed{}).Where("id = ?", feed.ID).Update("next_fetch_at", time.Now().UTC()).Error; err != nil {
					return err
				}
			}
		}
		return nil
	})
	if err != nil {
		httpx.Fail(c, 500, "import failed")
		return
	}
	c.JSON(200, gin.H{"added": added, "skipped": skipped})
}

func (a *Handler) ExportOPML(c *gin.Context) {
	var subs []Subscription
	var folders []Folder
	if err := a.DB.WithContext(c.Request.Context()).Preload("Feed").Where("user_id = ?", httpx.UserID(c)).Find(&subs).Error; err != nil {
		httpx.Fail(c, 500, "database error")
		return
	}
	if err := a.DB.WithContext(c.Request.Context()).Where("user_id = ?", httpx.UserID(c)).Find(&folders).Error; err != nil {
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
