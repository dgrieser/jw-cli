package service

import (
	"container/list"
	"context"
	"strconv"
	"strings"
	"sync"

	"github.com/dgrieser/jw-cli/internal/api/wol"
	"github.com/dgrieser/jw-cli/internal/bibleref"
	"github.com/dgrieser/jw-cli/internal/model"
	"github.com/dgrieser/jw-cli/internal/unfold"
)

// An expansion reads the same verses over and over: John 1:1 is cited by the
// study notes of one verse, the index passages of the next and the article on
// the page, each time through a citation link of its own — wol names a
// citation by the document and the position it is written at, not by what it
// points at. The disk cache keeps what each link answered, but only for that
// link, and only once it has been asked. So the service keeps in memory what it
// has read of the library, by what it is: a verse by the verses it holds, a
// chapter's study pane and verses by the chapter. A citation is told apart by
// its text before it is asked about ("Joh 1:1"), and a verse already read —
// through any link, or as part of its chapter — is never read again.

// memo is what the service keeps in memory of the library. Its zero value is
// ready to use, and it is safe for concurrent use.
type memo struct {
	once     sync.Once
	tips     *lru[model.Tooltip]
	verses   *lru[model.Tooltip]
	chapters *lru[*chapterMemo]
}

// How much is kept: a citation answer is a paragraph, a chapter its study pane
// — the notes and index entries of every verse — which is far more.
const (
	memoTips     = 20_000
	memoVerses   = 20_000
	memoChapters = 300
)

func (m *memo) init() {
	m.once.Do(func() {
		m.tips = newLRU[model.Tooltip](memoTips)
		m.verses = newLRU[model.Tooltip](memoVerses)
		m.chapters = newLRU[*chapterMemo](memoChapters)
	})
}

// chapterMemo is what a chapter page of the study bible says, verse by verse:
// its study pane, and the text of each verse as the page writes it.
type chapterMemo struct {
	sections map[int]model.StudySection
	verses   map[int]string
	url      string
}

// newChapterMemo extracts what a chapter page says, so the parsed page can go.
func newChapterMemo(doc *wol.ChapterDoc) *chapterMemo {
	m := &chapterMemo{sections: studySections(doc), verses: map[int]string{}, url: doc.URL}
	if verses, err := doc.Verses(0, 0); err == nil {
		for _, v := range verses {
			m.verses[v.ID%1000] = v.HTML
		}
	}
	return m
}

func tipKey(lng model.Language, path string) string { return lng.Locale + "|" + RefPath(path) }

func chapterKey(lng model.Language, edition string, book, chapter int) string {
	return lng.Locale + "|" + edition + "|" + strconv.Itoa(book) + "|" + strconv.Itoa(chapter)
}

// versesKey names a passage of bible text by the verses it holds.
func versesKey(lng model.Language, ids []int) string {
	var b strings.Builder
	b.WriteString(lng.Locale)
	for _, id := range ids {
		b.WriteByte('|')
		b.WriteString(strconv.Itoa(id))
	}
	return b.String()
}

// rememberTip keeps what a citation answered, under its link and, for bible
// text, under the verses it holds.
func (m *memo) rememberTip(lng model.Language, path string, tip model.Tooltip) {
	m.init()
	m.tips.put(tipKey(lng, path), tip)
	if unfold.IsCitation(path) && (unfold.Ref{Path: path}).IsVerse() {
		if ids := unfold.VerseIDs(tip.ContentHTML); len(ids) > 0 {
			m.verses.put(versesKey(lng, ids), tip)
		}
	}
}

// citedVerses are the verses a bible citation names by its text, or none when
// the text does not say — a bare "+", a verse number standing alone, a whole
// chapter.
func citedVerses(text string, table *bibleref.Table) []int {
	text = strings.TrimRight(strings.TrimSpace(text), ",;.")
	if text == "" || table == nil {
		return nil
	}
	refs, err := bibleref.Parse(text, table)
	if err != nil || len(refs) == 0 {
		return nil
	}
	var ids []int
	for _, ref := range refs {
		if ref.VerseStart <= 0 || ref.RunsToChapterEnd() || ref.VerseEnd < ref.VerseStart {
			return nil
		}
		for v := ref.VerseStart; v <= max(ref.VerseEnd, ref.VerseStart); v++ {
			ids = append(ids, ref.Book*1_000_000+ref.Chapter*1_000+v)
		}
	}
	return ids
}

// VersesOf answers unfold.KnownResolver: the verses a bible citation names by
// its text.
func (r *tooltipResolver) VersesOf(ctx context.Context, ref unfold.Ref) []int {
	if !ref.IsVerse() {
		return nil
	}
	if r.table == nil {
		r.table = r.s.BookTable(ctx, r.lng)
	}
	return citedVerses(ref.Text, r.table)
}

// Known answers unfold.KnownResolver: what a citation says when it was read
// already — through this link, through another link to the same verses, or as
// part of a chapter of the study bible read before.
func (r *tooltipResolver) Known(ctx context.Context, ref unfold.Ref) (model.Tooltip, bool) {
	if tip, ok := r.tips[ref.Path]; ok {
		return tip, true
	}
	m := &r.s.mem
	m.init()
	if tip, ok := m.tips.get(tipKey(r.lng, ref.Path)); ok {
		r.keep(ref.Path, tip)
		return tip, true
	}
	ids := r.VersesOf(ctx, ref)
	if len(ids) == 0 {
		return model.Tooltip{}, false
	}
	if tip, ok := m.verses.get(versesKey(r.lng, ids)); ok {
		r.keep(ref.Path, tip)
		return tip, true
	}
	// the verses of one chapter read before, written as the citation would
	book, chapter := ids[0]/1_000_000, ids[0]/1_000%1_000
	cm, ok := r.chapterMemo(book, chapter)
	if !ok {
		return model.Tooltip{}, false
	}
	var text strings.Builder
	for _, id := range ids {
		html, ok := cm.verses[id%1000]
		if !ok || id/1_000_000 != book || id/1_000%1_000 != chapter {
			return model.Tooltip{}, false
		}
		text.WriteString(paragraph(html))
	}
	first, last := ids[0]%1000, ids[len(ids)-1]%1000
	tip := model.Tooltip{
		Title:       RefString(bibleref.Ref{Book: book, Chapter: chapter, VerseStart: first, VerseEnd: last}, r.table),
		ContentHTML: text.String(),
		URL:         cm.url,
	}
	r.keep(ref.Path, tip)
	return tip, true
}

// keep remembers a citation's answer for the rest of this run.
func (r *tooltipResolver) keep(path string, tip model.Tooltip) {
	if r.tips == nil {
		r.tips = map[string]model.Tooltip{}
	}
	r.tips[path] = tip
}

// chapterMemo is what the study bible's page of a chapter says, when the
// service read it before.
func (r *tooltipResolver) chapterMemo(book, chapter int) (*chapterMemo, bool) {
	r.s.mem.init()
	return r.s.mem.chapters.get(chapterKey(r.lng, studyEdition, book, chapter))
}

// lru is a map that forgets what was used least recently once it holds max
// entries. It is safe for concurrent use.
type lru[V any] struct {
	mu    sync.Mutex
	max   int
	order *list.List
	items map[string]*list.Element
}

type lruEntry[V any] struct {
	key string
	val V
}

func newLRU[V any](max int) *lru[V] {
	return &lru[V]{max: max, order: list.New(), items: map[string]*list.Element{}}
}

func (l *lru[V]) get(key string) (V, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if e, ok := l.items[key]; ok {
		l.order.MoveToFront(e)
		return e.Value.(*lruEntry[V]).val, true
	}
	var zero V
	return zero, false
}

func (l *lru[V]) put(key string, val V) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if e, ok := l.items[key]; ok {
		e.Value.(*lruEntry[V]).val = val
		l.order.MoveToFront(e)
		return
	}
	l.items[key] = l.order.PushFront(&lruEntry[V]{key: key, val: val})
	if l.order.Len() > l.max {
		last := l.order.Back()
		l.order.Remove(last)
		delete(l.items, last.Value.(*lruEntry[V]).key)
	}
}
