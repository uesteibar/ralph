package commands

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/uesteibar/ralph/internal/claude"
	"github.com/uesteibar/ralph/internal/prompts"
	"github.com/uesteibar/ralph/internal/shell"
)

// Chat runs a free-form interactive Claude session in the project context.
func Chat(args []string) error {
	fs := flag.NewFlagSet("chat", flag.ExitOnError)
	configPath := AddProjectConfigFlag(fs)
	continueFlag := fs.Bool("continue", false, "Resume the most recent conversation")
	if err := fs.Parse(args); err != nil {
		return err
	}

	cfg, err := ResolveConfig(*configPath)
	if err != nil {
		return fmt.Errorf("resolving config: %w", err)
	}

	if _, ok := worktreeRoot(cfg.Repo.Path); !ok {
		return fmt.Errorf("ralph chat must be run from inside a worktree\n\nUse 'ralph run' first to create a worktree, then cd into it.")
	}

	data := prompts.ChatSystemData{
		ProjectName: cfg.Project,
	}

	// Read config YAML for context
	configYAML, err := os.ReadFile(filepath.Join(cfg.Repo.Path, ".ralph", "ralph.yaml"))
	if err == nil {
		data.Config = string(configYAML)
	}

	// Read progress log
	progress, err := os.ReadFile(cfg.ProgressPath())
	if err == nil {
		data.Progress = string(progress)
	}

	// Read recent git commits
	r := &shell.Runner{Dir: cfg.Repo.Path}
	commits, err := r.Run(context.Background(), "git", "log", "--oneline", "-20")
	if err == nil {
		data.RecentCommits = commits
	}

	prompt, err := prompts.RenderChatSystem(data)
	if err != nil {
		return fmt.Errorf("rendering chat prompt: %w", err)
	}

	_, err = claude.Invoke(context.Background(), claude.InvokeOpts{
		Prompt:      prompt,
		Dir:         cfg.Repo.Path,
		Interactive: true,
		Continue:    *continueFlag,
	})
	return err
}
