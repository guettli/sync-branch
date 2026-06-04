# pre-commit-branch-up-to-date

A [pre-commit](https://pre-commit.com) hook that keeps your local branch in sync
before every commit — no more "forgot to pull" surprises.

## What it does

Each time you run `git commit`, the hook:

1. **Fetches** `origin` to refresh remote-tracking refs.
2. **Merges** any new commits from `origin/<your-branch>` into your local branch
   (fast-forward when possible, regular merge otherwise).
3. **Detects the base branch** (e.g. `main`) by calling the forge API
   (GitHub, GitLab, or Forgejo — auto-detected from the remote URL via [git-pkgs/forge](https://github.com/git-pkgs/forge)).
4. **Merges** any new commits from `origin/<base-branch>` into your local branch
   so you stay on top of upstream changes.

If a merge produces conflicts, the commit is aborted and you are asked to resolve
them first.

## Installation

Add the hook to your `.pre-commit-config.yaml`:

```yaml
repos:
  - repo: https://github.com/guettli/pre-commit-branch-up-to-date
    rev: v0.0.1
    hooks:
      - id: branch-up-to-date
```

Then install:

```sh
pre-commit install
```

The hook is written in Go. pre-commit will build it automatically — no manual
`go install` needed.

## Authentication

For **public** repositories no token is required.

For **private** repositories set one of the standard environment variables
before running `git commit`:

| Forge   | Environment variable          |
|---------|-------------------------------|
| GitHub  | `GITHUB_TOKEN` or `GH_TOKEN`  |
| GitLab  | `GITLAB_TOKEN`                |
| Forgejo | `FORGEJO_TOKEN`               |
| Gitea   | `GITEA_TOKEN`                 |

You can also store tokens in `~/.config/forge/config` ([docs](https://github.com/git-pkgs/forge#authentication)):

```ini
[github.com]
token = ghp_…

[gitlab.com]
token = glpat-…

[forgejo.example.com]
type = forgejo
token = …
```

## Behaviour details

| Situation | Action |
|-----------|--------|
| Detached HEAD | Skip (nothing to merge) |
| No `origin` remote | Skip |
| `git fetch` fails (network issue) | Warn and continue with stale refs |
| Forge API unreachable / no token | Warn and skip base-branch check |
| Already on the base branch | Skip base-branch merge |
| Merge conflict | Abort commit; ask user to resolve |

## Development

```sh
git clone https://github.com/guettli/pre-commit-branch-up-to-date
cd pre-commit-branch-up-to-date
go build ./...
```

## License

MIT
