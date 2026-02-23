package pr

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/uesteibar/ralph/internal/autoralph/ai"
	"github.com/uesteibar/ralph/internal/autoralph/db"
	"github.com/uesteibar/ralph/internal/workspace"
)

// maxTurnsPR limits the number of agentic turns for PR description generation.
const maxTurnsPR = 10

// Invoker invokes an AI model with a prompt and returns the response.
// Dir sets the working directory for the AI process.
// MaxTurns limits the number of agentic turns; 0 means unlimited.
type Invoker interface {
	Invoke(ctx context.Context, prompt, dir string, maxTurns int) (string, error)
}

// GitPusher pushes a branch to the remote from a working directory.
type GitPusher interface {
	PushBranch(ctx context.Context, workDir, branch string) error
}

// DiffStatter returns git diff --stat output against a base ref.
type DiffStatter interface {
	DiffStats(ctx context.Context, workDir, base string) (string, error)
}

// PRDReader reads a PRD from a file path.
type PRDReader interface {
	ReadPRD(path string) (PRDInfo, error)
}

// PRDInfo holds the subset of PRD data needed for PR creation.
type PRDInfo struct {
	Description string
	Stories     []StoryInfo
}

// StoryInfo holds a story's ID and title.
type StoryInfo struct {
	ID    string
	Title string
}

// GitHubPRCreator creates a pull request on GitHub.
type GitHubPRCreator interface {
	CreatePullRequest(ctx context.Context, owner, repo, head, base, title, body string) (PRResult, error)
	FindOpenPR(ctx context.Context, owner, repo, head, base string) (*PRResult, error)
}

// Rebaser performs a rebase and reports conflicts.
type Rebaser interface {
	FetchBranch(ctx context.Context, workDir, branch string) error
	StartRebase(ctx context.Context, workDir, onto string) (RebaseResult, error)
	AbortRebase(ctx context.Context, workDir string) error
	ConflictFiles(ctx context.Context, workDir string) ([]string, error)
}

// RebaseResult describes the outcome of a rebase operation.
type RebaseResult struct {
	Success      bool
	HasConflicts bool
}

// ConflictError is returned when a push fails due to merge conflicts.
type ConflictError struct {
	Files []string
}

// PRResult holds the result of creating a GitHub PR.
type PRResult struct {
	Number  int
	HTMLURL string
}

// LinearPoster posts a comment on a Linear issue and returns the comment ID.
type LinearPoster interface {
	PostComment(ctx context.Context, linearIssueID, body string) (string, error)
}

// ProjectGetter fetches a project from the database.
type ProjectGetter interface {
	GetProject(id string) (db.Project, error)
}

// ConfigLoader loads the default base branch from a Ralph config.
type ConfigLoader interface {
	DefaultBase(projectLocalPath, ralphConfigPath string) (string, error)
}

func (e *ConflictError) Error() string {
	return fmt.Sprintf("merge conflicts in %d files: %s", len(e.Files), strings.Join(e.Files, ", "))
}

// GitHubPREditor updates an existing pull request's title and body.
type GitHubPREditor interface {
	EditPullRequest(ctx context.Context, owner, repo string, prNumber int, title, body string) error
}

// DescriptionInput holds the inputs needed to generate a PR description.
type DescriptionInput struct {
	TreePath    string
	DefaultBase string
	PRDPath     string
	Identifier  string
	OverrideDir string
}

// GenerateDescription generates a PR title and body by reading diff stats,
// reading the PRD, rendering an AI prompt, invoking AI, and parsing output.
func GenerateDescription(ctx context.Context, inv Invoker, diff DiffStatter, prd PRDReader, cfg ConfigLoader, input DescriptionInput) (title, body string, err error) {
	diffStats, diffErr := diff.DiffStats(ctx, input.TreePath, "origin/"+input.DefaultBase)
	if diffErr != nil {
		diffStats = "(diff stats unavailable)"
	} else {
		diffStats = capDiffStats(diffStats, 50)
	}

	prdInfo, err := prd.ReadPRD(input.PRDPath)
	if err != nil {
		return "", "", fmt.Errorf("reading PRD: %w", err)
	}

	var stories []ai.PRDescriptionStory
	for _, s := range prdInfo.Stories {
		stories = append(stories, ai.PRDescriptionStory{
			ID:    s.ID,
			Title: s.Title,
		})
	}

	prompt, err := ai.RenderPRDescription(ai.PRDescriptionData{
		PRDSummary:            prdInfo.Description,
		Stories:               stories,
		DiffStats:             diffStats,
		LinearIssueIdentifier: input.Identifier,
	}, input.OverrideDir)
	if err != nil {
		return "", "", fmt.Errorf("rendering PR prompt: %w", err)
	}

	aiOutput, err := inv.Invoke(ctx, prompt, input.TreePath, maxTurnsPR)
	if err != nil {
		return "", "", fmt.Errorf("invoking AI for PR description: %w", err)
	}

	title, body = parsePROutput(aiOutput)
	return title, body, nil
}

// UpdateDescription generates a new PR description and updates the existing PR.
// It is non-fatal by design: errors are logged as warnings and not returned.
func UpdateDescription(ctx context.Context, inv Invoker, diff DiffStatter, prd PRDReader, cfgLoad ConfigLoader, editor GitHubPREditor, issue db.Issue, project db.Project) {
	treePath := workspace.TreePath(project.LocalPath, issue.WorkspaceName)

	defaultBase, err := cfgLoad.DefaultBase(project.LocalPath, project.RalphConfigPath)
	if err != nil {
		slog.Warn("update PR description: loading default base", "issue", issue.Identifier, "error", err)
		return
	}

	prdPath := workspace.PRDPathForWorkspace(project.LocalPath, issue.WorkspaceName)

	title, body, err := GenerateDescription(ctx, inv, diff, prd, cfgLoad, DescriptionInput{
		TreePath:    treePath,
		DefaultBase: defaultBase,
		PRDPath:     prdPath,
		Identifier:  issue.Identifier,
	})
	if err != nil {
		slog.Warn("update PR description: generating description", "issue", issue.Identifier, "error", err)
		return
	}

	if err := editor.EditPullRequest(ctx, project.GithubOwner, project.GithubRepo, issue.PRNumber, title, body); err != nil {
		slog.Warn("update PR description: editing pull request", "issue", issue.Identifier, "pr", issue.PRNumber, "error", err)
	}
}

// Config holds the dependencies for the PR creation action.
type Config struct {
	Invoker     Invoker
	Git         GitPusher
	Diff        DiffStatter
	PRD         PRDReader
	GitHub      GitHubPRCreator
	Linear      LinearPoster
	Projects    ProjectGetter
	ConfigLoad  ConfigLoader
	Rebase      Rebaser // optional: when set, attempts rebase on push failure
	OverrideDir string
}

// NewAction returns a function that creates a GitHub PR for a completed build.
// It pushes the branch, generates a PR description via AI, creates the PR,
// stores PR info in the issue, and posts a Linear comment with the PR link.
func NewAction(cfg Config) func(issue db.Issue, database *db.DB) error {
	return func(issue db.Issue, database *db.DB) error {
		ctx := context.Background()

		project, err := cfg.Projects.GetProject(issue.ProjectID)
		if err != nil {
			return fmt.Errorf("loading project: %w", err)
		}

		treePath := workspace.TreePath(project.LocalPath, issue.WorkspaceName)

		defaultBase, err := cfg.ConfigLoad.DefaultBase(project.LocalPath, project.RalphConfigPath)
		if err != nil {
			return fmt.Errorf("loading default base: %w", err)
		}

		if err := pushWithRebase(ctx, cfg, treePath, issue.BranchName, defaultBase); err != nil {
			return err
		}

		prdPath := workspace.PRDPathForWorkspace(project.LocalPath, issue.WorkspaceName)

		title, body, err := GenerateDescription(ctx, cfg.Invoker, cfg.Diff, cfg.PRD, cfg.ConfigLoad, DescriptionInput{
			TreePath:    treePath,
			DefaultBase: defaultBase,
			PRDPath:     prdPath,
			Identifier:  issue.Identifier,
			OverrideDir: cfg.OverrideDir,
		})
		if err != nil {
			return err
		}

		// Idempotent: check for existing open PR before creating
		existingPR, err := cfg.GitHub.FindOpenPR(ctx,
			project.GithubOwner, project.GithubRepo,
			issue.BranchName, defaultBase,
		)
		if err != nil {
			return fmt.Errorf("checking for existing PR: %w", err)
		}

		var ghPR PRResult
		if existingPR != nil {
			ghPR = *existingPR
		} else {
			ghPR, err = cfg.GitHub.CreatePullRequest(ctx,
				project.GithubOwner, project.GithubRepo,
				issue.BranchName, defaultBase,
				title, body,
			)
			if err != nil {
				return fmt.Errorf("creating GitHub PR: %w", err)
			}
		}

		issue.PRNumber = ghPR.Number
		issue.PRURL = ghPR.HTMLURL
		if err := database.UpdateIssue(issue); err != nil {
			return fmt.Errorf("storing PR info: %w", err)
		}

		comment := fmt.Sprintf("PR created: [#%d](%s)", ghPR.Number, ghPR.HTMLURL)
		commentID, err := cfg.Linear.PostComment(ctx, issue.LinearIssueID, comment)
		if err != nil {
			return fmt.Errorf("posting PR link to Linear: %w", err)
		}

		issue.LastCommentID = commentID
		if err := database.UpdateIssue(issue); err != nil {
			return fmt.Errorf("storing last comment ID: %w", err)
		}

		if err := database.LogActivity(
			issue.ID,
			"pr_created",
			"building",
			"in_review",
			fmt.Sprintf("Created PR #%d: %s", ghPR.Number, ghPR.HTMLURL),
		); err != nil {
			return fmt.Errorf("logging activity: %w", err)
		}

		return nil
	}
}

// parsePROutput splits the AI response into title (first line) and body (rest).
func parsePROutput(output string) (string, string) {
	output = strings.TrimSpace(output)
	parts := strings.SplitN(output, "\n", 2)
	title := strings.TrimSpace(parts[0])
	var body string
	if len(parts) > 1 {
		body = strings.TrimSpace(parts[1])
	}
	return title, body
}

// capDiffStats caps the diff stats output to maxEntries file entries plus the
// final summary line. If there are more entries, a marker is inserted.
func capDiffStats(stats string, maxEntries int) string {
	if stats == "" {
		return stats
	}
	lines := strings.Split(stats, "\n")
	// The last line is the summary (e.g. " 80 files changed, ...").
	// Everything before it is a file entry.
	if len(lines) <= maxEntries+1 {
		return stats
	}

	summary := lines[len(lines)-1]
	omitted := len(lines) - 1 - maxEntries
	marker := fmt.Sprintf("[... %d file entries omitted ...]", omitted)

	result := make([]string, 0, maxEntries+2)
	result = append(result, lines[:maxEntries]...)
	result = append(result, marker)
	result = append(result, summary)
	return strings.Join(result, "\n")
}

// pushWithRebase attempts to push the branch. If push fails and a Rebaser is
// configured, it fetches the base, rebases, and retries. If the rebase results
// in conflicts, it aborts the rebase and returns a ConflictError.
func pushWithRebase(ctx context.Context, cfg Config, treePath, branch, base string) error {
	pushErr := cfg.Git.PushBranch(ctx, treePath, branch)
	if pushErr == nil {
		return nil
	}

	if cfg.Rebase == nil {
		return fmt.Errorf("pushing branch: %w", pushErr)
	}

	// Push failed — try rebase onto origin/<base>
	if err := cfg.Rebase.FetchBranch(ctx, treePath, base); err != nil {
		return fmt.Errorf("fetching base branch: %w", err)
	}

	result, err := cfg.Rebase.StartRebase(ctx, treePath, "origin/"+base)
	if err != nil {
		return fmt.Errorf("starting rebase: %w", err)
	}

	if result.HasConflicts {
		files, _ := cfg.Rebase.ConflictFiles(ctx, treePath)
		_ = cfg.Rebase.AbortRebase(ctx, treePath)
		return &ConflictError{Files: files}
	}

	// Rebase succeeded — retry push with force
	if err := cfg.Git.PushBranch(ctx, treePath, branch); err != nil {
		return fmt.Errorf("pushing branch after rebase: %w", err)
	}
	return nil
}
