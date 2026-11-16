package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/rineharc/pr-tool-openai/internal/githubclient"
	"github.com/rineharc/pr-tool-openai/internal/store"
	"github.com/rineharc/pr-tool-openai/internal/target"
	"github.com/rineharc/pr-tool-openai/internal/tui"
)

var version = "dev"

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "prr:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	flags := flag.NewFlagSet("prr", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	apiURL := flags.String("api-url", envOr("PRR_GITHUB_API_URL", "https://api.github.com"), "GitHub API base URL")
	statePath := flags.String("state", "", "state file path (default: user config directory)")
	showVersion := flags.Bool("version", false, "print version")
	noAltScreen := flags.Bool("no-alt-screen", false, "run without the terminal alternate screen")
	flags.Usage = func() {
		fmt.Fprintln(flags.Output(), "Usage: prr [flags] <PR URL | owner/repo#number | number>")
		flags.PrintDefaults()
	}
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *showVersion {
		fmt.Fprintln(os.Stdout, version)
		return nil
	}
	if flags.NArg() != 1 {
		flags.Usage()
		return errors.New("exactly one pull request target is required")
	}
	prTarget, err := target.Parse(flags.Arg(0), target.GitOrigin)
	if err != nil {
		return err
	}
	token := strings.TrimSpace(os.Getenv("GITHUB_TOKEN"))
	if token == "" {
		token = ghToken()
	}
	client, err := githubclient.New(*apiURL, token, nil)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	fmt.Fprintf(os.Stderr, "Loading %s/%s#%d…\n", prTarget.Owner, prTarget.Repo, prTarget.Number)
	pr, err := client.LoadPullRequest(ctx, prTarget.Owner, prTarget.Repo, prTarget.Number)
	if err != nil {
		return err
	}
	stateStore, err := store.Open(*statePath)
	if err != nil {
		return err
	}
	model := tui.New(pr, client, stateStore)
	options := []tea.ProgramOption{tea.WithMouseCellMotion()}
	if !*noAltScreen {
		options = append(options, tea.WithAltScreen())
	}
	_, err = tea.NewProgram(model, options...).Run()
	return err
}

func ghToken() string {
	command := exec.Command("gh", "auth", "token")
	output, err := command.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(output))
}

func envOr(name, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(name)); value != "" {
		return value
	}
	return fallback
}
