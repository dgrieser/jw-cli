package jworg

import (
	"encoding/xml"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"

	"github.com/dgrieser/jw-cli/internal/model"
)

// jw.org publishes its page of what is new as an RSS feed as well
// (…/rss/WhatsNewWebArticles/feed.xml). A server that jw.org's bot
// protection turns away can be handed that feed by a machine it lets
// through; ParseWhatsNewFeed reads it into the entries the page has.
//
// The feed says less than the page: an entry's title carries its context
// ("NACHRICHTEN | Ein Jahrhundert …"), its picture names its document, and
// its kind is read off the document id and the link — news (documents
// 7xxxxxxxx) and the site's announcements (under the page of what is new
// itself) are news, a video is a link into the video library, the rest are
// publications. Issues are not told apart from their articles.

// ErrNotAFeed is a body that is no RSS feed of jw.org's page of what is new.
var ErrNotAFeed = errors.New("not an RSS feed of jw.org's page of what is new")

type rssFeed struct {
	XMLName xml.Name `xml:"rss"`
	Channel struct {
		// <link> and <atom:link href=…>, which the decoder both reads as link
		Links []struct {
			XMLName xml.Name
			Text    string `xml:",chardata"`
			Href    string `xml:"href,attr"`
		} `xml:"link"`
		Items []rssItem `xml:"item"`
	} `xml:"channel"`
}

// link is the feed's own address: its <link>, else its <atom:link>.
func (f rssFeed) link() string {
	for _, l := range f.Channel.Links {
		if t := strings.TrimSpace(l.Text); t != "" {
			return t
		}
	}
	for _, l := range f.Channel.Links {
		if h := strings.TrimSpace(l.Href); h != "" {
			return h
		}
	}
	return ""
}

type rssItem struct {
	Title       string `xml:"title"`
	Link        string `xml:"link"`
	Description string `xml:"description"`
	PubDate     string `xml:"pubDate"`
}

// feedImageDoc reads the document a picture of jw.org's CMS is of:
// https://cms-imgp.jw-cdn.org/img/p/702026178/univ/art/702026178_univ_sqs_sm.jpg.
var feedImageDoc = regexp.MustCompile(`/img/p/(\d+)/`)

// newsDocs are the document ids of jw.org's news reports.
const newsDocsFrom, newsDocsTo = 700_000_000, 800_000_000

// ParseWhatsNewFeed reads the RSS feed of jw.org's page of what is new:
// its entries, newest first as the feed has them, and the locale its link
// names ("de" for https://www.jw.org/de/neu-auf-unserer-website/rss/…).
func ParseWhatsNewFeed(body []byte) ([]model.NewItem, string, error) {
	var feed rssFeed
	if err := xml.Unmarshal(body, &feed); err != nil {
		return nil, "", fmt.Errorf("%w: %v", ErrNotAFeed, err)
	}
	link, err := url.Parse(feed.link())
	if err != nil || !onJWOrg(link) || !strings.Contains(link.Path, "/rss/") {
		return nil, "", fmt.Errorf("%w: its link %q is no feed of jw.org", ErrNotAFeed, feed.link())
	}
	locale, _, _ := strings.Cut(strings.TrimPrefix(link.Path, "/"), "/")
	// the page of what is new itself: what is under it is the site's news
	pagePath, _, _ := strings.Cut(link.Path, "/rss/")
	var items []model.NewItem
	for _, it := range feed.Channel.Items {
		item, ok := feedItem(it, pagePath)
		if ok {
			items = append(items, item)
		}
	}
	if len(items) == 0 {
		return nil, "", fmt.Errorf("%w: it has no entries", ErrNotAFeed)
	}
	return items, locale, nil
}

// onJWOrg reports a link to www.jw.org (or jw.org).
func onJWOrg(u *url.URL) bool {
	h := strings.ToLower(u.Hostname())
	return u.Scheme == "https" && (h == "www.jw.org" || h == "jw.org")
}

func feedItem(it rssItem, pagePath string) (model.NewItem, bool) {
	link := strings.TrimSpace(it.Link)
	at, err := time.Parse(time.RFC1123Z, strings.TrimSpace(it.PubDate))
	if err != nil {
		at, err = time.Parse(time.RFC1123, strings.TrimSpace(it.PubDate))
	}
	if link == "" || err != nil {
		return model.NewItem{}, false
	}
	item := model.NewItem{
		URL:       link,
		Published: at.UTC().Format(time.DateOnly),
		Title:     clean(it.Title),
	}
	if ctx, title, ok := strings.Cut(item.Title, " | "); ok {
		item.Context, item.Title = clean(ctx), clean(title)
	}
	if doc, err := goquery.NewDocumentFromReader(strings.NewReader(it.Description)); err == nil {
		if src, ok := doc.Find("img[src]").First().Attr("src"); ok {
			// the feed's thumbnail is the small square; the page shows the
			// middle one
			item.ImageURL = strings.Replace(src, "_sqs_sm.", "_sqs_md.", 1)
		}
	}
	if m := feedImageDoc.FindStringSubmatch(item.ImageURL); m != nil {
		item.DocID, _ = strconv.Atoi(m[1])
	}
	u, _ := url.Parse(link)
	switch {
	case mediaKey(link) != "":
		item.Kind, item.LANK = "video", mediaKey(link)
	case item.DocID >= newsDocsFrom && item.DocID < newsDocsTo,
		u != nil && pagePath != "" && strings.HasPrefix(u.Path, pagePath+"/") && u.Path != pagePath+"/":
		item.Kind = "news"
	case item.DocID != 0:
		item.Kind = "publication"
	default:
		item.Kind = "article"
	}
	return item, true
}
