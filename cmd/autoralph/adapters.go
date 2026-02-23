package main

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/uesteibar/ralph/internal/autoralph/build"
	"github.com/uesteibar/ralph/internal/autoralph/checks"
	"github.com/uesteibar/ralph/internal/autoralph/complete"
	"github.com/uesteibar/ralph/internal/autoralph/feedback"
	"github.com/uesteibar/ralph/internal/autoralph/ghpoller"
	"github.com/uesteibar/ralph/internal/autoralph/rebase"
	"github.com/uesteibar/ralph/internal/autoralph/usagelimit"
	ghclient "github.com/uesteibar/ralph/internal/autoralph/github"
	"github.com/uesteibar/ralph/internal/autoralph/invoker"
	"github.com/uesteibar/ralph/internal/autoralph/linear"
	"github.com/uesteibar/ralph/internal/autoralph/pr"
	"github.com/uesteibar/ralph/internal/autoralph/worker"
	"log/slog"

	"github.com/uesteibar/ralph/internal/claude"
	"github.com/uesteibar/ralph/internal/config"
	"github.com/uesteibar/ralph/internal/events"
	"github.com/uesteibar/ralph/internal/gitops"
	"github.com/uesteibar/ralph/internal/loop"
	"github.com/uesteibar/ralph/internal/prd"
	"github.com/uesteibar/ralph/internal/shell"
	"github.com/uesteibar/ralph/internal/workspace"
)

// Compile-time interface checks.
var (
	_ checks.CheckRunFetcher       = (*ghclient.Client)(nil)
	_ checks.LogFetcher            = (*ghclient.Client)(nil)
	_ checks.PRFetcher             = (*ghclient.Client)(nil)
	_ checks.PRCommenter           = (*ghclient.Client)(nil)
	_ checks.ConfigLoader          = (*configLoaderAdapter)(nil)
	_ checks.BranchPuller          = (*branchPullerAdapter)(nil)
	_ feedback.ConfigLoader        = (*configLoaderAdapter)(nil)
	_ feedback.CommentFetcher      = (*ghclient.Client)(nil)
	_ feedback.ReviewFetcher       = (*ghclient.Client)(nil)
	_ feedback.IssueCommentFetcher = (*ghclient.Client)(nil)
	_ feedback.ReviewReplier       = (*ghclient.Client)(nil)
	_ feedback.PRCommenter         = (*ghclient.Client)(nil)
	_ feedback.CommentReactor      = (*ghclient.Client)(nil)
	_ feedback.IssueCommentReactor = (*ghclient.Client)(nil)
	_ feedback.BranchPuller        = (*branchPullerAdapter)(nil)
	_ rebase.BranchPuller          = (*branchPullerAdapter)(nil)
	_ ghpoller.GitHubClient        = (*ghclient.Client)(nil)
	_ invoker.EventInvoker         = (*claudeInvoker)(nil)
	_ invoker.EventInvoker         = (*usagelimitInvoker)(nil)
)

// claudeInvoker wraps claude.Invoke to satisfy the Invoker interface used by
// refine, approve, build, feedback, and pr packages.
type claudeInvoker struct {
	// DisallowedTools prevents the AI from using specific tools.
	// Used to block write operations during read-only phases like refinement.
	DisallowedTools []string
}

func (c *claudeInvoker) Invoke(ctx context.Context, prompt, dir string, maxTurns int) (string, error) {
	return claude.Invoke(ctx, claude.InvokeOpts{
		Prompt:          prompt,
		Dir:             dir,
		Print:           true,
		MaxTurns:        maxTurns,
		DisallowedTools: c.DisallowedTools,
	})
}

func (c *claudeInvoker) InvokeWithEvents(ctx context.Context, prompt, dir string, maxTurns int, handler events.EventHandler) (string, error) {
	return claude.Invoke(ctx, claude.InvokeOpts{
		Prompt:          prompt,
		Dir:             dir,
		Print:           true,
		MaxTurns:        maxTurns,
		DisallowedTools: c.DisallowedTools,
		EventHandler:    handler,
	})
}

// fullInvoker combines both Invoke and InvokeWithEvents for the
// usagelimitInvoker wrapper. The claudeInvoker satisfies this interface.
type fullInvoker interface {
	Invoke(ctx context.Context, prompt, dir string, maxTurns int) (string, error)
	InvokeWithEvents(ctx context.Context, prompt, dir string, maxTurns int, handler events.EventHandler) (string, error)
}

// usagelimitInvoker wraps an invoker with wait-and-retry logic for usage limits.
// When a usage limit is already known (from another worker), it waits before
// invoking. When claude.Invoke returns a UsageLimitError, it records the reset
// time in shared state and retries after waiting.
type usagelimitInvoker struct {
	inner fullInvoker
	state *usagelimit.State
}

func (u *usagelimitInvoker) Invoke(ctx context.Context, prompt, dir string, maxTurns int) (string, error) {
	for {
		if err := u.state.Wait(ctx); err != nil {
			return "", err
		}

		out, err := u.inner.Invoke(ctx, prompt, dir, maxTurns)

		var ulErr *claude.UsageLimitError
		if errors.As(err, &ulErr) {
			u.state.Set(ulErr.ResetAt)
			continue
		}

		return out, err
	}
}

func (u *usagelimitInvoker) InvokeWithEvents(ctx context.Context, prompt, dir string, maxTurns int, handler events.EventHandler) (string, error) {
	for {
		if err := u.state.Wait(ctx); err != nil {
			return "", err
		}

		out, err := u.inner.InvokeWithEvents(ctx, prompt, dir, maxTurns, handler)

		var ulErr *claude.UsageLimitError
		if errors.As(err, &ulErr) {
			u.state.Set(ulErr.ResetAt)
			continue
		}

		return out, err
	}
}

// loopRunnerAdapter wraps loop.Run to satisfy worker.LoopRunner.
type loopRunnerAdapter struct{}

func (l *loopRunnerAdapter) Run(ctx context.Context, cfg worker.LoopConfig) error {
	return loop.Run(ctx, loop.Config{
		MaxIterations: cfg.MaxIterations,
		WorkDir:       cfg.WorkDir,
		PRDPath:       cfg.PRDPath,
		ProgressPath:  cfg.ProgressPath,
		PromptsDir:    cfg.PromptsDir,
		QualityChecks: cfg.QualityChecks,
		KnowledgePath: cfg.KnowledgePath,
		Verbose:       cfg.Verbose,
		EventHandler:  cfg.EventHandler,
	})
}

// linearCommentPoster wraps linear.Client.PostComment to satisfy refine.Poster.
type linearCommentPoster struct {
	client *linear.Client
}

func (p *linearCommentPoster) PostComment(ctx context.Context, issueID, body string) (string, error) {
	c, err := p.client.PostComment(ctx, issueID, body)
	return c.ID, err
}

// linearPoster wraps linear.Client.PostComment to satisfy pr.LinearPoster
// (which returns the comment ID as a string).
type linearPoster struct {
	client *linear.Client
}

func (p *linearPoster) PostComment(ctx context.Context, issueID, body string) (string, error) {
	c, err := p.client.PostComment(ctx, issueID, body)
	if err != nil {
		return "", err
	}
	return c.ID, nil
}

// buildLinearUpdater wraps linear.Client to satisfy build.LinearStateUpdater.
type buildLinearUpdater struct {
	client *linear.Client
}

func (u *buildLinearUpdater) FetchWorkflowStates(ctx context.Context, teamID string) ([]build.WorkflowState, error) {
	states, err := u.client.FetchWorkflowStates(ctx, teamID)
	if err != nil {
		return nil, err
	}
	result := make([]build.WorkflowState, len(states))
	for i, s := range states {
		result[i] = build.WorkflowState{ID: s.ID, Name: s.Name, Type: s.Type}
	}
	return result, nil
}

func (u *buildLinearUpdater) UpdateIssueState(ctx context.Context, issueID, stateID string) error {
	return u.client.UpdateIssueState(ctx, issueID, stateID)
}

// completeLinearUpdater wraps linear.Client to satisfy complete.LinearStateUpdater.
type completeLinearUpdater struct {
	client *linear.Client
}

func (u *completeLinearUpdater) FetchWorkflowStates(ctx context.Context, teamID string) ([]complete.WorkflowState, error) {
	states, err := u.client.FetchWorkflowStates(ctx, teamID)
	if err != nil {
		return nil, err
	}
	result := make([]complete.WorkflowState, len(states))
	for i, s := range states {
		result[i] = complete.WorkflowState{ID: s.ID, Name: s.Name, Type: s.Type}
	}
	return result, nil
}

func (u *completeLinearUpdater) UpdateIssueState(ctx context.Context, issueID, stateID string) error {
	return u.client.UpdateIssueState(ctx, issueID, stateID)
}

// workspaceCreatorAdapter wraps workspace.CreateWorkspace.
type workspaceCreatorAdapter struct {
	// pullFn pulls the base branch before workspace creation.
	// When nil, the pull step is skipped.
	pullFn func(ctx context.Context, r *shell.Runner, branch string) error
}

func (w *workspaceCreatorAdapter) Create(ctx context.Context, repoPath string, ws workspace.Workspace, base string, copyPatterns []string) error {
	r := &shell.Runner{Dir: repoPath}
	// Prune stale worktree registrations before creating to avoid
	// "already registered worktree" errors from previous failed attempts.
	_ = gitops.WorktreePrune(ctx, r)

	if w.pullFn != nil {
		if err := w.pullFn(ctx, r, base); err != nil {
			slog.Warn("git pull --ff-only failed before workspace creation", "branch", base, "error", err)
		}
	}

	return workspace.CreateWorkspace(ctx, r, repoPath, ws, base, copyPatterns)
}

// gitPullerAdapter resolves the default base branch and pulls it via
// gitops.PullFFOnly. It implements refine.GitPuller and approve.GitPuller.
type gitPullerAdapter struct {
	defaultBaseFn func(repoPath, ralphConfigPath string) (string, error)
	pullFn        func(ctx context.Context, r *shell.Runner, branch string) error
}

func (g *gitPullerAdapter) PullDefaultBase(ctx context.Context, repoPath, ralphConfigPath string) error {
	base, err := g.defaultBaseFn(repoPath, ralphConfigPath)
	if err != nil {
		return fmt.Errorf("resolving default base: %w", err)
	}
	r := &shell.Runner{Dir: repoPath}
	return g.pullFn(ctx, r, base)
}

// workspaceRemoverAdapter wraps workspace.RemoveWorkspace.
type workspaceRemoverAdapter struct{}

func (w *workspaceRemoverAdapter) RemoveWorkspace(ctx context.Context, repoPath, name string) error {
	r := &shell.Runner{Dir: repoPath}
	return workspace.RemoveWorkspace(ctx, r, repoPath, name)
}

// configLoaderAdapter satisfies build.ConfigLoader, feedback.ConfigLoader, and pr.ConfigLoader.
type configLoaderAdapter struct{}

func (c *configLoaderAdapter) Load(path string) (*config.Config, error) {
	return config.Load(path)
}

func (c *configLoaderAdapter) DefaultBase(projectLocalPath, ralphConfigPath string) (string, error) {
	cfg, err := config.Load(filepath.Join(projectLocalPath, ralphConfigPath))
	if err != nil {
		return "", err
	}
	return cfg.Repo.DefaultBase, nil
}

// buildPRDReaderAdapter wraps prd.Read to satisfy build.PRDReader.
type buildPRDReaderAdapter struct{}

func (p *buildPRDReaderAdapter) Read(path string) (*prd.PRD, error) {
	return prd.Read(path)
}

// prdReaderAdapter wraps prd.Read to satisfy pr.PRDReader.
type prdReaderAdapter struct{}

func (p *prdReaderAdapter) ReadPRD(path string) (pr.PRDInfo, error) {
	d, err := prd.Read(path)
	if err != nil {
		return pr.PRDInfo{}, err
	}
	stories := make([]pr.StoryInfo, len(d.UserStories))
	for i, s := range d.UserStories {
		stories[i] = pr.StoryInfo{ID: s.ID, Title: s.Title}
	}
	return pr.PRDInfo{
		Description: d.Description,
		Stories:     stories,
	}, nil
}

// gitOpsAdapter wraps gitops functions to satisfy feedback.GitOps, pr.GitPusher,
// pr.DiffStatter, and pr.Rebaser.
type gitOpsAdapter struct {
	gitAuthorName  string
	gitAuthorEmail string
}

func (g *gitOpsAdapter) gitEnv() []string {
	return []string{
		"GIT_AUTHOR_NAME=" + g.gitAuthorName,
		"GIT_AUTHOR_EMAIL=" + g.gitAuthorEmail,
		"GIT_COMMITTER_NAME=" + g.gitAuthorName,
		"GIT_COMMITTER_EMAIL=" + g.gitAuthorEmail,
	}
}

func (g *gitOpsAdapter) Commit(ctx context.Context, workDir, message string) error {
	r := &shell.Runner{Dir: workDir, Env: g.gitEnv()}
	return gitops.Commit(ctx, r, message)
}

func (g *gitOpsAdapter) PushBranch(ctx context.Context, workDir, branch string) error {
	r := &shell.Runner{Dir: workDir, Env: g.gitEnv()}
	return gitops.PushBranch(ctx, r, branch)
}

func (g *gitOpsAdapter) HeadSHA(ctx context.Context, workDir string) (string, error) {
	r := &shell.Runner{Dir: workDir, Env: g.gitEnv()}
	out, err := r.Run(ctx, "git", "rev-parse", "HEAD")
	if err != nil {
		return "", fmt.Errorf("git rev-parse HEAD: %w", err)
	}
	return strings.TrimSpace(out), nil
}

func (g *gitOpsAdapter) DiffStats(ctx context.Context, workDir, base string) (string, error) {
	r := &shell.Runner{Dir: workDir, Env: g.gitEnv()}
	return gitops.DiffStats(ctx, r, base)
}

func (g *gitOpsAdapter) FetchBranch(ctx context.Context, workDir, branch string) error {
	r := &shell.Runner{Dir: workDir, Env: g.gitEnv()}
	return gitops.FetchBranch(ctx, r, branch)
}

func (g *gitOpsAdapter) StartRebase(ctx context.Context, workDir, onto string) (pr.RebaseResult, error) {
	r := &shell.Runner{Dir: workDir, Env: g.gitEnv()}
	result, err := gitops.StartRebase(ctx, r, onto)
	if err != nil {
		return pr.RebaseResult{}, err
	}
	return pr.RebaseResult{
		Success:      result.Success,
		HasConflicts: result.HasConflicts,
	}, nil
}

func (g *gitOpsAdapter) AbortRebase(ctx context.Context, workDir string) error {
	r := &shell.Runner{Dir: workDir, Env: g.gitEnv()}
	return gitops.AbortRebase(ctx, r)
}

func (g *gitOpsAdapter) ConflictFiles(ctx context.Context, workDir string) ([]string, error) {
	r := &shell.Runner{Dir: workDir, Env: g.gitEnv()}
	return gitops.ConflictFiles(ctx, r)
}

// ghPRCreatorAdapter wraps github.Client to satisfy pr.GitHubPRCreator.
type ghPRCreatorAdapter struct {
	client *ghclient.Client
	owner  string
	repo   string
}

func (g *ghPRCreatorAdapter) CreatePullRequest(ctx context.Context, owner, repo, head, base, title, body string) (pr.PRResult, error) {
	p, err := g.client.CreatePullRequest(ctx, owner, repo, head, base, title, body)
	if err != nil {
		return pr.PRResult{}, err
	}
	return pr.PRResult{Number: p.Number, HTMLURL: p.HTMLURL}, nil
}

func (g *ghPRCreatorAdapter) FindOpenPR(ctx context.Context, owner, repo, head, base string) (*pr.PRResult, error) {
	p, err := g.client.FindOpenPR(ctx, owner, repo, head, base)
	if err != nil {
		return nil, err
	}
	if p == nil {
		return nil, nil
	}
	return &pr.PRResult{Number: p.Number, HTMLURL: p.HTMLURL}, nil
}

func (g *gitOpsAdapter) IsAncestor(ctx context.Context, workDir, ancestor, descendant string) (bool, error) {
	r := &shell.Runner{Dir: workDir}
	return gitops.IsAncestor(ctx, r, ancestor, descendant)
}

func (g *gitOpsAdapter) ForcePushBranch(ctx context.Context, workDir, branch string) error {
	r := &shell.Runner{Dir: workDir}
	return gitops.ForcePushBranch(ctx, r, branch)
}

// branchPullerAdapter creates a shell.Runner with the provided workDir and
// delegates to gitops.PullFFOnly. It satisfies feedback.BranchPuller,
// checks.BranchPuller, and rebase.BranchPuller.
type branchPullerAdapter struct{}

func (b *branchPullerAdapter) PullBranch(ctx context.Context, workDir, branch string) error {
	r := &shell.Runner{Dir: workDir}
	return gitops.PullFFOnly(ctx, r, branch)
}

// rebaseRunnerAdapter invokes ralph rebase as a subprocess to satisfy
// rebase.RebaseRunner.
type rebaseRunnerAdapter struct {
	// cmdFn builds the exec.Cmd. When nil, defaults to exec.CommandContext.
	cmdFn func(ctx context.Context, name string, args ...string) *exec.Cmd
}

func (r *rebaseRunnerAdapter) RunRebase(ctx context.Context, base, workspaceName, projectConfigPath string) error {
	buildCmd := r.cmdFn
	if buildCmd == nil {
		buildCmd = exec.CommandContext
	}
	cmd := buildCmd(ctx, "ralph", "rebase",
		"--workspace", workspaceName,
		"--project-config", projectConfigPath,
		base,
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("ralph rebase: %w\n%s", err, out)
	}
	return nil
}
