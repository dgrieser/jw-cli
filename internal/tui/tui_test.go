package tui

import (
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/dgrieser/jw-cli/internal/model"
	"github.com/dgrieser/jw-cli/internal/results"
)

func fakeFetcher(pages map[int][]model.Result) Fetcher {
	return func(page int) (results.ResultSet, string, error) {
		items, ok := pages[page]
		if !ok {
			return results.ResultSet{}, "", fmt.Errorf("no page %d", page)
		}
		return results.ResultSet{Items: items, Page: page}, fmt.Sprintf("page %d", page), nil
	}
}

func newTestModel(t *testing.T, fetch Fetcher, actions Actions) uiModel {
	t.Helper()
	m := newModel("test", fetch, actions)
	// size the model
	next, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m = next.(uiModel)
	// load page 1 directly
	msg := m.loadPage(1)()
	next, _ = m.Update(msg)
	return next.(uiModel)
}

func TestListLoadsAndPaginates(t *testing.T) {
	fetch := fakeFetcher(map[int][]model.Result{
		1: {{Title: "First", Kind: "article"}, {Title: "Second", Kind: "video", Duration: "3:10"}},
		2: {{Title: "Third", Kind: "article"}},
	})
	m := newTestModel(t, fetch, Actions{})
	if m.mode != "list" || len(m.list.Items()) != 2 {
		t.Fatalf("mode=%s items=%d", m.mode, len(m.list.Items()))
	}
	if !strings.Contains(m.list.Items()[1].(item).Title(), "[video] Second (3:10)") {
		t.Errorf("item title: %s", m.list.Items()[1].(item).Title())
	}

	// next page
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}})
	m = next.(uiModel)
	if m.mode != "busy" {
		t.Fatalf("expected busy while paging, got %s", m.mode)
	}
	m = applyPage(t, m, cmd)
	if m.top().page != 2 || len(m.list.Items()) != 1 {
		t.Fatalf("page=%d items=%d", m.top().page, len(m.list.Items()))
	}

	// page 3 fails -> stays usable with an error status
	next, cmd = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}})
	m = applyPage(t, next.(uiModel), cmd)
	if m.mode != "list" || !strings.Contains(m.status, "error") {
		t.Fatalf("mode=%s status=%q", m.mode, m.status)
	}
}

func applyPage(t *testing.T, m uiModel, cmd tea.Cmd) uiModel {
	t.Helper()
	if cmd == nil {
		t.Fatal("expected a command")
	}
	msg := cmd()
	if batch, ok := msg.(tea.BatchMsg); ok {
		for _, c := range batch {
			if got := c(); got != nil {
				if _, isPage := got.(pageMsg); isPage {
					next, _ := m.Update(got)
					return next.(uiModel)
				}
			}
		}
		t.Fatal("no pageMsg in batch")
	}
	next, _ := m.Update(msg)
	return next.(uiModel)
}

func TestShowAndBack(t *testing.T) {
	fetch := fakeFetcher(map[int][]model.Result{1: {{Title: "Doc", Kind: "article", DocID: 1}}})
	m := newTestModel(t, fetch, Actions{
		Show: func(r model.Result) (string, error) { return "CONTENT of " + r.Title, nil },
	})
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(uiModel)
	msg := findMsg(t, cmd, func(msg tea.Msg) bool { _, ok := msg.(contentMsg); return ok })
	next, _ = m.Update(msg)
	m = next.(uiModel)
	if m.mode != "detail" || !strings.Contains(m.viewport.View(), "CONTENT of Doc") {
		t.Fatalf("mode=%s view=%q", m.mode, m.viewport.View())
	}
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = next.(uiModel)
	if m.mode != "list" {
		t.Fatalf("esc should return to list, mode=%s", m.mode)
	}
}

func TestDownloadStatus(t *testing.T) {
	fetch := fakeFetcher(map[int][]model.Result{1: {{Title: "Video", Kind: "video", LANK: "x"}}})
	m := newTestModel(t, fetch, Actions{
		Download: func(r model.Result) (string, error) { return "/tmp/file.mp4", nil },
	})
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'d'}})
	m = next.(uiModel)
	msg := findMsg(t, cmd, func(msg tea.Msg) bool { _, ok := msg.(downloadMsg); return ok })
	next, _ = m.Update(msg)
	m = next.(uiModel)
	if m.mode != "list" || !strings.Contains(m.status, "/tmp/file.mp4") {
		t.Fatalf("mode=%s status=%q", m.mode, m.status)
	}
}

func TestBrowseIntoCategory(t *testing.T) {
	sub := fakeFetcher(map[int][]model.Result{1: {{Title: "Inner", Kind: "video"}}})
	fetch := fakeFetcher(map[int][]model.Result{1: {{Title: "Cat", Kind: "category", CategoryKey: "K"}}})
	m := newTestModel(t, fetch, Actions{
		Browse: func(r model.Result) (Fetcher, string, bool) {
			if r.Kind == "category" {
				return sub, r.Title, true
			}
			return nil, "", false
		},
	})
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(uiModel)
	msg := findMsg(t, cmd, func(msg tea.Msg) bool { _, ok := msg.(pageMsg); return ok })
	next, _ = m.Update(msg)
	m = next.(uiModel)
	if len(m.stack) != 2 || m.list.Items()[0].(item).r.Title != "Inner" {
		t.Fatalf("stack=%d first=%v", len(m.stack), m.list.Items()[0])
	}
	// back pops the stack
	next, cmd = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = next.(uiModel)
	msg = findMsg(t, cmd, func(msg tea.Msg) bool { _, ok := msg.(pageMsg); return ok })
	next, _ = m.Update(msg)
	m = next.(uiModel)
	if len(m.stack) != 1 || m.list.Items()[0].(item).r.Title != "Cat" {
		t.Fatalf("back failed: stack=%d", len(m.stack))
	}
}

// findMsg runs cmd (recursing into batches) until pred matches.
func findMsg(t *testing.T, cmd tea.Cmd, pred func(tea.Msg) bool) tea.Msg {
	t.Helper()
	if cmd == nil {
		t.Fatal("nil command")
	}
	msg := cmd()
	if pred(msg) {
		return msg
	}
	if batch, ok := msg.(tea.BatchMsg); ok {
		for _, c := range batch {
			if c == nil {
				continue
			}
			if got := c(); got != nil && pred(got) {
				return got
			}
		}
	}
	t.Fatalf("wanted message not produced, got %T", msg)
	return nil
}
