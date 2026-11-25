package service

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dgrieser/jw-cli/internal/httpx"
	"github.com/dgrieser/jw-cli/internal/model"
)

// whatsNewSite is a jw.org whose page of what is new answers the page or
// the bot check, counting how often it is asked.
func whatsNewSite(t *testing.T, blocked *atomic.Bool, hits *atomic.Int32) *Service {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.Header().Set("Content-Type", "text/html")
		if blocked.Load() {
			w.Write([]byte(`<html><body><noscript>verify that you're not a robot</noscript></body></html>`))
			return
		}
		w.Write([]byte(`<html><body><div class="synopsis news docId-702026178">
<p class="pubDate">2026-11-24</p><h3><a href="/de/nachrichten/x/">From the page</a></h3></div></body></html>`))
	}))
	t.Cleanup(srv.Close)
	hc := httpx.New(httpx.WithBaseURLs(httpx.BaseURLs{JWOrg: srv.URL, WOL: srv.URL, CDN: srv.URL}),
		httpx.WithRetryDelays(time.Millisecond))
	svc := New(hc, httpx.OpenCacheAt(t.TempDir()))
	feeds, err := OpenFeeds(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	svc.Feeds = feeds
	return svc
}

// A feed posted stands in for jw.org's page for a day: the page is not
// asked, not even by a reload. Past the hold the page is read again; when
// it turns the server away, the last feed stands, said to be stale.
func TestWhatsNewFeedHold(t *testing.T) {
	var blocked atomic.Bool
	var hits atomic.Int32
	svc := whatsNewSite(t, &blocked, &hits)
	ctx := context.Background()
	feed := []model.NewItem{{Kind: "news", Title: "From the feed", Published: "2026-11-25"}}

	if _, err := svc.Feeds.Put("X", feed, time.Now()); err != nil {
		t.Fatal(err)
	}
	for _, c := range []context.Context{ctx, httpx.WithRefresh(ctx)} {
		items, err := svc.WhatsNew(c, "X")
		if err != nil || len(items) != 1 || items[0].Title != "From the feed" {
			t.Fatalf("within the hold: %v %+v", err, items)
		}
	}
	if hits.Load() != 0 {
		t.Fatalf("jw.org was asked %d times within the hold", hits.Load())
	}

	// the hold ran out a minute ago, nothing posted since
	if _, err := svc.Feeds.Put("X", feed, time.Now().Add(-FeedHold-time.Minute)); err != nil {
		t.Fatal(err)
	}
	items, err := svc.WhatsNew(ctx, "X")
	if err != nil || len(items) != 1 || items[0].Title != "From the page" || hits.Load() != 1 {
		t.Fatalf("past the hold: %v %+v (%d hits)", err, items, hits.Load())
	}

	// past the hold and turned away: the last feed, stale
	blocked.Store(true)
	items, err = svc.WhatsNew(httpx.WithRefresh(ctx), "X")
	if !IsStaleFeed(err) || len(items) != 1 || items[0].Title != "From the feed" {
		t.Fatalf("past the hold, turned away: %v %+v", err, items)
	}

	// a new feed starts the hold anew: the page is left alone again
	before := hits.Load()
	if _, err := svc.Feeds.Put("X", feed, time.Now()); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.WhatsNew(httpx.WithRefresh(ctx), "X"); err != nil || hits.Load() != before {
		t.Fatalf("after a new feed: %v, %d more hits", err, hits.Load()-before)
	}
}

// The feeds and their holds survive a restart.
func TestFeedStoreKeepsFeeds(t *testing.T) {
	dir := t.TempDir()
	f, err := OpenFeeds(dir)
	if err != nil {
		t.Fatal(err)
	}
	at := time.Date(2026, 11, 25, 6, 0, 0, 0, time.UTC)
	if _, err := f.Put("X", []model.NewItem{{Title: "kept"}}, at); err != nil {
		t.Fatal(err)
	}
	again, _ := OpenFeeds(dir)
	feed, ok := again.Get("X")
	if !ok || !feed.Received.Equal(at) || len(feed.Items) != 1 || feed.Items[0].Title != "kept" {
		t.Fatalf("after reopening: %v %+v", ok, feed)
	}
	if _, ok := again.Get("E"); ok {
		t.Error("a feed in a language none was posted in")
	}
	var none *FeedStore
	if _, ok := none.Get("X"); ok {
		t.Error("no store, yet a feed")
	}
}
