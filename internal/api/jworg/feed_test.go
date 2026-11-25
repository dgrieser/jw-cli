package jworg

import (
	"errors"
	"os"
	"testing"
)

// jw.org's German feed of what is new reads into the entries the page has:
// the context split off the title, the day, the middle picture and the
// document it names, news and announcements told from publications.
func TestParseWhatsNewFeed(t *testing.T) {
	body, err := os.ReadFile("testdata/whatsnew_de.xml")
	if err != nil {
		t.Fatal(err)
	}
	items, locale, err := ParseWhatsNewFeed(body)
	if err != nil {
		t.Fatal(err)
	}
	if locale != "de" || len(items) != 20 {
		t.Fatalf("locale %q, %d items", locale, len(items))
	}
	news := items[0]
	if news.Kind != "news" || news.Context != "NACHRICHTEN" || news.Published != "2026-11-24" || news.DocID != 702026178 ||
		news.Title != "Ein Jahrhundert der „Verteidigung und gesetzlichen Befestigung der guten Botschaft“" ||
		news.ImageURL != "https://cms-imgp.jw-cdn.org/img/p/702026178/univ/art/702026178_univ_sqs_md.jpg" ||
		news.URL != "https://www.jw.org/de/nachrichten/region/deutschland/Ein-Jahrhundert-der-Verteidigung-und-gesetzlichen-Befestigung-der-guten-Botschaft/" {
		t.Errorf("news: %+v", news)
	}
	if a := items[1]; a.Kind != "news" || a.Context != "NEU AUF JW.ORG" {
		t.Errorf("announcement: %+v", a)
	}
	if p := items[2]; p.Kind != "publication" || p.DocID != 1102026536 || p.Title != "Bibelbuch 36: Zephanja" ||
		p.Context != "GOTTES WORT IST WAHRHEIT – EIN HANDBUCH ZUR BIBEL" {
		t.Errorf("publication: %+v", p)
	}
}

func TestParseWhatsNewFeedRefuses(t *testing.T) {
	for name, body := range map[string]string{
		"html":     `<html><body>JavaScript is disabled</body></html>`,
		"no items": `<rss><channel><link>https://www.jw.org/de/neu-auf-unserer-website/rss/WhatsNewWebArticles/feed.xml</link></channel></rss>`,
		"elsewhere": `<rss><channel><link>https://example.org/</link><item><title>x</title><link>https://example.org/x</link>
			<pubDate>Mon, 23 Nov 2026 00:00:00 +0000</pubDate></item></channel></rss>`,
	} {
		if _, _, err := ParseWhatsNewFeed([]byte(body)); !errors.Is(err, ErrNotAFeed) {
			t.Errorf("%s: want ErrNotAFeed, got %v", name, err)
		}
	}
}

// Every language's feed reads the same way: its locale off its link, which
// may name a page in the language's own script (the Japanese page's path is
// 新着情報, percent-encoded), the site's announcements under that page told
// as news, the context split off the title.
func TestParseWhatsNewFeedLanguages(t *testing.T) {
	for _, c := range []struct {
		file, locale string
		items        int
		news         int // news releases and announcements
	}{
		{"whatsnew_en.xml", "en", 20, 5},
		{"whatsnew_ja.xml", "ja", 17, 10},
		{"whatsnew_ase.xml", "ase", 20, 13},
		{"whatsnew_cmn-hans.xml", "cmn-hans", 18, 7},
	} {
		body, err := os.ReadFile("testdata/" + c.file)
		if err != nil {
			t.Fatal(err)
		}
		items, locale, err := ParseWhatsNewFeed(body)
		if err != nil {
			t.Errorf("%s: %v", c.file, err)
			continue
		}
		news := 0
		for _, it := range items {
			if it.Kind == "news" {
				news++
			}
			if it.Context == "" || it.Title == "" || it.Published == "" {
				t.Errorf("%s: entry without context, title or day: %+v", c.file, it)
			}
		}
		if locale != c.locale || len(items) != c.items || news != c.news {
			t.Errorf("%s: locale %q, %d items, %d news; want %q, %d, %d", c.file, locale, len(items), news, c.locale, c.items, c.news)
		}
	}
	// a Japanese announcement: under 新着情報, an id outside the news'
	body, _ := os.ReadFile("testdata/whatsnew_ja.xml")
	items, _, _ := ParseWhatsNewFeed(body)
	if a := items[10]; a.Kind != "news" || a.Context != "新着情報" || a.DocID != 500800020 {
		t.Errorf("Japanese announcement: %+v", a)
	}
}
