package commands

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/uesteibar/ralph/internal/claude"
	"github.com/uesteibar/ralph/internal/knowledge"
	"github.com/uesteibar/ralph/internal/prd"
	"github.com/uesteibar/ralph/internal/progress"
	"github.com/uesteibar/ralph/internal/prompts"
	"github.com/uesteibar/ralph/internal/shell"
)

// Chat runs a free-form interactive Claude session in the project context.
func Chat(args []string) error {
	fs := flag.NewFlagSet("chat", flag.ExitOnError)
	configPath := AddProjectConfigFlag(fs)
	workspaceFlag := AddWorkspaceFlag(fs)
	continueFlag := fs.Bool("continue", false, "Resume the most recent conversation")
	if err := fs.Parse(args); err != nil {
		return err
	}

	cfg, err := ResolveConfig(*configPath)
	if err != nil {
		return fmt.Errorf("resolving config: %w", err)
	}

	wc, err := resolveWorkContextFromFlags(*workspaceFlag, cfg.Repo.Path)
	if err != nil {
		return fmt.Errorf("resolving workspace context: %w", err)
	}

	printWorkspaceHeader(wc, cfg.Repo.Path)

	data := prompts.ChatSystemData{
		ProjectName:   cfg.Project,
		WorkspaceName: wc.Name,
		KnowledgePath: knowledge.Dir(wc.WorkDir),
	}

	// Read config YAML for context
	configYAML, err := os.ReadFile(filepath.Join(cfg.Repo.Path, ".ralph", "ralph.yaml"))
	if err == nil {
		data.Config = string(configYAML)
	}

	// Read progress log from workspace (or repo root for base), capped to recent entries
	progressContent, err := os.ReadFile(wc.ProgressPath)
	if err == nil {
		data.Progress = progress.CapProgressEntries(string(progressContent), progress.DefaultMaxEntries)
	}

	// Read recent git commits from workspace workDir
	r := &shell.Runner{Dir: wc.WorkDir}
	commits, err := r.Run(context.Background(), "git", "log", "--oneline", "-20")
	if err == nil {
		data.RecentCommits = commits
	}

	// Read PRD from workspace-level path for context if exists
	p, err := prd.Read(wc.PRDPath)
	if err == nil {
		data.PRDContext = formatPRDContext(p)
	}

	prompt, err := prompts.RenderChatSystem(data, cfg.PromptsDir())
	if err != nil {
		return fmt.Errorf("rendering chat prompt: %w", err)
	}

	_, err = claude.Invoke(context.Background(), claude.InvokeOpts{
		Prompt:      prompt,
		Dir:         wc.WorkDir,
		Interactive: true,
		Continue:    *continueFlag,
	})
	return err
}

// formatPRDContext formats PRD data as a brief context summary.
func formatPRDContext(p *prd.PRD) string {
	summary := fmt.Sprintf("Project: %s\nDescription: %s\n", p.Project, p.Description)
	if len(p.UserStories) > 0 {
		summary += "Stories:\n"
		summary += formatStories(p.UserStories)
	}
	return summary
}
