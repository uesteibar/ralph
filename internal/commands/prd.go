package commands

import (
	"context"
	"flag"
	"fmt"

	"github.com/uesteibar/ralph/internal/claude"
	"github.com/uesteibar/ralph/internal/prompts"
	"github.com/uesteibar/ralph/internal/workspace"
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
	workspaceFlag := AddWorkspaceFlag(fs)
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

	data := prompts.PRDNewData{
		ProjectName: cfg.Project,
		PRDPath:     wc.PRDPath,
	}

	// If in a workspace, look up the branch name from registry.
	if wc.Name != "base" {
		ws, lookupErr := workspace.RegistryGet(cfg.Repo.Path, wc.Name)
		if lookupErr == nil {
			data.WorkspaceBranch = ws.Branch
		}
	}

	prompt, err := prompts.RenderPRDNew(data, cfg.PromptsDir())
	if err != nil {
		return fmt.Errorf("rendering PRD prompt: %w", err)
	}

	_, err = claude.Invoke(context.Background(), claude.InvokeOpts{
		Prompt:      prompt,
		Dir:         wc.WorkDir,
		Interactive: true,
	})
	return err
}
