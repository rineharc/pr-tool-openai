package target

import (
	"errors"
	"fmt"
	"net/url"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
)

type Target struct {
	Owner  string
	Repo   string
	Number int
}

var shortPattern = regexp.MustCompile(`^([^/\s]+)/([^#\s]+)#(\d+)$`)

func Parse(value string, remoteResolver func() (string, error)) (Target, error) {
	value = strings.TrimSpace(value)
	if match := shortPattern.FindStringSubmatch(value); match != nil {
		number, _ := strconv.Atoi(match[3])
		return Target{Owner: match[1], Repo: strings.TrimSuffix(match[2], ".git"), Number: number}, nil
	}
	if parsed, err := url.Parse(value); err == nil && parsed.Host != "" {
		parts := strings.Split(strings.Trim(parsed.Path, "/"), "/")
		for i := 0; i+3 < len(parts); i++ {
			if parts[i+2] == "pull" || parts[i+2] == "pulls" {
				number, err := strconv.Atoi(parts[i+3])
				if err == nil {
					return Target{Owner: parts[i], Repo: strings.TrimSuffix(parts[i+1], ".git"), Number: number}, nil
				}
			}
		}
	}
	if number, err := strconv.Atoi(value); err == nil && number > 0 {
		if remoteResolver == nil {
			return Target{}, errors.New("a bare PR number requires a git remote")
		}
		remote, err := remoteResolver()
		if err != nil {
			return Target{}, fmt.Errorf("resolve git remote: %w", err)
		}
		owner, repo, err := ParseRemote(remote)
		if err != nil {
			return Target{}, err
		}
		return Target{Owner: owner, Repo: repo, Number: number}, nil
	}
	return Target{}, fmt.Errorf("invalid PR target %q (use URL, owner/repo#number, or number)", value)
}

func GitOrigin() (string, error) {
	output, err := exec.Command("git", "config", "--get", "remote.origin.url").Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(output)), nil
}

func ParseRemote(remote string) (owner, repo string, err error) {
	remote = strings.TrimSpace(remote)
	if strings.HasPrefix(remote, "git@") {
		if colon := strings.IndexByte(remote, ':'); colon >= 0 {
			remote = remote[colon+1:]
		}
	} else if parsed, parseErr := url.Parse(remote); parseErr == nil && parsed.Path != "" {
		remote = parsed.Path
	}
	parts := strings.Split(strings.Trim(remote, "/"), "/")
	if len(parts) < 2 {
		return "", "", fmt.Errorf("cannot determine owner/repository from remote %q", remote)
	}
	return parts[len(parts)-2], strings.TrimSuffix(parts[len(parts)-1], ".git"), nil
}
