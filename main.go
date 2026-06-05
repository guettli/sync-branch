package main

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	forge "github.com/git-pkgs/forge"
	"github.com/spf13/cobra"
)

func main() {
	rootCmd := &cobra.Command{
		Use:   "pre-commit-branch-up-to-date",
		Short: "Keep your local branch in sync with origin",
		Long: `pre-commit-branch-up-to-date is a pre-commit hook that fetches origin
and merges any new commits from origin/<branch> and the repository's base
branch (e.g. main) into your local branch before every commit.`,
	}

	checkCmd := &cobra.Command{
		Use:   "check",
		Short: "Fetch origin and merge any new commits into the current branch",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			return run(ctx)
		},
		SilenceUsage: true,
	}

	rootCmd.AddCommand(checkCmd)

	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

func run(ctx context.Context) error {
	branch, err := gitOutput("rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		return fmt.Errorf("get current branch: %w", err)
	}
	if branch == "HEAD" {
		return nil // detached HEAD, nothing to check
	}

	remoteURL, err := gitOutput("remote", "get-url", "origin")
	if err != nil {
		return nil // no origin remote configured
	}

	fmt.Printf("Fetching origin...\n")
	if err := gitRun("fetch", "--quiet", "origin"); err != nil {
		fmt.Fprintf(os.Stderr, "warning: git fetch origin failed: %v\n", err)
		// Continue with stale remote refs rather than blocking the commit.
	}

	// Step 1: integrate any new commits from origin/<branch>.
	originRef := "origin/" + branch
	behind, err := countBehind("HEAD", originRef)
	if err == nil {
		if behind > 0 {
			fmt.Printf("Merging %d commit(s) from %s into %s\n", behind, originRef, branch)
			if err := gitRun("merge", "--ff-only", originRef); err != nil {
				// Fast-forward failed; fall back to a regular merge.
				if err := gitRun("merge", originRef); err != nil {
					return fmt.Errorf("merge %s failed: %w\n  Resolve conflicts, then retry the commit", originRef, err)
				}
			}
		} else {
			fmt.Printf("%s is up to date with %s\n", branch, originRef)
		}
	}

	// Step 2: detect the repository's base branch and integrate its new commits.
	fmt.Printf("Detecting base branch...\n")
	baseBranch, err := detectBaseBranch(ctx, remoteURL)
	if err != nil {
		fmt.Fprintf(os.Stderr, "warning: cannot detect base branch: %v\n", err)
		return nil
	}
	fmt.Printf("Base branch: %s\n", baseBranch)

	if branch == baseBranch {
		fmt.Printf("Already on base branch, skipping base-branch merge\n")
		return nil
	}

	baseRef := "origin/" + baseBranch
	behind, err = countBehind("HEAD", baseRef)
	if err != nil {
		return nil // baseRef may not exist locally yet
	}
	if behind > 0 {
		fmt.Printf("Merging %d commit(s) from %s into %s\n", behind, baseRef, branch)
		if err := gitRun("merge", baseRef); err != nil {
			return fmt.Errorf("merge %s failed: %w\n  Resolve conflicts, then retry the commit", baseRef, err)
		}
	} else {
		fmt.Printf("%s is up to date with %s\n", branch, baseRef)
	}

	return nil
}

// countBehind returns how many commits b has that a does not.
func countBehind(a, b string) (int, error) {
	out, err := gitOutput("rev-list", "--count", a+".."+b)
	if err != nil {
		return 0, err
	}
	n, err := strconv.Atoi(out)
	if err != nil {
		return 0, fmt.Errorf("unexpected rev-list output %q: %w", out, err)
	}
	return n, nil
}

func gitOutput(args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

func gitRun(args ...string) error {
	cmd := exec.Command("git", args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// parseRemoteURL extracts domain, owner, and repo name from a git remote URL.
// Supports HTTPS (https://github.com/owner/repo.git) and
// SCP-style SSH (git@github.com:owner/repo.git) formats.
func parseRemoteURL(rawURL string) (domain, owner, repo string, err error) {
	rawURL = strings.TrimSpace(rawURL)

	if strings.HasPrefix(rawURL, "git@") {
		// SCP-style SSH: git@host:owner/repo.git
		rest := strings.TrimPrefix(rawURL, "git@")
		parts := strings.SplitN(rest, ":", 2)
		if len(parts) != 2 {
			return "", "", "", fmt.Errorf("malformed SSH URL")
		}
		domain = parts[0]
		path := strings.TrimSuffix(parts[1], ".git")
		segs := strings.SplitN(path, "/", 2)
		if len(segs) != 2 {
			return "", "", "", fmt.Errorf("malformed SSH URL path %q", parts[1])
		}
		return domain, segs[0], segs[1], nil
	}

	u, err := url.Parse(rawURL)
	if err != nil {
		return "", "", "", fmt.Errorf("invalid URL: %w", err)
	}
	domain = u.Hostname()
	path := strings.TrimPrefix(strings.TrimSuffix(u.Path, ".git"), "/")
	segs := strings.SplitN(path, "/", 2)
	if len(segs) != 2 {
		return "", "", "", fmt.Errorf("cannot find owner/repo in URL path %q", u.Path)
	}
	return domain, segs[0], segs[1], nil
}

// detectBaseBranch returns the repository's default branch.
// It first checks the local git ref refs/remotes/origin/HEAD (set at clone time,
// requires no network or credentials), then falls back to querying the forge API.
func detectBaseBranch(ctx context.Context, remoteURL string) (string, error) {
	// Fast path: git symbolic-ref refs/remotes/origin/HEAD → refs/remotes/origin/main
	if ref, err := gitOutput("symbolic-ref", "refs/remotes/origin/HEAD"); err == nil {
		return strings.TrimPrefix(ref, "refs/remotes/origin/"), nil
	}

	// Slow path: ask the forge API.
	domain, owner, repo, err := parseRemoteURL(remoteURL)
	if err != nil {
		return "", fmt.Errorf("parse remote URL %q: %w", remoteURL, err)
	}
	return getDefaultBranch(ctx, domain, owner, repo)
}

// getDefaultBranch contacts the forge at domain and returns the repository's
// default branch. Tokens are read automatically from environment variables
// (GITHUB_TOKEN, GH_TOKEN, GITLAB_TOKEN, etc.) and from ~/.config/forge/config.
func getDefaultBranch(ctx context.Context, domain, owner, repoName string) (string, error) {
	client := forge.NewClient()
	f, err := client.ForgeFor(domain)
	if err != nil {
		return "", fmt.Errorf("forge for %s: %w", domain, err)
	}
	r, err := f.Repos().Get(ctx, owner, repoName)
	if err != nil {
		return "", fmt.Errorf("get repository %s/%s: %w", owner, repoName, err)
	}
	if r.DefaultBranch == "" {
		return "", fmt.Errorf("repository %s/%s has empty default branch", owner, repoName)
	}

	return r.DefaultBranch, nil
}
