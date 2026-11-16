# prr

`prr` is a keyboard-first GitHub pull-request review TUI. It keeps changed files, unified diffs, and line-anchored conversations together, then submits locally queued comments as one explicit GitHub review.

## Current MVP

- Opens GitHub.com or GitHub Enterprise PRs by URL, `owner/repo#number`, or a bare number in a Git checkout.
- Fetches real PR metadata, paginated files, line comments, mergeability, and check runs.
- Provides adaptive wide, standard, and compact terminal layouts.
- Navigates files, lines, changed lines, and existing inline conversations.
- Persists viewed files, the last open file, and inline comment drafts in an atomic local JSON file.
- Submits queued line comments with a `COMMENT`, `APPROVE`, or `REQUEST_CHANGES` review after a separate confirmation step.
- Retries transient read failures and rejects drafts created against an outdated head SHA.
- Ships as a CGO-free Go binary and cross-compiles for macOS, Windows, and Linux.

No mutation is performed while browsing or saving a draft. A review reaches GitHub only after the submit summary and `y` confirmation.

## Build and run

Go 1.24.2 or newer is required.

```sh
go build -o prr ./cmd/prr
./prr https://github.com/OWNER/REPOSITORY/pull/123
```

Authentication is resolved in this order:

1. `GITHUB_TOKEN`
2. `gh auth token`, when GitHub CLI is installed and signed in
3. unauthenticated access (public read-only requests only)

Tokens are never written to the local state file. The token needs read access to load a PR and pull-request write access to submit reviews.

Other accepted targets:

```sh
prr OWNER/REPOSITORY#123
prr 123                       # resolves owner/repository from remote.origin.url
```

GitHub Enterprise:

```sh
PRR_GITHUB_API_URL=https://github.example.com/api/v3 \
GITHUB_TOKEN=... \
prr https://github.example.com/OWNER/REPOSITORY/pull/123
```

### Keyboard controls

| Key | Action |
|---|---|
| `j` / `k`, arrows | Move through the diff |
| `[` / `]` | Previous / next file |
| `n` / `p` | Next / previous changed line |
| `N` | Next existing inline conversation |
| `c` | Compose a local inline draft; `Ctrl+S` saves it |
| `v` | Toggle local viewed state |
| `f` / `T` | Toggle file / conversation panes |
| `s` | Open review summary |
| `Tab` / `Shift+Tab` | Cycle comment / approve / request-changes action |
| `?` | Help |
| `q` | Quit |

The default state file is in the platform user-config directory under `prr/state.json`. Override it with `--state`. Set `PRR_THEME=light`, `PRR_THEME=dark`, or `PRR_THEME=monochrome`; `NO_COLOR` also selects monochrome.

## Verify

```sh
go test ./...
go vet ./...
make cross-build
```

`make cross-build` writes one executable per supported OS/architecture to `dist/`.

## Approved MVP items deferred

The vertical slice does not claim the following design items yet:

- **OS keyring authentication:** environment and optional `gh` credential bridging are complete. Native keyring integration is deferred because it requires platform-specific credential lifecycle and packaging work.
- **Reply and resolve/unresolve existing threads:** existing conversations are readable and navigable, but GitHub thread resolution requires GraphQL thread-node discovery and mutations. The REST-only mutation path currently submits new review comments.
- **Multi-line draft selection:** comments are anchored to one selected line. A range-selection interaction and cross-hunk validation are still needed.
- **Large/truncated patch reconstruction:** files with no REST patch clearly show an unavailable-patch message. Rebuilding from base/head blobs is deferred until exact rename, binary, and GitHub anchor-parity fixtures are in place.
- **ETag/offline cache:** atomic draft/viewed-state persistence and bounded request retry are implemented. Response-body caching and reconnect refresh UI are deferred to avoid serving stale review heads.
- **Signed release artifacts:** deterministic CGO-free cross-builds are provided. Apple notarization, Windows Authenticode, release checksums, and an SBOM require repository release credentials and policy decisions.
- **Syntax-aware diff coloring:** additions, deletions, hunk headers, and selection are accessible in dark, light, and monochrome modes. Language-specific tokenization is deferred to keep the first binary small.

Split diffs, inbox/notifications, suggested changes, plugins, offline search, Azure DevOps provider support, and self-update remain later-scope features as described in the architecture report.