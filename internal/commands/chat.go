package commands

import (
	"context"
	"flag"
	"fmt"
	"log"
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
	if err := fs.Parse(args); err != nil {
		return err
	}

	cfg, err := ResolveConfig(*configPath)
	if err != nil {
		log.Printf("[chat] no project config found, running without context: %v", err)
		_, invokeErr := claude.Invoke(context.Background(), claude.InvokeOpts{
			Interactive: true,
		})
		return invokeErr
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
	})
	return err
}
