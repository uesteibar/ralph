package commands

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/uesteibar/ralph/internal/gitops"
	"github.com/uesteibar/ralph/internal/prd"
	"github.com/uesteibar/ralph/internal/shell"
	"github.com/uesteibar/ralph/internal/workspace"
)

// Overview handles the `ralph overview` command.
func Overview(args []string) error {
	return overviewRun(args, os.Stdout)
}

func overviewRun(args []string, w io.Writer) error {
	fs := flag.NewFlagSet("overview", flag.ExitOnError)
	configPath := AddProjectConfigFlag(fs)
	if err := fs.Parse(args); err != nil {
		return err
	}

	cfg, err := ResolveConfig(*configPath)
	if err != nil {
		return fmt.Errorf("resolving config: %w", err)
	}

	repoPath := cfg.Repo.Path

	// Resolve current workspace to mark it.
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("getting working directory: %w", err)
	}
	currentWC, _ := workspace.ResolveWorkContext("", os.Getenv("RALPH_WORKSPACE"), cwd, repoPath)

	// Get base branch.
	ctx := context.Background()
	r := &shell.Runner{Dir: repoPath}
	baseBranch, err := gitops.CurrentBranch(ctx, r)
	if err != nil {
		baseBranch = cfg.Repo.DefaultBase
	}

	// Print base entry.
	basePrefix := "  "
	baseSuffix := ""
	if currentWC.Name == "base" {
		basePrefix = "* "
		baseSuffix = " " + hintStyle.Render("[current]")
	}
	fmt.Fprintf(w, "%s%s  %s%s\n",
		basePrefix,
		valueStyle.Render("base"),
		hintStyle.Render(baseBranch),
		baseSuffix,
	)

	// Get all workspaces with missing detection.
	entries, err := workspace.RegistryListWithMissing(repoPath)
	if err != nil {
		return fmt.Errorf("reading workspace registry: %w", err)
	}

	for _, entry := range entries {
		prefix := "  "
		if entry.Name == currentWC.Name {
			prefix = "* "
		}

		name := valueStyle.Render(entry.Name)
		branch := hintStyle.Render(entry.Branch)

		if entry.Missing {
			fmt.Fprintf(w, "%s%s  %s  %s\n", prefix, name, branch, failStyle.Render("[missing]"))
			continue
		}

		prdPath := workspace.PRDPathForWorkspace(repoPath, entry.Name)
		p, prdErr := prd.Read(prdPath)
		if prdErr != nil {
			suffix := ""
			if entry.Name == currentWC.Name {
				suffix = " " + hintStyle.Render("[current]")
			}
			fmt.Fprintf(w, "%s%s  %s  %s%s\n", prefix, name, branch, hintStyle.Render("(no prd)"), suffix)
			continue
		}

		passing, total := storyProgress(p)
		var storyStr string
		if passing == total {
			storyStr = passStyle.Render(fmt.Sprintf("Stories: %d/%d", passing, total))
		} else {
			storyStr = failStyle.Render(fmt.Sprintf("Stories: %d/%d", passing, total))
		}

		parts := fmt.Sprintf("%s%s  %s  %s", prefix, name, branch, storyStr)

		if len(p.IntegrationTests) > 0 {
			itPassing, itTotal := integrationTestProgress(p)
			var testStr string
			if itPassing == itTotal {
				testStr = passStyle.Render(fmt.Sprintf("Tests: %d/%d", itPassing, itTotal))
			} else {
				testStr = failStyle.Render(fmt.Sprintf("Tests: %d/%d", itPassing, itTotal))
			}
			parts += "  " + testStr
		}

		if entry.Name == currentWC.Name {
			parts += " " + hintStyle.Render("[current]")
		}

		fmt.Fprintln(w, parts)
	}

	return nil
}
