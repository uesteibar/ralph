package commands

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/charmbracelet/lipgloss"
	"github.com/uesteibar/ralph/internal/gitops"
	"github.com/uesteibar/ralph/internal/prd"
	"github.com/uesteibar/ralph/internal/shell"
	"github.com/uesteibar/ralph/internal/workspace"
)

var (
	labelStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("4")).Bold(true)
	valueStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("7"))
	hintStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("8")).Italic(true)
	passStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("2"))
	failStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("1"))
)

// Status handles the `ralph status` command.
func Status(args []string) error {
	return statusRun(args, os.Stdout)
}

func statusRun(args []string, w io.Writer) error {
	fs := flag.NewFlagSet("status", flag.ExitOnError)
	configPath := AddProjectConfigFlag(fs)
	short := fs.Bool("short", false, "Short output for shell prompt embedding")
	if err := fs.Parse(args); err != nil {
		return err
	}

	cfg, err := ResolveConfig(*configPath)
	if err != nil {
		return fmt.Errorf("resolving config: %w", err)
	}

	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("getting working directory: %w", err)
	}

	wc, err := workspace.ResolveWorkContext("", os.Getenv("RALPH_WORKSPACE"), cwd, cfg.Repo.Path)
	if err != nil {
		return fmt.Errorf("resolving workspace: %w", err)
	}

	if *short {
		return statusShort(w, wc)
	}

	printWorkspaceHeader(wc, cfg.Repo.Path)
	return statusFull(w, wc, cfg.Repo.Path)
}

func statusShort(w io.Writer, wc workspace.WorkContext) error {
	if wc.Name == "base" {
		fmt.Fprint(w, "base")
		return nil
	}

	p, err := prd.Read(wc.PRDPath)
	if err != nil {
		fmt.Fprintf(w, "%s (no prd)", wc.Name)
		return nil
	}

	passing, total := storyProgress(p)
	fmt.Fprintf(w, "%s %d/%d", wc.Name, passing, total)
	return nil
}

func statusFull(w io.Writer, wc workspace.WorkContext, repoPath string) error {
	fmt.Fprintf(w, "%s %s\n", labelStyle.Render("Workspace:"), valueStyle.Render(wc.Name))

	if wc.Name == "base" {
		ctx := context.Background()
		r := &shell.Runner{Dir: repoPath}
		branch, err := gitops.CurrentBranch(ctx, r)
		if err != nil {
			return fmt.Errorf("getting current branch: %w", err)
		}
		fmt.Fprintf(w, "%s %s\n", labelStyle.Render("Branch:"), valueStyle.Render(branch))
		fmt.Fprintln(w)
		fmt.Fprintln(w, hintStyle.Render("Tip: ralph workspaces new <name>"))
		return nil
	}

	// In a workspace: get branch from registry.
	ws, err := workspace.RegistryGet(repoPath, wc.Name)
	if err == nil && ws != nil {
		fmt.Fprintf(w, "%s %s\n", labelStyle.Render("Branch:"), valueStyle.Render(ws.Branch))
	}

	p, err := prd.Read(wc.PRDPath)
	if err != nil {
		fmt.Fprintln(w)
		fmt.Fprintln(w, valueStyle.Render("No PRD"))
		return nil
	}

	passing, total := storyProgress(p)
	if passing == total {
		fmt.Fprintf(w, "%s %s\n", labelStyle.Render("Stories:"), passStyle.Render(fmt.Sprintf("%d/%d passing", passing, total)))
	} else {
		fmt.Fprintf(w, "%s %s\n", labelStyle.Render("Stories:"), failStyle.Render(fmt.Sprintf("%d/%d passing", passing, total)))
	}

	if len(p.IntegrationTests) > 0 {
		itPassing, itTotal := integrationTestProgress(p)
		if itPassing == itTotal {
			fmt.Fprintf(w, "%s %s\n", labelStyle.Render("Tests:"), passStyle.Render(fmt.Sprintf("%d/%d passing", itPassing, itTotal)))
		} else {
			fmt.Fprintf(w, "%s %s\n", labelStyle.Render("Tests:"), failStyle.Render(fmt.Sprintf("%d/%d passing", itPassing, itTotal)))
		}
	}

	return nil
}

func storyProgress(p *prd.PRD) (passing, total int) {
	total = len(p.UserStories)
	for _, s := range p.UserStories {
		if s.Passes {
			passing++
		}
	}
	return
}

func integrationTestProgress(p *prd.PRD) (passing, total int) {
	total = len(p.IntegrationTests)
	for _, t := range p.IntegrationTests {
		if t.Passes {
			passing++
		}
	}
	return
}
