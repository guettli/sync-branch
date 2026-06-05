# sync-branch

`sync-branch` is a tool to sync (update) the local branch by fetching `origin` and merging upstream changes. It can be run either as a standalone CLI tool or integrated as a [pre-commit](https://pre-commit.com) hook to prevent "forgot to pull" surprises.

## What it does

When run, `sync-branch`:

1. **Fetches** `origin` to refresh remote-tracking refs.
2. **Merges** any new commits from `origin/<your-branch>` into your local branch
   (fast-forward when possible, regular merge otherwise).
3. **Detects the base branch** (e.g. `main`) using the `vscode-merge-base` trick (see details below).
4. **Merges** any new commits from `origin/<base-branch>` into your local branch
   so you stay on top of upstream changes.

If a merge produces conflicts, the operation is aborted and you are asked to resolve
them first.

## The vscode-merge-base trick

To merge upstream base branch commits (Step 4), `sync-branch` needs to know what the base branch is (e.g. `origin/main` vs `origin/develop` vs a parent feature branch):

1. **Check local git config:** It looks for `branch.<your-branch>.vscode-merge-base` in your local git configuration.
2. **Forge API Fallback:** If not set, it queries the forge API (GitHub, GitLab, or Forgejo — auto-detected from the remote URL via [git-pkgs/forge](https://github.com/git-pkgs/forge)) to detect the repository's default branch.
3. **Auto-set config:** Once detected, it saves this base branch to `branch.<your-branch>.vscode-merge-base` in your local git config.

### Details:
* **Performance / Offline:** Subsequent runs on the same branch are instant and don't need network access or API tokens because the base branch is cached locally.
* **VS Code Integration:** VS Code and extensions (like GitLens) natively use `branch.<branch>.vscode-merge-base` to determine the comparison base branch for showing Incoming/Outgoing changes in the Source Control view. By setting this config value, `sync-branch` configures VS Code automatically.
* **Custom Base Branches:** If your branch is based on another feature branch instead of `main`, you can manually change this config value:
  ```sh
  git config branch.my-feature.vscode-merge-base origin/parent-feature
  ```
  `sync-branch` will then automatically pull and merge from `origin/parent-feature` instead.



## Standalone Usage

You can run the sync check directly as a standalone tool without any installation using `go run`:

```sh
go run github.com/guettli/sync-branch@latest sync
```

## Usage as pre-commit hook

Add the hook to your `.pre-commit-config.yaml`:

```yaml
repos:
  - repo: https://github.com/guettli/sync-branch
    rev: v0.0.8
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
| --------- | ------ |
| Detached HEAD | Skip (nothing to merge) |
| No `origin` remote | Skip |
| `git fetch` fails (network issue) | Warn and continue with stale refs |
| Forge API unreachable / no token | Warn and skip base-branch check |
| Already on the base branch | Skip base-branch merge |
| Merge conflict | Abort commit; ask user to resolve |

## Development

```sh
git clone https://github.com/guettli/sync-branch
cd sync-branch
go build ./...
```

Set up the local hooks:

```sh
cp scripts/pre-push-hook.sh .git/hooks/pre-push
pre-commit install
```

The pre-commit config in this repo uses two hooks:

- `branch-up-to-date` — the hook itself (dogfooding).
- `check-pre-push-hook` — verifies the pre-push hook is installed on every commit.

The pre-push hook (`scripts/pre-push-hook.sh`) runs only when pushing to `main`
and checks that the `rev:` in `.pre-commit-config.yaml` and `README.md` both
match the latest git tag.

### Releasing

```sh
./scripts/release.sh
git push && git push --tags
```

`release.sh` reads the latest semver tag from git, bumps the patch version,
updates `rev:` in `.pre-commit-config.yaml` and `README.md`, commits, and tags.

## License

MIT

...
