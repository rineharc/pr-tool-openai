package tui

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textarea"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/rineharc/pr-tool-openai/internal/review"
	"github.com/rineharc/pr-tool-openai/internal/store"
)

type Reviewer interface {
	SubmitReview(context.Context, review.PullRequest, string, string, []review.DraftComment) error
}

type StateStore interface {
	Load(string) store.PRState
	Save(string, store.PRState) error
}

type mode uint8

const (
	browseMode mode = iota
	commentMode
	submitMode
	confirmMode
	helpMode
)

type Model struct {
	pr          review.PullRequest
	reviewer    Reviewer
	store       StateStore
	state       store.PRState
	fileIndex   int
	lineIndex   int
	width       int
	height      int
	mode        mode
	previous    mode
	showFiles   bool
	showThreads bool
	editor      textarea.Model
	event       string
	status      string
	busy        bool
	quitting    bool
}

type submitResult struct{ err error }

func New(pr review.PullRequest, reviewer Reviewer, stateStore StateStore) Model {
	editor := textarea.New()
	editor.Placeholder = "Write a review comment"
	editor.CharLimit = 10_000
	editor.SetHeight(7)
	editor.ShowLineNumbers = false
	state := store.PRState{ViewedFiles: map[string]bool{}}
	if stateStore != nil {
		state = stateStore.Load(pr.Key())
	}
	if state.ViewedFiles == nil {
		state.ViewedFiles = map[string]bool{}
	}
	fileIndex := 0
	for i := range pr.Files {
		if pr.Files[i].Path == state.LastFile {
			fileIndex = i
			break
		}
	}
	return Model{
		pr: pr, reviewer: reviewer, store: stateStore, state: state,
		fileIndex: fileIndex, showFiles: true, showThreads: true, editor: editor,
		event: "COMMENT",
	}
}

func (m Model) Init() tea.Cmd { return textarea.Blink }

func (m Model) Update(message tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := message.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.editor.SetWidth(max(30, min(100, msg.Width-8)))
		return m, nil
	case submitResult:
		m.busy = false
		if msg.err != nil {
			m.status = "Submission failed: " + msg.err.Error()
			m.mode = submitMode
			return m, nil
		}
		m.status = "Review submitted successfully"
		m.state.Drafts = nil
		_ = m.persist()
		m.mode = browseMode
		return m, nil
	case tea.KeyMsg:
		if m.busy {
			return m, nil
		}
		switch m.mode {
		case commentMode:
			return m.updateComment(msg)
		case submitMode:
			return m.updateSubmit(msg)
		case confirmMode:
			return m.updateConfirm(msg)
		case helpMode:
			if msg.String() == "?" || msg.String() == "esc" || msg.String() == "q" {
				m.mode = m.previous
			}
			return m, nil
		default:
			return m.updateBrowse(msg)
		}
	}
	if m.mode == commentMode || m.mode == submitMode {
		var cmd tea.Cmd
		m.editor, cmd = m.editor.Update(message)
		return m, cmd
	}
	return m, nil
}

func (m Model) updateBrowse(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "q", "ctrl+c":
		m.quitting = true
		return m, tea.Quit
	case "?":
		m.previous, m.mode = m.mode, helpMode
	case "j", "down":
		m.moveLine(1)
	case "k", "up":
		m.moveLine(-1)
	case "]":
		m.moveFile(1)
	case "[":
		m.moveFile(-1)
	case "n":
		m.moveChange(1)
	case "p":
		m.moveChange(-1)
	case "N":
		m.nextThread()
	case "f":
		m.showFiles = !m.showFiles
	case "T", "t":
		m.showThreads = !m.showThreads
	case "v":
		if file := m.currentFile(); file != nil {
			m.state.ViewedFiles[file.Path] = !m.state.ViewedFiles[file.Path]
			m.persistStatus()
		}
	case "c":
		line, side, ok := m.currentAnchor()
		if !ok {
			m.status = "Select a changed or context line before commenting"
			break
		}
		m.editor.Reset()
		m.editor.Placeholder = fmt.Sprintf("Comment on %s:%d (%s)", m.currentFile().Path, line, side)
		m.editor.Focus()
		m.mode = commentMode
	case "s":
		m.editor.Reset()
		m.editor.Placeholder = "Optional review summary"
		m.editor.Focus()
		m.mode = submitMode
		m.event = "COMMENT"
	}
	return m, nil
}

func (m Model) updateComment(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.editor.Blur()
		m.mode = browseMode
		return m, nil
	case "ctrl+s":
		body := strings.TrimSpace(m.editor.Value())
		if body == "" {
			m.status = "Comment draft cannot be empty"
			return m, nil
		}
		line, side, ok := m.currentAnchor()
		if !ok {
			m.status = "The selected line is no longer commentable"
			m.mode = browseMode
			return m, nil
		}
		m.state.Drafts = append(m.state.Drafts, review.DraftComment{
			Path: m.currentFile().Path, Line: line, Side: side, Body: body, HeadSHA: m.pr.HeadSHA,
		})
		m.editor.Blur()
		m.mode = browseMode
		m.status = fmt.Sprintf("Saved draft comment (%d pending)", len(m.state.Drafts))
		m.persistStatus()
		return m, nil
	}
	var cmd tea.Cmd
	m.editor, cmd = m.editor.Update(msg)
	return m, cmd
}

func (m Model) updateSubmit(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.editor.Blur()
		m.mode = browseMode
		return m, nil
	case "tab":
		m.event = nextEvent(m.event, 1)
		return m, nil
	case "shift+tab":
		m.event = nextEvent(m.event, -1)
		return m, nil
	case "enter":
		body := strings.TrimSpace(m.editor.Value())
		if m.event == "COMMENT" && body == "" && len(m.state.Drafts) == 0 {
			m.status = "A comment-only review needs a summary or inline draft"
			return m, nil
		}
		if m.event == "REQUEST_CHANGES" && body == "" {
			m.status = "Request changes needs a review summary"
			return m, nil
		}
		m.editor.Blur()
		m.mode = confirmMode
		return m, nil
	}
	var cmd tea.Cmd
	m.editor, cmd = m.editor.Update(msg)
	return m, cmd
}

func (m Model) updateConfirm(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc", "n":
		m.mode = submitMode
	case "y":
		if m.reviewer == nil {
			m.status = "No GitHub reviewer configured"
			m.mode = submitMode
			return m, nil
		}
		m.busy = true
		pr, event, body := m.pr, m.event, strings.TrimSpace(m.editor.Value())
		drafts := append([]review.DraftComment(nil), m.state.Drafts...)
		return m, func() tea.Msg {
			ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
			defer cancel()
			return submitResult{err: m.reviewer.SubmitReview(ctx, pr, event, body, drafts)}
		}
	}
	return m, nil
}

func (m *Model) moveFile(delta int) {
	if len(m.pr.Files) == 0 {
		return
	}
	m.fileIndex = clamp(m.fileIndex+delta, 0, len(m.pr.Files)-1)
	m.lineIndex = 0
	m.state.LastFile = m.pr.Files[m.fileIndex].Path
	m.persistStatus()
}

func (m *Model) moveLine(delta int) {
	file := m.currentFile()
	if file == nil || len(file.Lines) == 0 {
		return
	}
	m.lineIndex = clamp(m.lineIndex+delta, 0, len(file.Lines)-1)
}

func (m *Model) moveChange(direction int) {
	file := m.currentFile()
	if file == nil {
		return
	}
	for offset := 1; offset <= len(file.Lines); offset++ {
		index := m.lineIndex + offset*direction
		if index < 0 || index >= len(file.Lines) {
			break
		}
		if file.Lines[index].Kind == review.Added || file.Lines[index].Kind == review.Removed {
			m.lineIndex = index
			return
		}
	}
}

func (m *Model) nextThread() {
	if len(m.pr.Comments) == 0 {
		m.status = "This PR has no inline review comments"
		return
	}
	currentPath := ""
	currentLine := 0
	if file := m.currentFile(); file != nil {
		currentPath = file.Path
		if line, _, ok := m.currentAnchor(); ok {
			currentLine = line
		}
	}
	start := 0
	for i, comment := range m.pr.Comments {
		if comment.Path == currentPath && comment.Line == currentLine {
			start = i + 1
		}
	}
	comment := m.pr.Comments[start%len(m.pr.Comments)]
	for i := range m.pr.Files {
		if m.pr.Files[i].Path != comment.Path {
			continue
		}
		m.fileIndex = i
		for j, line := range m.pr.Files[i].Lines {
			anchor, side, ok := line.Anchor()
			if ok && anchor == comment.Line && side == comment.Side {
				m.lineIndex = j
				break
			}
		}
		break
	}
	m.showThreads = true
}

func (m Model) currentFile() *review.File {
	if m.fileIndex < 0 || m.fileIndex >= len(m.pr.Files) {
		return nil
	}
	return &m.pr.Files[m.fileIndex]
}

func (m Model) currentAnchor() (int, string, bool) {
	file := m.currentFile()
	if file == nil || m.lineIndex < 0 || m.lineIndex >= len(file.Lines) {
		return 0, "", false
	}
	return file.Lines[m.lineIndex].Anchor()
}

func (m Model) commentsAtCursor() []review.Comment {
	file := m.currentFile()
	line, side, ok := m.currentAnchor()
	if file == nil || !ok {
		return nil
	}
	var result []review.Comment
	for _, comment := range m.pr.Comments {
		if comment.Path == file.Path && comment.Line == line && (comment.Side == side || comment.Side == "") {
			result = append(result, comment)
		}
	}
	return result
}

func (m *Model) persist() error {
	if m.store == nil {
		return nil
	}
	return m.store.Save(m.pr.Key(), m.state)
}

func (m *Model) persistStatus() {
	if err := m.persist(); err != nil {
		m.status = "State save failed: " + err.Error()
	}
}

func clamp(value, low, high int) int {
	if value < low {
		return low
	}
	if value > high {
		return high
	}
	return value
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func nextEvent(current string, direction int) string {
	events := []string{"COMMENT", "APPROVE", "REQUEST_CHANGES"}
	index := 0
	for i, event := range events {
		if event == current {
			index = i
			break
		}
	}
	index = (index + direction + len(events)) % len(events)
	return events[index]
}
