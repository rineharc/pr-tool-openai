package tui

import (
	"fmt"
	"os"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/charmbracelet/lipgloss"
	"github.com/rineharc/pr-tool-openai/internal/review"
)

type palette struct {
	accent, muted, border, added, removed, selected lipgloss.TerminalColor
	useColor                                        bool
}

func activePalette() palette {
	if os.Getenv("NO_COLOR") != "" || strings.EqualFold(os.Getenv("PRR_THEME"), "monochrome") {
		return palette{useColor: false}
	}
	if strings.EqualFold(os.Getenv("PRR_THEME"), "light") {
		return palette{
			accent: lipgloss.Color("25"), muted: lipgloss.Color("243"), border: lipgloss.Color("250"),
			added: lipgloss.Color("22"), removed: lipgloss.Color("88"), selected: lipgloss.Color("254"), useColor: true,
		}
	}
	return palette{
		accent: lipgloss.Color("39"), muted: lipgloss.Color("244"), border: lipgloss.Color("240"),
		added: lipgloss.Color("42"), removed: lipgloss.Color("203"), selected: lipgloss.Color("236"), useColor: true,
	}
}

func (m Model) View() string {
	if m.quitting {
		return ""
	}
	width, height := max(m.width, 70), max(m.height, 20)
	p := activePalette()
	header := m.header(width, p)
	footer := m.footer(width, p)
	bodyHeight := max(8, height-lipgloss.Height(header)-lipgloss.Height(footer))
	var body string
	switch m.mode {
	case commentMode:
		body = m.commentDialog(width, bodyHeight, p)
	case submitMode:
		body = m.submitDialog(width, bodyHeight, p)
	case confirmMode:
		body = m.confirmDialog(width, bodyHeight, p)
	case helpMode:
		body = m.helpDialog(width, bodyHeight, p)
	default:
		body = m.workspace(width, bodyHeight, p)
	}
	return lipgloss.JoinVertical(lipgloss.Left, header, body, footer)
}

func (m Model) header(width int, p palette) string {
	checks := fmt.Sprintf("checks %d/%d", m.pr.Checks.Successful, m.pr.Checks.Total)
	if m.pr.Checks.Failed > 0 {
		checks = fmt.Sprintf("checks %d failed", m.pr.Checks.Failed)
	}
	merge := m.pr.MergeState
	if merge == "" {
		merge = "unknown"
	}
	left := fmt.Sprintf(" %s/%s #%d  %s", m.pr.Owner, m.pr.Repo, m.pr.Number, m.pr.Title)
	right := fmt.Sprintf("%s · %s · %d drafts ", checks, merge, len(m.state.Drafts))
	space := max(1, width-utf8.RuneCountInString(left)-utf8.RuneCountInString(right))
	value := truncate(left, width-len(right)-1) + strings.Repeat(" ", space) + right
	style := lipgloss.NewStyle().Width(width).Bold(true).Reverse(true)
	if p.useColor {
		style = style.Foreground(p.accent)
	}
	return style.Render(value)
}

func (m Model) footer(width int, p palette) string {
	keys := " j/k move  [/] file  n/p change  N thread  c comment  v viewed  s submit  ? help  q quit"
	if m.width < 90 {
		keys = " j/k move  [/] file  c comment  s submit  ? help"
	}
	status := m.status
	if m.busy {
		status = "Submitting review…"
	}
	if status != "" {
		keys = truncate(" "+status+" · "+strings.TrimSpace(keys), width)
	}
	style := lipgloss.NewStyle().Width(width).Foreground(p.muted)
	return style.Render(truncate(keys, width))
}

func (m Model) workspace(width, height int, p palette) string {
	border := lipgloss.NewStyle().Border(lipgloss.NormalBorder()).BorderForeground(p.border)
	fileWidth := 30
	threadWidth := 38
	if width >= 120 {
		diffWidth := width - 2
		var panes []string
		if m.showFiles {
			diffWidth -= fileWidth
			panes = append(panes, border.Width(fileWidth-2).Height(height-2).Render(m.filePane(fileWidth-4, height-2, p)))
		}
		if m.showThreads {
			diffWidth -= threadWidth
		}
		panes = append(panes, border.Width(diffWidth-2).Height(height-2).Render(m.diffPane(diffWidth-4, height-2, p)))
		if m.showThreads {
			panes = append(panes, border.Width(threadWidth-2).Height(height-2).Render(m.threadPane(threadWidth-4, height-2, p)))
		}
		return lipgloss.JoinHorizontal(lipgloss.Top, panes...)
	}
	if width >= 90 && m.showFiles {
		diffWidth := width - fileWidth
		top := lipgloss.JoinHorizontal(lipgloss.Top,
			border.Width(fileWidth-2).Height(height-2).Render(m.filePane(fileWidth-4, height-2, p)),
			border.Width(diffWidth-2).Height(height-2).Render(m.diffPane(diffWidth-4, height-2, p)),
		)
		return top
	}
	content := m.diffPane(width-4, height-2, p)
	if m.showThreads && len(m.commentsAtCursor()) > 0 {
		content = m.threadPane(width-4, height-2, p)
	}
	return border.Width(width - 2).Height(height - 2).Render(content)
}

func (m Model) filePane(width, height int, p palette) string {
	if len(m.pr.Files) == 0 {
		return "FILES\n\nNo changed files returned by GitHub."
	}
	lines := []string{"FILES"}
	visible := max(1, height-2)
	start := clamp(m.fileIndex-visible/2, 0, max(0, len(m.pr.Files)-visible))
	end := min(len(m.pr.Files), start+visible)
	for index := start; index < end; index++ {
		file := m.pr.Files[index]
		marker := " "
		if m.state.ViewedFiles[file.Path] {
			marker = "✓"
		}
		text := fmt.Sprintf("%s %s  +%d -%d", marker, file.Path, file.Additions, file.Deletions)
		style := lipgloss.NewStyle()
		if index == m.fileIndex {
			text = "> " + text
			style = style.Bold(true)
			if p.useColor {
				style = style.Foreground(p.accent)
			}
		} else {
			text = "  " + text
		}
		lines = append(lines, style.Render(truncate(text, width)))
	}
	return strings.Join(lines, "\n")
}

func (m Model) diffPane(width, height int, p palette) string {
	file := m.currentFile()
	if file == nil {
		return "DIFF\n\nNo changed files."
	}
	title := fmt.Sprintf("%s  +%d -%d", file.Path, file.Additions, file.Deletions)
	if m.state.ViewedFiles[file.Path] {
		title += "  [viewed]"
	}
	lines := []string{lipgloss.NewStyle().Bold(true).Render(truncate(title, width))}
	if len(file.Lines) == 0 {
		lines = append(lines, "", "GitHub did not return a textual patch for this file.", "Binary or very large patches must be opened in GitHub.")
		return strings.Join(lines, "\n")
	}
	visible := max(1, height-1)
	start := clamp(m.lineIndex-visible/2, 0, max(0, len(file.Lines)-visible))
	end := min(len(file.Lines), start+visible)
	for index := start; index < end; index++ {
		line := file.Lines[index]
		oldNumber, newNumber := "", ""
		if line.OldLine > 0 {
			oldNumber = fmt.Sprint(line.OldLine)
		}
		if line.NewLine > 0 {
			newNumber = fmt.Sprint(line.NewLine)
		}
		prefix := fmt.Sprintf("%4s %4s ", oldNumber, newNumber)
		content := truncate(prefix+line.Content, width)
		style := lipgloss.NewStyle()
		switch line.Kind {
		case review.Added:
			if p.useColor {
				style = style.Foreground(p.added)
			}
		case review.Removed:
			if p.useColor {
				style = style.Foreground(p.removed)
			}
		case review.Header:
			style = style.Foreground(p.muted)
		}
		if index == m.lineIndex {
			content = ">" + truncate(content, width-1)
			style = style.Bold(true)
			if p.useColor {
				style = style.Background(p.selected)
			}
		} else {
			content = " " + truncate(content, width-1)
		}
		lines = append(lines, style.Render(content))
	}
	return strings.Join(lines, "\n")
}

func (m Model) threadPane(width, height int, p palette) string {
	comments := m.commentsAtCursor()
	line, side, _ := m.currentAnchor()
	lines := []string{lipgloss.NewStyle().Bold(true).Render(fmt.Sprintf("THREAD · line %d %s", line, side))}
	if len(comments) == 0 {
		lines = append(lines, "", "No conversation on this line.", "", "Press c to queue a draft comment.")
		return strings.Join(lines, "\n")
	}
	for _, comment := range comments {
		lines = append(lines, "", lipgloss.NewStyle().Bold(true).Render(truncate(comment.Author+" · "+relativeTime(comment.CreatedAt), width)))
		for _, bodyLine := range wrap(comment.Body, width) {
			if len(lines) >= height {
				break
			}
			lines = append(lines, bodyLine)
		}
	}
	return strings.Join(lines[:min(len(lines), height)], "\n")
}

func (m Model) commentDialog(width, height int, p palette) string {
	file := m.currentFile()
	line, side, _ := m.currentAnchor()
	title := fmt.Sprintf("DRAFT INLINE COMMENT · %s:%d %s", file.Path, line, side)
	content := title + "\n\n" + m.editor.View() + "\n\nCtrl+S save locally · Esc cancel"
	return dialog(content, width, height, p)
}

func (m Model) submitDialog(width, height int, p palette) string {
	action := map[string]string{"COMMENT": "comment", "APPROVE": "approve", "REQUEST_CHANGES": "request changes"}[m.event]
	content := fmt.Sprintf(
		"SUBMIT REVIEW\n\nAction: %s\nPending inline comments: %d\nViewed files: %d/%d\nChecks: %d successful, %d failed, %d pending\n\n%s\n\nTab / Shift+Tab changes action\nEnter review confirmation · Esc cancel",
		action, len(m.state.Drafts), viewedCount(m.state.ViewedFiles), len(m.pr.Files),
		m.pr.Checks.Successful, m.pr.Checks.Failed, m.pr.Checks.Pending, m.editor.View(),
	)
	return dialog(content, width, height, p)
}

func (m Model) confirmDialog(width, height int, p palette) string {
	action := strings.ToLower(strings.ReplaceAll(m.event, "_", " "))
	content := fmt.Sprintf(
		"CONFIRM GITHUB REVIEW\n\nThis will submit %d inline comments and the review action “%s” against head %.12s.\n\nThis action is externally visible and cannot be undone here.\n\n[y] submit to GitHub  [n/Esc] go back",
		len(m.state.Drafts), action, m.pr.HeadSHA,
	)
	return dialog(content, width, height, p)
}

func (m Model) helpDialog(width, height int, p palette) string {
	content := `KEYBOARD HELP

j/k or arrows    move through diff
[/]              previous/next file
n/p              next/previous changed line
N                jump to next inline conversation
c                compose inline draft comment
v                toggle local viewed state
f / T            toggle files / conversation panes
s                review summary and submission
?                toggle help
q                quit

Draft comments are local until the confirmation step.`
	return dialog(content, width, height, p)
}

func dialog(content string, width, height int, p palette) string {
	boxWidth := min(max(54, width-12), 100)
	style := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(p.border).
		Padding(1, 2).
		Width(boxWidth - 6)
	rendered := style.Render(content)
	top := max(0, (height-lipgloss.Height(rendered))/2)
	left := max(0, (width-lipgloss.Width(rendered))/2)
	return strings.Repeat("\n", top) + lipgloss.NewStyle().MarginLeft(left).Render(rendered)
}

func viewedCount(viewed map[string]bool) int {
	count := 0
	for _, value := range viewed {
		if value {
			count++
		}
	}
	return count
}

func relativeTime(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	age := time.Since(value)
	if age < 0 {
		return value.Format("2006-01-02")
	}
	switch {
	case age < time.Minute:
		return "now"
	case age < time.Hour:
		return fmt.Sprintf("%dm", int(age.Minutes()))
	case age < 24*time.Hour:
		return fmt.Sprintf("%dh", int(age.Hours()))
	case age < 30*24*time.Hour:
		return fmt.Sprintf("%dd", int(age.Hours()/24))
	default:
		return value.Format("2006-01-02")
	}
}

func wrap(value string, width int) []string {
	if width <= 1 {
		return []string{value}
	}
	var result []string
	for _, paragraph := range strings.Split(value, "\n") {
		for utf8.RuneCountInString(paragraph) > width {
			runes := []rune(paragraph)
			result = append(result, string(runes[:width]))
			paragraph = string(runes[width:])
		}
		result = append(result, paragraph)
	}
	return result
}

func truncate(value string, width int) string {
	if width <= 0 {
		return ""
	}
	runes := []rune(strings.ReplaceAll(value, "\t", "    "))
	if len(runes) <= width {
		return string(runes)
	}
	if width == 1 {
		return "…"
	}
	return string(runes[:width-1]) + "…"
}
