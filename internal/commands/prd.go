package commands

import (
	"context"
	"flag"
	"fmt"

	"github.com/uesteibar/ralph/internal/claude"
	"github.com/uesteibar/ralph/internal/prompts"
)

// PRD handles the `ralph prd` subcommand.
func PRD(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: ralph prd new")
	}

	subcmd := args[0]
	rest := args[1:]

	switch subcmd {
	case "new":
		return prdNew(rest)
	default:
		return fmt.Errorf("unknown prd subcommand: %s (use 'new')", subcmd)
	}
}

func prdNew(args []string) error {
	fs := flag.NewFlagSet("prd new", flag.ExitOnError)
	configPath := AddProjectConfigFlag(fs)
	if err := fs.Parse(args); err != nil {
		return err
	}

	cfg, err := ResolveConfig(*configPath)
	if err != nil {
		return fmt.Errorf("resolving config: %w", err)
	}

	prompt, err := prompts.RenderPRDNew(cfg.Project)
	if err != nil {
		return fmt.Errorf("rendering PRD prompt: %w", err)
	}

	_, err = claude.Invoke(context.Background(), claude.InvokeOpts{
		Prompt:      prompt,
		Dir:         cfg.Repo.Path,
		Interactive: true,
	})
	return err
}
