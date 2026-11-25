package service

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/dgrieser/jw-cli/internal/model"
)

// A server whose network jw.org's bot protection turns away cannot read
// jw.org's page of what is new; a machine jw.org lets through can post the
// page's RSS feed to it instead (POST /api/v1/latest/feed). A feed posted
// holds for FeedHold: until then what is new is what it lists, and
// www.jw.org is not asked at all — not by the updates, not by a reader's
// reload. Each feed posted starts the hold anew. Once a hold has run out
// with no feed since, the page is read again; when that fails too, the last
// feed is what is new, until a feed or the page answers.

// FeedHold is how long a posted feed stands in for jw.org's page.
const FeedHold = 24 * time.Hour

// PostedFeed is the feed of what is new last posted in a language.
type PostedFeed struct {
	Lang     string          `json:"lang"`
	Received time.Time       `json:"received"`
	Items    []model.NewItem `json:"items"`
}

// Holds reports whether the feed still stands in for jw.org's page at now.
func (f PostedFeed) Holds(now time.Time) bool { return now.Before(f.Received.Add(FeedHold)) }

// Until is when the feed stops standing in for jw.org's page.
func (f PostedFeed) Until() time.Time { return f.Received.Add(FeedHold) }

// FeedStore keeps the feeds posted, by language, in memory and — with a
// folder — on disk, so a restart keeps them and their holds.
type FeedStore struct {
	dir    string
	mu     sync.Mutex
	byLang map[string]PostedFeed
	loaded map[string]bool
}

// OpenFeeds keeps the posted feeds in dir; an empty dir keeps them in
// memory only.
func OpenFeeds(dir string) (*FeedStore, error) {
	if dir != "" {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return nil, fmt.Errorf("feeds: %w", err)
		}
	}
	return &FeedStore{dir: dir, byLang: map[string]PostedFeed{}, loaded: map[string]bool{}}, nil
}

func (f *FeedStore) path(lang string) string {
	return filepath.Join(f.dir, "whatsnew-"+strings.ToUpper(lang)+".json")
}

// Get is the feed last posted in lang.
func (f *FeedStore) Get(lang string) (PostedFeed, bool) {
	if f == nil {
		return PostedFeed{}, false
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if !f.loaded[lang] && f.dir != "" {
		f.loaded[lang] = true
		if b, err := os.ReadFile(f.path(lang)); err == nil {
			var feed PostedFeed
			if json.Unmarshal(b, &feed) == nil && feed.Lang == lang {
				f.byLang[lang] = feed
			}
		}
	}
	feed, ok := f.byLang[lang]
	return feed, ok
}

// Put keeps the feed posted in lang at now, starting its hold.
func (f *FeedStore) Put(lang string, items []model.NewItem, now time.Time) (PostedFeed, error) {
	feed := PostedFeed{Lang: lang, Received: now.UTC(), Items: items}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.dir != "" {
		b, err := json.Marshal(feed)
		if err != nil {
			return feed, err
		}
		// written aside and moved over, so a crash leaves the last feed
		tmp := f.path(lang) + ".tmp"
		if err := os.WriteFile(tmp, b, 0o600); err != nil {
			return feed, fmt.Errorf("feeds: %w", err)
		}
		if err := os.Rename(tmp, f.path(lang)); err != nil {
			return feed, fmt.Errorf("feeds: %w", err)
		}
	}
	f.byLang[lang], f.loaded[lang] = feed, true
	return feed, nil
}

// StaleFeedError is the page of what is new failing after a posted feed's
// hold ran out: what is new is that feed's still, and the error says since
// when.
type StaleFeedError struct {
	Received time.Time
	Err      error
}

func (e *StaleFeedError) Error() string {
	return fmt.Sprintf("the feed posted %s is shown: %v", e.Received.Format(time.RFC3339), e.Err)
}

func (e *StaleFeedError) Unwrap() error { return e.Err }

// IsStaleFeed reports an error that came with the last posted feed's
// entries, not with none.
func IsStaleFeed(err error) bool {
	var stale *StaleFeedError
	return errors.As(err, &stale)
}
