package tui

import (
	"context"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/rineharc/pr-tool-openai/internal/review"
	"github.com/rineharc/pr-tool-openai/internal/store"
)

type memoryStore struct {
	state store.PRState
	saves int
}

func (s *memoryStore) Load(string) store.PRState { return s.state }
func (s *memoryStore) Save(_ string, state store.PRState) error {
	s.state = state
	s.saves++
	return nil
}

type recordingReviewer struct {
	called   bool
	event    string
	body     string
	comments []review.DraftComment
}

func (r *recordingReviewer) SubmitReview(_ context.Context, _ review.PullRequest, event, body string, comments []review.DraftComment) error {
	r.called = true
	r.event = event
	r.body = body
	r.comments = comments
	return nil
}

func fixturePR() review.PullRequest {
	patch := "@@ -1,2 +1,2 @@\n-old\n+new\n context"
	return review.PullRequest{
		Owner: "acme", Repo: "repo", Number: 7, Title: "Test review", HeadSHA: "abc123",
		Files: []review.File{{
			Path: "main.go", Additions: 1, Deletions: 1, Patch: patch, Lines: review.ParsePatch(patch),
		}},
		Comments: []review.Comment{{Path: "main.go", Line: 1, Side: "RIGHT", Author: "sam", Body: "Please explain."}},
		Checks:   review.CheckSummary{Total: 1, Successful: 1},
	}
}

func update(t *testing.T, model Model, msg tea.Msg) (Model, tea.Cmd) {
	t.Helper()
	next, cmd := model.Update(msg)
	got, ok := next.(Model)
	if !ok {
		t.Fatalf("model type = %T", next)
	}
	return got, cmd
}

func TestDraftAndSubmitReviewFlow(t *testing.T) {
	state := &memoryStore{state: store.PRState{ViewedFiles: map[string]bool{}}}
	reviewer := &recordingReviewer{}
	model := New(fixturePR(), reviewer, state)
	model, _ = update(t, model, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("n")})
	model, _ = update(t, model, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("c")})
	if model.mode != commentMode {
		t.Fatalf("mode = %v, want comment", model.mode)
	}
	model, _ = update(t, model, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("Please add a test.")})
	model, _ = update(t, model, tea.KeyMsg{Type: tea.KeyCtrlS})
	if len(model.state.Drafts) != 1 || model.state.Drafts[0].Line != 1 || model.state.Drafts[0].Side != "LEFT" {
		t.Fatalf("unexpected draft: %#v", model.state.Drafts)
	}
	if state.saves == 0 {
		t.Fatal("draft was not persisted")
	}

	model, _ = update(t, model, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("s")})
	model, _ = update(t, model, tea.KeyMsg{Type: tea.KeyTab})
	model, _ = update(t, model, tea.KeyMsg{Type: tea.KeyEnter})
	if model.mode != confirmMode {
		t.Fatalf("mode = %v, want confirm", model.mode)
	}
	var command tea.Cmd
	model, command = update(t, model, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y")})
	if command == nil || !model.busy {
		t.Fatal("expected asynchronous submission")
	}
	model, _ = update(t, model, command())
	if !reviewer.called || reviewer.event != "APPROVE" || reviewer.body != "" || len(reviewer.comments) != 1 {
		t.Fatalf("review call = %#v", reviewer)
	}
	if len(model.state.Drafts) != 0 || model.status != "Review submitted successfully" {
		t.Fatalf("unexpected final state: drafts=%v status=%q", model.state.Drafts, model.status)
	}
}

func TestResponsiveViews(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	for _, test := range []struct {
		name      string
		width     int
		want      string
		doNotWant string
	}{
		{"wide", 130, "THREAD", ""},
		{"standard", 100, "FILES", ""},
		{"compact", 72, "main.go", "FILES"},
	} {
		t.Run(test.name, func(t *testing.T) {
			model := New(fixturePR(), nil, nil)
			model, _ = update(t, model, tea.WindowSizeMsg{Width: test.width, Height: 30})
			view := model.View()
			if !strings.Contains(view, test.want) {
				t.Fatalf("view does not contain %q:\n%s", test.want, view)
			}
			if test.doNotWant != "" && strings.Contains(view, test.doNotWant) {
				t.Fatalf("view unexpectedly contains %q:\n%s", test.doNotWant, view)
			}
		})
	}
}

func TestNavigationAndViewedState(t *testing.T) {
	pr := fixturePR()
	secondPatch := "@@ -3 +3 @@\n-old\n+new"
	pr.Files = append(pr.Files, review.File{Path: "other.go", Patch: secondPatch, Lines: review.ParsePatch(secondPatch)})
	state := &memoryStore{state: store.PRState{ViewedFiles: map[string]bool{}}}
	model := New(pr, nil, state)
	model, _ = update(t, model, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("]")})
	model, _ = update(t, model, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("v")})
	if model.fileIndex != 1 || !model.state.ViewedFiles["other.go"] || model.state.LastFile != "other.go" {
		t.Fatalf("unexpected navigation state: index=%d state=%#v", model.fileIndex, model.state)
	}
}
