package review

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

type PullRequest struct {
	Owner      string
	Repo       string
	Number     int
	Title      string
	Author     string
	HeadSHA    string
	BaseRef    string
	HeadRef    string
	Mergeable  *bool
	MergeState string
	HTMLURL    string
	Files      []File
	Comments   []Comment
	Checks     CheckSummary
}

type File struct {
	Path      string
	Status    string
	Additions int
	Deletions int
	Patch     string
	Lines     []DiffLine
}

type DiffLine struct {
	Kind    LineKind
	Content string
	OldLine int
	NewLine int
}

type LineKind uint8

const (
	Context LineKind = iota
	Added
	Removed
	Header
)

func (l DiffLine) Anchor() (line int, side string, ok bool) {
	switch l.Kind {
	case Added, Context:
		if l.NewLine > 0 {
			return l.NewLine, "RIGHT", true
		}
	case Removed:
		if l.OldLine > 0 {
			return l.OldLine, "LEFT", true
		}
	}
	return 0, "", false
}

type Comment struct {
	ID          int64
	InReplyToID int64
	Path        string
	Line        int
	Side        string
	Body        string
	Author      string
	CreatedAt   time.Time
}

type DraftComment struct {
	Path      string `json:"path"`
	Line      int    `json:"line"`
	Side      string `json:"side"`
	StartLine int    `json:"startLine,omitempty"`
	StartSide string `json:"startSide,omitempty"`
	Body      string `json:"body"`
	HeadSHA   string `json:"headSha"`
}

type CheckSummary struct {
	Total      int
	Successful int
	Failed     int
	Pending    int
}

func ParsePatch(patch string) []DiffLine {
	if patch == "" {
		return nil
	}
	var result []DiffLine
	oldLine, newLine := 0, 0
	for _, raw := range strings.Split(strings.ReplaceAll(patch, "\r\n", "\n"), "\n") {
		if strings.HasPrefix(raw, "@@") {
			oldStart, newStart, ok := parseHunkHeader(raw)
			if ok {
				oldLine, newLine = oldStart, newStart
			}
			result = append(result, DiffLine{Kind: Header, Content: raw})
			continue
		}
		if strings.HasPrefix(raw, "\\ No newline") {
			result = append(result, DiffLine{Kind: Header, Content: raw})
			continue
		}
		if raw == "" && oldLine == 0 && newLine == 0 {
			continue
		}
		line := DiffLine{Content: raw}
		switch {
		case strings.HasPrefix(raw, "+"):
			line.Kind, line.NewLine = Added, newLine
			newLine++
		case strings.HasPrefix(raw, "-"):
			line.Kind, line.OldLine = Removed, oldLine
			oldLine++
		default:
			line.Kind, line.OldLine, line.NewLine = Context, oldLine, newLine
			oldLine++
			newLine++
		}
		result = append(result, line)
	}
	return result
}

func parseHunkHeader(header string) (oldStart, newStart int, ok bool) {
	fields := strings.Fields(header)
	if len(fields) < 3 {
		return 0, 0, false
	}
	parse := func(value string) (int, error) {
		value = strings.TrimLeft(value, "+-")
		if i := strings.IndexByte(value, ','); i >= 0 {
			value = value[:i]
		}
		return strconv.Atoi(value)
	}
	var err error
	if oldStart, err = parse(fields[1]); err != nil {
		return 0, 0, false
	}
	if newStart, err = parse(fields[2]); err != nil {
		return 0, 0, false
	}
	return oldStart, newStart, true
}

func (p PullRequest) Key() string {
	return fmt.Sprintf("%s/%s#%d", p.Owner, p.Repo, p.Number)
}
