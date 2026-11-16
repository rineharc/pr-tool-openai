package githubclient

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rineharc/pr-tool-openai/internal/review"
)

func TestLoadPullRequest(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer secret" {
			t.Errorf("authorization header = %q", r.Header.Get("Authorization"))
		}
		switch r.URL.Path {
		case "/repos/acme/repo/pulls/12":
			writeJSON(t, w, map[string]any{
				"number": 12, "title": "Anchor comments", "html_url": "https://example/pull/12",
				"mergeable": true, "mergeable_state": "clean",
				"user": map[string]any{"login": "author"},
				"head": map[string]any{"sha": "abc123", "ref": "feature"},
				"base": map[string]any{"ref": "main"},
			})
		case "/repos/acme/repo/pulls/12/files":
			writeJSON(t, w, []map[string]any{{
				"filename": "main.go", "status": "modified", "additions": 1, "deletions": 1,
				"patch": "@@ -1 +1 @@\n-old\n+new",
			}})
		case "/repos/acme/repo/pulls/12/comments":
			writeJSON(t, w, []map[string]any{{
				"id": 4, "path": "main.go", "line": 1, "side": "RIGHT", "body": "Why?",
				"created_at": "2026-01-01T00:00:00Z", "user": map[string]any{"login": "reviewer"},
			}})
		case "/repos/acme/repo/commits/abc123/check-runs":
			writeJSON(t, w, map[string]any{
				"total_count": 2,
				"check_runs": []map[string]any{
					{"status": "completed", "conclusion": "success"},
					{"status": "in_progress", "conclusion": nil},
				},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client, err := New(server.URL, "secret", server.Client())
	if err != nil {
		t.Fatal(err)
	}
	pr, err := client.LoadPullRequest(context.Background(), "acme", "repo", 12)
	if err != nil {
		t.Fatal(err)
	}
	if pr.Title != "Anchor comments" || len(pr.Files) != 1 || len(pr.Files[0].Lines) == 0 {
		t.Fatalf("unexpected PR: %#v", pr)
	}
	if len(pr.Comments) != 1 || pr.Comments[0].Line != 1 || pr.Checks.Successful != 1 || pr.Checks.Pending != 1 {
		t.Fatalf("unexpected review metadata: comments=%#v checks=%#v", pr.Comments, pr.Checks)
	}
}

func TestSubmitReviewUsesLineSideAnchors(t *testing.T) {
	var received struct {
		CommitID string `json:"commit_id"`
		Event    string `json:"event"`
		Comments []struct {
			Path string `json:"path"`
			Line int    `json:"line"`
			Side string `json:"side"`
			Body string `json:"body"`
		} `json:"comments"`
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/repos/acme/repo/pulls/12/reviews" {
			http.NotFound(w, r)
			return
		}
		if err := json.NewDecoder(r.Body).Decode(&received); err != nil {
			t.Fatal(err)
		}
		w.WriteHeader(http.StatusCreated)
	}))
	defer server.Close()
	client, _ := New(server.URL, "secret", server.Client())
	pr := review.PullRequest{Owner: "acme", Repo: "repo", Number: 12, HeadSHA: "abc123"}
	err := client.SubmitReview(context.Background(), pr, "APPROVE", "Looks good", []review.DraftComment{{
		Path: "main.go", Line: 7, Side: "RIGHT", Body: "Nit", HeadSHA: "abc123",
	}})
	if err != nil {
		t.Fatal(err)
	}
	if received.Event != "APPROVE" || received.CommitID != "abc123" || len(received.Comments) != 1 {
		t.Fatalf("unexpected payload: %#v", received)
	}
	if received.Comments[0].Line != 7 || received.Comments[0].Side != "RIGHT" {
		t.Fatalf("unexpected anchor: %#v", received.Comments[0])
	}
}

func TestSubmitReviewRejectsStaleDraft(t *testing.T) {
	client, _ := New("https://api.github.test", "secret", nil)
	err := client.SubmitReview(context.Background(),
		review.PullRequest{Owner: "a", Repo: "b", Number: 1, HeadSHA: "new"},
		"COMMENT", "", []review.DraftComment{{HeadSHA: "old"}},
	)
	if err == nil {
		t.Fatal("expected stale head error")
	}
}

func TestGetRetriesTransientFailures(t *testing.T) {
	var attempts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if attempts.Add(1) < 3 {
			http.Error(w, "temporary", http.StatusBadGateway)
			return
		}
		writeJSON(t, w, map[string]bool{"ok": true})
	}))
	defer server.Close()
	client, _ := New(server.URL, "", &http.Client{Timeout: 2 * time.Second})
	var value map[string]bool
	if err := client.get(context.Background(), "/retry", &value); err != nil {
		t.Fatal(err)
	}
	if attempts.Load() != 3 || !value["ok"] {
		t.Fatalf("attempts=%d value=%v", attempts.Load(), value)
	}
}

func writeJSON(t *testing.T, w http.ResponseWriter, value any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(value); err != nil {
		t.Fatal(err)
	}
}
