package target

import (
	"errors"
	"testing"
)

func TestParseAcceptedForms(t *testing.T) {
	tests := []struct {
		name  string
		value string
		want  Target
	}{
		{"short", "acme/widgets#42", Target{"acme", "widgets", 42}},
		{"github URL", "https://github.com/acme/widgets/pull/42/files", Target{"acme", "widgets", 42}},
		{"enterprise URL", "https://git.example.test/acme/widgets/pulls/42", Target{"acme", "widgets", 42}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := Parse(test.value, nil)
			if err != nil {
				t.Fatal(err)
			}
			if got != test.want {
				t.Fatalf("got %#v, want %#v", got, test.want)
			}
		})
	}
}

func TestParseBareNumberUsesRemote(t *testing.T) {
	got, err := Parse("17", func() (string, error) {
		return "git@github.com:octo/repo.git", nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if got != (Target{"octo", "repo", 17}) {
		t.Fatalf("got %#v", got)
	}
}

func TestParseRejectsInvalidTargets(t *testing.T) {
	if _, err := Parse("17", func() (string, error) { return "", errors.New("no git") }); err == nil {
		t.Fatal("expected remote error")
	}
	if _, err := Parse("not a target", nil); err == nil {
		t.Fatal("expected validation error")
	}
}

func TestParseRemote(t *testing.T) {
	for _, remote := range []string{
		"https://github.com/acme/widgets.git",
		"ssh://git@github.com/acme/widgets.git",
		"git@github.com:acme/widgets.git",
	} {
		owner, repo, err := ParseRemote(remote)
		if err != nil || owner != "acme" || repo != "widgets" {
			t.Fatalf("%q => %q/%q, %v", remote, owner, repo, err)
		}
	}
}
