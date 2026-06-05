package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"
)

const prefix = "prefix-integration-test-"

var binaryPath string

func TestMain(m *testing.M) {
	// Save current branch to restore later
	currentBranch, _ := gitOutput("rev-parse", "--abbrev-ref", "HEAD")

	// Track all changes so they are committed
	exec.Command("git", "add", "main.go", "go.mod", "go.sum", "main_test.go").Run()

	hasChanges := false
	cmdStatus := exec.Command("git", "status", "--porcelain")
	if out, err := cmdStatus.Output(); err == nil && len(strings.TrimSpace(string(out))) > 0 {
		hasChanges = true
		exec.Command("git", "commit", "-m", "temporary integration test commit").Run()
	}

	code := m.Run()

	if hasChanges {
		exec.Command("git", "reset", "HEAD~1").Run()
	}
	exec.Command("git", "checkout", currentBranch).Run()

	if binaryPath != "" {
		os.Remove(binaryPath)
	}
	os.Exit(code)
}

func getBinaryPath(t *testing.T) string {
	if binaryPath != "" {
		return binaryPath
	}
	binaryPath = "./pre-commit-branch-up-to-date-test-bin"
	buildCmd := exec.Command("go", "build", "-o", binaryPath, "main.go")
	buildCmd.Stdout = os.Stdout
	buildCmd.Stderr = os.Stderr
	if err := buildCmd.Run(); err != nil {
		t.Fatalf("failed to build binary: %v", err)
	}
	return binaryPath
}

func runHook(t *testing.T, bin string) (string, error) {
	cmd := exec.Command(bin, "sync")
	out, err := cmd.CombinedOutput()
	outputStr := string(out)

	// Clean up any conflict markers/merges left by the run
	exec.Command("git", "merge", "--abort").Run()

	return outputStr, err
}

func TestIntegration(t *testing.T) {
	// Save current branch to restore later
	currentBranch, err := gitOutput("rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		t.Fatalf("failed to get current branch: %v", err)
	}

	// Defer cleanup and restore
	defer func() {
		// Restore original branch
		exec.Command("git", "checkout", currentBranch).Run()
		os.Remove("temp-base.txt")
		cleanup(t)
	}()

	// Initial cleanup of any previous run leftovers
	cleanup(t)

	binaryPath := getBinaryPath(t)
	baseBranch := prefix + "base"
	featureBranch := prefix + "feature"

	// 1. Create base branch and push it to origin
	if err := runGit("checkout", "-b", baseBranch); err != nil {
		t.Fatalf("failed to create base branch: %v", err)
	}
	// Create an initial commit on base branch
	if err := os.WriteFile("temp-base.txt", []byte("base initial"), 0644); err != nil {
		t.Fatalf("failed to write temp file: %v", err)
	}
	if err := runGit("add", "temp-base.txt"); err != nil {
		t.Fatalf("failed to git add: %v", err)
	}
	if err := runGit("commit", "-m", "initial base commit"); err != nil {
		t.Fatalf("failed to git commit: %v", err)
	}
	if err := runGit("push", "origin", baseBranch); err != nil {
		t.Fatalf("failed to push base branch: %v", err)
	}

	// 2. Create feature branch off base branch
	if err := runGit("checkout", "-b", featureBranch); err != nil {
		t.Fatalf("failed to create feature branch: %v", err)
	}
	// Push feature branch so origin/feature exists
	if err := runGit("push", "origin", featureBranch); err != nil {
		t.Fatalf("failed to push feature branch: %v", err)
	}

	// 3. Go back to base branch, make a new commit, and push it
	if err := runGit("checkout", baseBranch); err != nil {
		t.Fatalf("failed to checkout base branch: %v", err)
	}
	if err := os.WriteFile("temp-base.txt", []byte("base updated"), 0644); err != nil {
		t.Fatalf("failed to update temp file: %v", err)
	}
	if err := runGit("add", "temp-base.txt"); err != nil {
		t.Fatalf("failed to git add: %v", err)
	}
	if err := runGit("commit", "-m", "update base commit"); err != nil {
		t.Fatalf("failed to git commit: %v", err)
	}
	if err := runGit("push", "origin", baseBranch); err != nil {
		t.Fatalf("failed to push updated base branch: %v", err)
	}

	// 4. Switch back to feature branch
	if err := runGit("checkout", featureBranch); err != nil {
		t.Fatalf("failed to checkout feature branch: %v", err)
	}

	// Test Case 1: vscode-merge-base is set in git config for feature branch.
	configKey := "branch." + featureBranch + ".vscode-merge-base"
	if err := runGit("config", configKey, "origin/"+baseBranch); err != nil {
		t.Fatalf("failed to set git config: %v", err)
	}

	// Run the hook binary
	outputStr, err := runHook(t, binaryPath)
	if err != nil && !strings.Contains(outputStr, "merge") {
		t.Fatalf("hook failed to run: %v\nOutput: %s", err, outputStr)
	}

	// Verify that the output mentions taking the value from config
	expectedMsg := fmt.Sprintf("Value taken from branch.%s.vscode-merge-base: origin/%s", featureBranch, baseBranch)
	if !strings.Contains(outputStr, expectedMsg) {
		t.Errorf("expected output to contain %q, but got %q", expectedMsg, outputStr)
	}

	// Verify that the commit from base branch was merged
	logCmd := exec.Command("git", "log", "-n", "5", "--oneline")
	logOut, _ := logCmd.Output()
	if !strings.Contains(string(logOut), "update base commit") {
		t.Errorf("expected commit 'update base commit' to be merged, but git log showed: %s", logOut)
	}

	// Test Case 2: vscode-merge-base is NOT set.
	if err := runGit("config", "--unset", configKey); err != nil {
		t.Fatalf("failed to unset git config: %v", err)
	}

	// Run the hook binary again. Since vscode-merge-base is not set, it should query the forge
	// for the default branch (which is main in this repo) and write origin/main to config.
	outputStr2, _ := runHook(t, binaryPath)
	t.Logf("Hook run 2 output: %s", outputStr2)

	// Check that vscode-merge-base is now set to origin/main (or the repo's default branch)
	val, err := gitOutput("config", configKey)
	if err != nil {
		t.Errorf("expected %s to be set, but got error: %v", configKey, err)
	}
	expectedDefault := "origin/main"
	if val != expectedDefault {
		t.Errorf("expected config value %q, but got %q", expectedDefault, val)
	}
}

func TestDetachedHEAD(t *testing.T) {
	currentBranch, err := gitOutput("rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		t.Fatalf("failed to get current branch: %v", err)
	}
	defer exec.Command("git", "checkout", currentBranch).Run()

	// Detach HEAD on the current commit
	if err := runGit("checkout", "--detach"); err != nil {
		t.Fatalf("failed to detach HEAD: %v", err)
	}

	bin := getBinaryPath(t)
	outputStr, err := runHook(t, bin)
	if err != nil {
		t.Fatalf("expected hook to succeed on detached HEAD, but failed: %v\nOutput: %s", err, outputStr)
	}
}

func TestNoRemote(t *testing.T) {
	currentBranch, err := gitOutput("rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		t.Fatalf("failed to get current branch: %v", err)
	}
	tempBranch := prefix + "no-remote"
	defer func() {
		exec.Command("git", "checkout", currentBranch).Run()
		exec.Command("git", "branch", "-D", tempBranch).Run()
	}()

	// Create a new branch, but do not push it or set upstream
	if err := runGit("checkout", "-b", tempBranch); err != nil {
		t.Fatalf("failed to create branch: %v", err)
	}

	bin := getBinaryPath(t)
	outputStr, err := runHook(t, bin)
	// If it fails with merge conflict/exit code, that is acceptable since we have local changes on main
	if err != nil && !strings.Contains(outputStr, "merge") && !strings.Contains(outputStr, "conflict") {
		t.Fatalf("expected hook to succeed or fail with merge conflict, but got: %v\nOutput: %s", err, outputStr)
	}
}

func TestStep1Merge(t *testing.T) {
	currentBranch, err := gitOutput("rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		t.Fatalf("failed to get current branch: %v", err)
	}
	tempBranch := prefix + "step1"
	defer func() {
		exec.Command("git", "checkout", currentBranch).Run()
		exec.Command("git", "branch", "-D", tempBranch).Run()
		exec.Command("git", "push", "origin", "--delete", tempBranch).Run()
	}()

	// 1. Create branch and push to origin
	if err := runGit("checkout", "-b", tempBranch); err != nil {
		t.Fatalf("failed to create branch: %v", err)
	}
	if err := runGit("push", "origin", tempBranch); err != nil {
		t.Fatalf("failed to push branch: %v", err)
	}

	// 2. Make a commit on origin/tempBranch
	tempFile := "temp-step1.txt"
	if err := os.WriteFile(tempFile, []byte("step1 content"), 0644); err != nil {
		t.Fatalf("failed to write temp file: %v", err)
	}
	defer os.Remove(tempFile)

	if err := runGit("add", tempFile); err != nil {
		t.Fatalf("failed to git add: %v", err)
	}
	if err := runGit("commit", "-m", "temp commit for step 1"); err != nil {
		t.Fatalf("failed to git commit: %v", err)
	}
	if err := runGit("push", "origin", tempBranch); err != nil {
		t.Fatalf("failed to git push: %v", err)
	}

	// Reset local branch by 1 commit so it is behind origin
	if err := runGit("reset", "--hard", "HEAD~1"); err != nil {
		t.Fatalf("failed to reset local branch: %v", err)
	}

	// 3. Run hook binary
	bin := getBinaryPath(t)
	outputStr, err := runHook(t, bin)
	if err != nil && !strings.Contains(outputStr, "merge") {
		t.Fatalf("hook failed to run: %v\nOutput: %s", err, outputStr)
	}

	// Verify that the output mentions merging commits from origin/<branch>
	expectedMsg := fmt.Sprintf("Merging 1 commit(s) from origin/%s into %s", tempBranch, tempBranch)
	if !strings.Contains(outputStr, expectedMsg) {
		t.Errorf("expected output to contain %q, but got %q", expectedMsg, outputStr)
	}
}

func TestAlreadyOnBaseBranch(t *testing.T) {
	currentBranch, err := gitOutput("rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		t.Fatalf("failed to get current branch: %v", err)
	}
	tempBranch := prefix + "already-base"
	defer func() {
		exec.Command("git", "checkout", currentBranch).Run()
		exec.Command("git", "branch", "-D", tempBranch).Run()
		exec.Command("git", "push", "origin", "--delete", tempBranch).Run()
	}()

	// Create branch and push to origin
	if err := runGit("checkout", "-b", tempBranch); err != nil {
		t.Fatalf("failed to create branch: %v", err)
	}
	if err := runGit("push", "origin", tempBranch); err != nil {
		t.Fatalf("failed to push branch: %v", err)
	}

	// Set vscode-merge-base to itself (or origin/itself)
	configKey := "branch." + tempBranch + ".vscode-merge-base"
	if err := runGit("config", configKey, "origin/"+tempBranch); err != nil {
		t.Fatalf("failed to set git config: %v", err)
	}

	bin := getBinaryPath(t)
	outputStr, err := runHook(t, bin)
	if err != nil && !strings.Contains(outputStr, "merge") {
		t.Fatalf("hook failed to run: %v\nOutput: %s", err, outputStr)
	}

	// Verify that the output does NOT try to merge itself or do any other merge operations
	if strings.Contains(outputStr, "Merging") {
		t.Errorf("expected no merge operations, but got output: %s", outputStr)
	}
}

func runGit(args ...string) error {
	cmd := exec.Command("git", args...)
	return cmd.Run()
}

func cleanup(t *testing.T) {
	// 1. Close PRs starting with the prefix
	cmd := exec.Command("gh", "pr", "list", "--state", "open", "--json", "number,headRefName")
	if out, err := cmd.Output(); err == nil {
		type PR struct {
			Number      int    `json:"number"`
			HeadRefName string `json:"headRefName"`
		}
		var prs []PR
		if err := json.Unmarshal(out, &prs); err == nil {
			for _, pr := range prs {
				if strings.HasPrefix(pr.HeadRefName, prefix) {
					t.Logf("Closing test PR #%d (%s)...", pr.Number, pr.HeadRefName)
					exec.Command("gh", "pr", "close", strconv.Itoa(pr.Number)).Run()
				}
			}
		}
	}

	// 2. Delete remote branches
	cmd = exec.Command("git", "branch", "-r")
	if out, err := cmd.Output(); err == nil {
		lines := strings.Split(string(out), "\n")
		for _, line := range lines {
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, "origin/"+prefix) {
				branchName := strings.TrimPrefix(line, "origin/")
				t.Logf("Deleting remote branch %s...", branchName)
				exec.Command("git", "push", "origin", "--delete", branchName).Run()
			}
		}
	}

	// 3. Delete local branches
	cmd = exec.Command("git", "branch")
	if out, err := cmd.Output(); err == nil {
		lines := strings.Split(string(out), "\n")
		for _, line := range lines {
			line = strings.TrimSpace(line)
			line = strings.TrimPrefix(line, "*")
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, prefix) {
				t.Logf("Deleting local branch %s...", line)
				exec.Command("git", "branch", "-D", line).Run()
			}
		}
	}
}
