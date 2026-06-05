package main

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	forge "github.com/git-pkgs/forge"
	gitea "github.com/git-pkgs/forge/gitea"
	ghforge "github.com/git-pkgs/forge/github"
	glforge "github.com/git-pkgs/forge/gitlab"
	"github.com/spf13/cobra"
)

func main() {
	rootCmd := &cobra.Command{
		Use:   "sync-branch",
		Short: "Keep your local branch in sync with origin",
		Long: `sync-branch is a pre-commit hook that fetches origin
and merges any new commits from origin/<branch> and the repository's base
branch (e.g. main) into your local branch before every commit.`,
	}

	checkCmd := &cobra.Command{
		Use:   "sync",
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

	remoteName := "origin"
	if r, err := gitOutput("config", "branch."+branch+".remote"); err == nil && r != "" {
		remoteName = r
	}

	remoteURL, err := gitOutput("remote", "get-url", remoteName)
	if err != nil {
		return nil // no remote configured
	}

	fmt.Printf("Fetching %s...\n", remoteName)
	// Fetch to update remote-tracking refs.
	if err := gitRun("fetch", "--quiet", remoteName); err != nil {
		fmt.Fprintf(os.Stderr, "warning: git fetch %s failed: %v\n", remoteName, err)
		// Continue with stale remote refs rather than blocking the commit.
	}

	// Step 1: integrate any new commits from origin/<branch>.
	originRef := remoteName + "/" + branch
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
	baseBranch, baseBranchReason, err := detectBaseBranch(ctx, branch, remoteURL, remoteName)
	if err != nil {
		fmt.Fprintf(os.Stderr, "warning: cannot detect base branch: %v\n", err)
		return nil
	}
	fmt.Printf("Base branch: %s (%s)\n", baseBranch, baseBranchReason)

	localBaseBranch := baseBranch
	if strings.HasPrefix(localBaseBranch, remoteName+"/") {
		localBaseBranch = strings.TrimPrefix(localBaseBranch, remoteName+"/")
	}
	if branch == localBaseBranch {
		fmt.Printf("Already on base branch, skipping base-branch merge\n")
		return nil
	}

	baseRef := baseBranch
	if !strings.HasPrefix(baseRef, remoteName+"/") {
		baseRef = remoteName + "/" + baseRef
	}
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
// It first checks the local git config branch.<branch>.vscode-merge-base.
// If not set, it queries the forge API, sets the config, and returns it.
func detectBaseBranch(ctx context.Context, branch, remoteURL, remoteName string) (baseBranch, reason string, err error) {
	// Check if vscode-merge-base is set in git config for this branch.
	if mergeBase, err := gitOutput("config", "branch."+branch+".vscode-merge-base"); err == nil && mergeBase != "" {
		fmt.Printf("Value taken from branch.%s.vscode-merge-base: %s\n", branch, mergeBase)
		return mergeBase, fmt.Sprintf("taken from branch.%s.vscode-merge-base", branch), nil
	}

	// Slow path: ask the forge API.
	domain, owner, repo, err := parseRemoteURL(remoteURL)
	if err != nil {
		return "", "", fmt.Errorf("parse remote URL %q: %w", remoteURL, err)
	}
	defaultBranch, err := getDefaultBranch(ctx, domain, owner, repo)
	if err != nil {
		return "", "", err
	}

	// When vscode-merge-base does not exist yet, then add it after getting it from forge.
	// Be sure it contains upstream as prefix (in most cases "origin").
	mergeBase := remoteName + "/" + defaultBranch
	if _, err := gitOutput("config", "branch."+branch+".vscode-merge-base", mergeBase); err != nil {
		fmt.Fprintf(os.Stderr, "warning: failed to set branch.%s.vscode-merge-base: %v\n", branch, err)
	}

	return mergeBase, fmt.Sprintf("default branch on %s", domain), nil
}

// getDefaultBranch contacts the forge at domain and returns the repository's
// default branch. Tokens are read automatically from environment variables
// (GITHUB_TOKEN, GH_TOKEN, GITLAB_TOKEN, etc.) and from ~/.config/forge/config.
func getDefaultBranch(ctx context.Context, domain, owner, repoName string) (string, error) {
	client := forge.NewClient()

	token := getToken(domain)
	builders := forge.ForgeBuilders{
		GitHub: func(baseURL, token string, hc *http.Client) forge.Forge {
			return ghforge.New(token, hc)
		},
		GitLab: func(baseURL, token string, hc *http.Client) forge.Forge {
			return glforge.New(baseURL, token, hc)
		},
		Gitea: func(baseURL, token string, hc *http.Client) forge.Forge {
			return gitea.New(baseURL, token, hc)
		},
	}

	if err := client.RegisterDomain(ctx, domain, token, builders); err != nil {
		return "", fmt.Errorf("register domain %s: %w", domain, err)
	}

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

func getToken(domain string) string {
	// 1. Env variables
	switch domain {
	case "github.com":
		if t := os.Getenv("GITHUB_TOKEN"); t != "" {
			return t
		}
		if t := os.Getenv("GH_TOKEN"); t != "" {
			return t
		}
	case "gitlab.com":
		if t := os.Getenv("GITLAB_TOKEN"); t != "" {
			return t
		}
	}
	// General fallbacks
	if t := os.Getenv("GITHUB_TOKEN"); t != "" {
		return t
	}
	if t := os.Getenv("GH_TOKEN"); t != "" {
		return t
	}
	if t := os.Getenv("GITLAB_TOKEN"); t != "" {
		return t
	}
	if t := os.Getenv("FORGEJO_TOKEN"); t != "" {
		return t
	}
	if t := os.Getenv("GITEA_TOKEN"); t != "" {
		return t
	}

	// 2. Read ~/.config/forge/config
	home, err := os.UserHomeDir()
	if err == nil {
		configPath := home + "/.config/forge/config"
		if data, err := os.ReadFile(configPath); err == nil {
			// Parse simple ini format
			lines := strings.Split(string(data), "\n")
			currentSection := ""
			for _, line := range lines {
				line = strings.TrimSpace(line)
				if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
					currentSection = strings.TrimSpace(line[1 : len(line)-1])
				} else if currentSection == domain {
					parts := strings.SplitN(line, "=", 2)
					if len(parts) == 2 && strings.TrimSpace(parts[0]) == "token" {
						return strings.TrimSpace(parts[1])
					}
				}
			}
		}
	}

	// 3. Fallback to gh CLI for github.com if installed
	if domain == "github.com" {
		if cmdPath, err := exec.LookPath("gh"); err == nil {
			cmd := exec.Command(cmdPath, "auth", "token")
			if out, err := cmd.Output(); err == nil {
				token := strings.TrimSpace(string(out))
				if token != "" {
					return token
				}
			}
		}
	}

	return ""
}
