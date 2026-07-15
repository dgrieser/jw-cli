// Package tui is the interactive terminal UI for browsing listings (search
// results, media categories): a list with a detail pane, downloads, and
// pagination, driven by callbacks so every listing command can reuse it.
package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/dgrieser/jw-cli/internal/model"
	"github.com/dgrieser/jw-cli/internal/results"
)

// Fetcher loads one page of a listing (1-based) and returns it with a
// human header.
type Fetcher func(page int) (results.ResultSet, string, error)

// Actions supplies the behavior behind the key bindings.
type Actions struct {
	// Show returns rendered content for the detail pane.
	Show func(item model.Result) (string, error)
	// Download fetches the item and returns the saved path.
	Download func(item model.Result) (string, error)
	// Open opens the item's link externally (optional).
	Open func(item model.Result) error
	// Browse returns a new Fetcher when the item is a container (optional).
	Browse func(item model.Result) (Fetcher, string, bool)
}

// Run starts the TUI over the given fetcher.
func Run(header string, fetch Fetcher, actions Actions) error {
	m := newModel(header, fetch, actions)
	p := tea.NewProgram(m, tea.WithAltScreen())
	final, err := p.Run()
	if err != nil {
		return err
	}
	if fm, ok := final.(uiModel); ok && fm.fatal != nil {
		return fm.fatal
	}
	return nil
}

type item struct{ r model.Result }

func (i item) Title() string {
	t := fmt.Sprintf("[%s] %s", i.r.Kind, i.r.Title)
	if i.r.Duration != "" {
		t += " (" + i.r.Duration + ")"
	}
	return t
}

func (i item) Description() string {
	if i.r.Snippet != "" {
		return i.r.Snippet
	}
	return i.r.Context
}

func (i item) FilterValue() string { return i.r.Title + " " + i.r.Snippet }

type level struct {
	fetch  Fetcher
	header string
	page   int
}

type keyMap struct {
	Show, Download, Open, Next, Prev, Back, Quit key.Binding
}

var keys = keyMap{
	Show:     key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "view")),
	Download: key.NewBinding(key.WithKeys("d"), key.WithHelp("d", "download")),
	Open:     key.NewBinding(key.WithKeys("o"), key.WithHelp("o", "open link")),
	Next:     key.NewBinding(key.WithKeys("n", "right"), key.WithHelp("n", "next page")),
	Prev:     key.NewBinding(key.WithKeys("p", "left"), key.WithHelp("p", "prev page")),
	Back:     key.NewBinding(key.WithKeys("esc", "backspace"), key.WithHelp("esc", "back")),
	Quit:     key.NewBinding(key.WithKeys("q", "ctrl+c"), key.WithHelp("q", "quit")),
}

type uiModel struct {
	actions Actions
	stack   []level

	list     list.Model
	viewport viewport.Model
	spin     spinner.Model

	mode    string // list | detail | busy
	status  string
	width   int
	height  int
	fatal   error
	content string
}

type pageMsg struct {
	rs     results.ResultSet
	header string
	page   int
	err    error
}
type contentMsg struct {
	text string
	err  error
}
type downloadMsg struct {
	path string
	err  error
}

func newModel(header string, fetch Fetcher, actions Actions) uiModel {
	l := list.New(nil, list.NewDefaultDelegate(), 0, 0)
	l.Title = header
	l.SetShowStatusBar(false)
	l.AdditionalShortHelpKeys = func() []key.Binding {
		return []key.Binding{keys.Show, keys.Download, keys.Open, keys.Next, keys.Prev}
	}
	sp := spinner.New()
	sp.Spinner = spinner.Dot
	return uiModel{
		actions: actions,
		stack:   []level{{fetch: fetch, header: header, page: 1}},
		list:    l,
		spin:    sp,
		mode:    "busy",
		status:  "loading…",
	}
}

func (m uiModel) top() *level { return &m.stack[len(m.stack)-1] }

func (m uiModel) Init() tea.Cmd {
	return tea.Batch(m.spin.Tick, m.loadPage(1))
}

func (m uiModel) loadPage(page int) tea.Cmd {
	lv := *m.top()
	return func() tea.Msg {
		rs, header, err := lv.fetch(page)
		return pageMsg{rs: rs, header: header, page: page, err: err}
	}
}

func (m uiModel) selected() (model.Result, bool) {
	if it, ok := m.list.SelectedItem().(item); ok {
		return it.r, true
	}
	return model.Result{}, false
}

func (m uiModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.list.SetSize(msg.Width, msg.Height-1)
		m.viewport = viewport.New(msg.Width, msg.Height-2)
		m.viewport.SetContent(m.content)
		return m, nil

	case pageMsg:
		if msg.err != nil {
			if len(m.stack) == 1 && len(m.list.Items()) == 0 {
				m.fatal = msg.err
				return m, tea.Quit
			}
			m.mode = "list"
			m.status = "error: " + msg.err.Error()
			return m, nil
		}
		items := make([]list.Item, 0, len(msg.rs.Items))
		for _, r := range msg.rs.Items {
			items = append(items, item{r: r})
		}
		m.list.SetItems(items)
		m.list.Select(0)
		if msg.header != "" {
			m.list.Title = msg.header
		}
		m.top().page = msg.page
		m.mode = "list"
		m.status = ""
		return m, nil

	case contentMsg:
		if msg.err != nil {
			m.mode = "list"
			m.status = "error: " + msg.err.Error()
			return m, nil
		}
		m.content = msg.text
		if m.width > 0 {
			m.viewport = viewport.New(m.width, m.height-2)
		}
		m.viewport.SetContent(msg.text)
		m.mode = "detail"
		m.status = ""
		return m, nil

	case downloadMsg:
		m.mode = "list"
		if msg.err != nil {
			m.status = "download failed: " + msg.err.Error()
		} else {
			m.status = "downloaded " + msg.path
		}
		return m, nil

	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spin, cmd = m.spin.Update(msg)
		return m, cmd

	case tea.KeyMsg:
		if key.Matches(msg, keys.Quit) && m.mode != "detail" {
			return m, tea.Quit
		}
		switch m.mode {
		case "detail":
			if key.Matches(msg, keys.Back) || key.Matches(msg, keys.Quit) {
				m.mode = "list"
				return m, nil
			}
			var cmd tea.Cmd
			m.viewport, cmd = m.viewport.Update(msg)
			return m, cmd

		case "list":
			if m.list.FilterState() == list.Filtering {
				break
			}
			switch {
			case key.Matches(msg, keys.Show):
				r, ok := m.selected()
				if !ok {
					return m, nil
				}
				if m.actions.Browse != nil {
					if fetch, header, isContainer := m.actions.Browse(r); isContainer {
						m.stack = append(m.stack, level{fetch: fetch, header: header, page: 1})
						m.mode = "busy"
						m.status = "loading…"
						return m, tea.Batch(m.spin.Tick, m.loadPage(1))
					}
				}
				if m.actions.Show == nil {
					return m, nil
				}
				m.mode = "busy"
				m.status = "loading…"
				return m, tea.Batch(m.spin.Tick, func() tea.Msg {
					text, err := m.actions.Show(r)
					return contentMsg{text: text, err: err}
				})
			case key.Matches(msg, keys.Download):
				r, ok := m.selected()
				if !ok || m.actions.Download == nil {
					return m, nil
				}
				m.mode = "busy"
				m.status = "downloading " + r.Title + "…"
				return m, tea.Batch(m.spin.Tick, func() tea.Msg {
					path, err := m.actions.Download(r)
					return downloadMsg{path: path, err: err}
				})
			case key.Matches(msg, keys.Open):
				if r, ok := m.selected(); ok && m.actions.Open != nil {
					if err := m.actions.Open(r); err != nil {
						m.status = "open failed: " + err.Error()
					} else {
						m.status = "opened " + r.Title
					}
				}
				return m, nil
			case key.Matches(msg, keys.Next):
				m.mode = "busy"
				m.status = "loading…"
				return m, tea.Batch(m.spin.Tick, m.loadPage(m.top().page+1))
			case key.Matches(msg, keys.Prev):
				if m.top().page > 1 {
					m.mode = "busy"
					m.status = "loading…"
					return m, tea.Batch(m.spin.Tick, m.loadPage(m.top().page-1))
				}
				return m, nil
			case key.Matches(msg, keys.Back):
				if len(m.stack) > 1 {
					m.stack = m.stack[:len(m.stack)-1]
					m.mode = "busy"
					m.status = "loading…"
					return m, tea.Batch(m.spin.Tick, m.loadPage(m.top().page))
				}
				return m, tea.Quit
			}
		}
	}

	var cmd tea.Cmd
	m.list, cmd = m.list.Update(msg)
	return m, cmd
}

var statusStyle = lipgloss.NewStyle().Faint(true)

func (m uiModel) View() string {
	switch m.mode {
	case "busy":
		return fmt.Sprintf("\n %s %s\n", m.spin.View(), m.status)
	case "detail":
		help := statusStyle.Render("↑/↓ scroll · esc back · q quit")
		return m.viewport.View() + "\n" + help
	default:
		s := m.list.View()
		if m.status != "" {
			s += "\n" + statusStyle.Render(truncate(m.status, max(m.width-2, 20)))
		}
		return s
	}
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return strings.TrimSpace(s[:n-1]) + "…"
}
