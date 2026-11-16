package githubclient

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/rineharc/pr-tool-openai/internal/review"
)

type Client struct {
	baseURL    string
	token      string
	httpClient *http.Client
}

func New(baseURL, token string, httpClient *http.Client) (*Client, error) {
	if baseURL == "" {
		baseURL = "https://api.github.com"
	}
	parsed, err := url.Parse(baseURL)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return nil, fmt.Errorf("invalid GitHub API URL %q", baseURL)
	}
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 30 * time.Second}
	}
	return &Client{
		baseURL:    strings.TrimRight(baseURL, "/"),
		token:      token,
		httpClient: httpClient,
	}, nil
}

type pullResponse struct {
	Number     int    `json:"number"`
	Title      string `json:"title"`
	HTMLURL    string `json:"html_url"`
	Mergeable  *bool  `json:"mergeable"`
	MergeState string `json:"mergeable_state"`
	User       struct {
		Login string `json:"login"`
	} `json:"user"`
	Head struct {
		SHA string `json:"sha"`
		Ref string `json:"ref"`
	} `json:"head"`
	Base struct {
		Ref string `json:"ref"`
	} `json:"base"`
}

type fileResponse struct {
	Filename  string `json:"filename"`
	Status    string `json:"status"`
	Additions int    `json:"additions"`
	Deletions int    `json:"deletions"`
	Patch     string `json:"patch"`
}

type commentResponse struct {
	ID           int64     `json:"id"`
	InReplyToID  int64     `json:"in_reply_to_id"`
	Path         string    `json:"path"`
	Line         *int      `json:"line"`
	OriginalLine *int      `json:"original_line"`
	Side         string    `json:"side"`
	OriginalSide string    `json:"original_side"`
	Body         string    `json:"body"`
	CreatedAt    time.Time `json:"created_at"`
	User         struct {
		Login string `json:"login"`
	} `json:"user"`
}

func (c *Client) LoadPullRequest(ctx context.Context, owner, repo string, number int) (review.PullRequest, error) {
	prefix := fmt.Sprintf("/repos/%s/%s", url.PathEscape(owner), url.PathEscape(repo))
	var pull pullResponse
	if err := c.get(ctx, fmt.Sprintf("%s/pulls/%d", prefix, number), &pull); err != nil {
		return review.PullRequest{}, err
	}
	files, err := c.loadFiles(ctx, prefix, number)
	if err != nil {
		return review.PullRequest{}, err
	}
	comments, err := c.loadComments(ctx, prefix, number)
	if err != nil {
		return review.PullRequest{}, err
	}
	checks, _ := c.loadChecks(ctx, prefix, pull.Head.SHA)
	return review.PullRequest{
		Owner: owner, Repo: repo, Number: pull.Number, Title: pull.Title,
		Author: pull.User.Login, HeadSHA: pull.Head.SHA, BaseRef: pull.Base.Ref,
		HeadRef: pull.Head.Ref, Mergeable: pull.Mergeable, MergeState: pull.MergeState,
		HTMLURL: pull.HTMLURL, Files: files, Comments: comments, Checks: checks,
	}, nil
}

func (c *Client) loadFiles(ctx context.Context, prefix string, number int) ([]review.File, error) {
	var result []review.File
	for page := 1; ; page++ {
		var payload []fileResponse
		path := fmt.Sprintf("%s/pulls/%d/files?per_page=100&page=%d", prefix, number, page)
		if err := c.get(ctx, path, &payload); err != nil {
			return nil, err
		}
		for _, file := range payload {
			result = append(result, review.File{
				Path: file.Filename, Status: file.Status, Additions: file.Additions,
				Deletions: file.Deletions, Patch: file.Patch, Lines: review.ParsePatch(file.Patch),
			})
		}
		if len(payload) < 100 {
			return result, nil
		}
	}
}

func (c *Client) loadComments(ctx context.Context, prefix string, number int) ([]review.Comment, error) {
	var result []review.Comment
	for page := 1; ; page++ {
		var payload []commentResponse
		path := fmt.Sprintf("%s/pulls/%d/comments?per_page=100&page=%d", prefix, number, page)
		if err := c.get(ctx, path, &payload); err != nil {
			return nil, err
		}
		for _, comment := range payload {
			line := comment.Line
			side := comment.Side
			if line == nil {
				line, side = comment.OriginalLine, comment.OriginalSide
			}
			if line == nil {
				continue
			}
			result = append(result, review.Comment{
				ID: comment.ID, InReplyToID: comment.InReplyToID, Path: comment.Path,
				Line: *line, Side: side, Body: comment.Body, Author: comment.User.Login,
				CreatedAt: comment.CreatedAt,
			})
		}
		if len(payload) < 100 {
			return result, nil
		}
	}
}

func (c *Client) loadChecks(ctx context.Context, prefix, sha string) (review.CheckSummary, error) {
	var payload struct {
		Total int `json:"total_count"`
		Runs  []struct {
			Status     string `json:"status"`
			Conclusion string `json:"conclusion"`
		} `json:"check_runs"`
	}
	if err := c.get(ctx, fmt.Sprintf("%s/commits/%s/check-runs?per_page=100", prefix, sha), &payload); err != nil {
		return review.CheckSummary{}, err
	}
	summary := review.CheckSummary{Total: payload.Total}
	for _, run := range payload.Runs {
		if run.Status != "completed" {
			summary.Pending++
		} else if run.Conclusion == "success" || run.Conclusion == "neutral" || run.Conclusion == "skipped" {
			summary.Successful++
		} else {
			summary.Failed++
		}
	}
	return summary, nil
}

func (c *Client) SubmitReview(ctx context.Context, pr review.PullRequest, event, body string, comments []review.DraftComment) error {
	type apiComment struct {
		Path      string `json:"path"`
		Line      int    `json:"line"`
		Side      string `json:"side"`
		StartLine int    `json:"start_line,omitempty"`
		StartSide string `json:"start_side,omitempty"`
		Body      string `json:"body"`
	}
	payload := struct {
		CommitID string       `json:"commit_id"`
		Event    string       `json:"event"`
		Body     string       `json:"body,omitempty"`
		Comments []apiComment `json:"comments,omitempty"`
	}{CommitID: pr.HeadSHA, Event: event, Body: body}
	for _, comment := range comments {
		if comment.HeadSHA != pr.HeadSHA {
			return errors.New("draft comment targets an outdated head; discard or recreate it")
		}
		payload.Comments = append(payload.Comments, apiComment{
			Path: comment.Path, Line: comment.Line, Side: comment.Side,
			StartLine: comment.StartLine, StartSide: comment.StartSide, Body: comment.Body,
		})
	}
	path := fmt.Sprintf("/repos/%s/%s/pulls/%d/reviews", url.PathEscape(pr.Owner), url.PathEscape(pr.Repo), pr.Number)
	return c.request(ctx, http.MethodPost, path, payload, nil)
}

func (c *Client) get(ctx context.Context, path string, out any) error {
	return c.request(ctx, http.MethodGet, path, nil, out)
}

func (c *Client) request(ctx context.Context, method, path string, body, out any) error {
	var encoded []byte
	var err error
	if body != nil {
		encoded, err = json.Marshal(body)
		if err != nil {
			return err
		}
	}
	attempts := 1
	if method == http.MethodGet {
		attempts = 3
	}
	for attempt := 0; attempt < attempts; attempt++ {
		request, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, bytes.NewReader(encoded))
		if err != nil {
			return err
		}
		request.Header.Set("Accept", "application/vnd.github+json")
		request.Header.Set("X-GitHub-Api-Version", "2022-11-28")
		request.Header.Set("User-Agent", "pr-review-tui")
		if c.token != "" {
			request.Header.Set("Authorization", "Bearer "+c.token)
		}
		if body != nil {
			request.Header.Set("Content-Type", "application/json")
		}
		response, err := c.httpClient.Do(request)
		if err != nil {
			if attempt+1 < attempts && wait(ctx, time.Duration(1<<attempt)*200*time.Millisecond) {
				continue
			}
			return fmt.Errorf("GitHub request: %w", err)
		}
		data, readErr := io.ReadAll(io.LimitReader(response.Body, 8<<20))
		response.Body.Close()
		if readErr != nil {
			return readErr
		}
		if response.StatusCode >= 200 && response.StatusCode < 300 {
			if out != nil && len(data) > 0 {
				if err := json.Unmarshal(data, out); err != nil {
					return fmt.Errorf("decode GitHub response: %w", err)
				}
			}
			return nil
		}
		if attempt+1 < attempts && (response.StatusCode == 429 || response.StatusCode >= 500) {
			delay := time.Duration(1<<attempt) * 300 * time.Millisecond
			if seconds, parseErr := strconv.Atoi(response.Header.Get("Retry-After")); parseErr == nil {
				delay = time.Duration(seconds) * time.Second
			}
			if wait(ctx, delay) {
				continue
			}
		}
		var apiError struct {
			Message string `json:"message"`
		}
		_ = json.Unmarshal(data, &apiError)
		if apiError.Message == "" {
			apiError.Message = strings.TrimSpace(string(data))
		}
		return fmt.Errorf("GitHub API %s %s: %s", response.Status, path, apiError.Message)
	}
	return errors.New("GitHub request failed after retries")
}

func wait(ctx context.Context, delay time.Duration) bool {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}
