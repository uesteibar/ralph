package commands

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/uesteibar/ralph/internal/prompts"
)

// Eject copies all embedded prompt templates to .ralph/prompts/ for customization.
func Eject(args []string) error {
	fs := flag.NewFlagSet("eject", flag.ExitOnError)
	configFlag := AddProjectConfigFlag(fs)
	if err := fs.Parse(args); err != nil {
		return err
	}

	cfg, err := ResolveConfig(*configFlag)
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	if err := ejectTemplates(cfg.Repo.Path); err != nil {
		return err
	}

	fmt.Fprintf(os.Stderr, "Ejected %d prompt templates to .ralph/prompts/\n", len(prompts.TemplateNames))
	return nil
}

// ejectTemplates writes all embedded prompt templates to <repoPath>/.ralph/prompts/.
func ejectTemplates(repoPath string) error {
	ralphDir := filepath.Join(repoPath, ".ralph")
	if _, err := os.Stat(ralphDir); os.IsNotExist(err) {
		return fmt.Errorf(".ralph/ directory not found — run ralph init first")
	}

	promptsDir := filepath.Join(ralphDir, "prompts")
	if _, err := os.Stat(promptsDir); err == nil {
		return fmt.Errorf("prompts already ejected at .ralph/prompts/ — remove the directory to re-eject")
	}

	if err := os.MkdirAll(promptsDir, 0755); err != nil {
		return fmt.Errorf("creating .ralph/prompts/: %w", err)
	}

	tFS := prompts.TemplateFS()
	for _, name := range prompts.TemplateNames {
		content, err := tFS.ReadFile("templates/" + name)
		if err != nil {
			return fmt.Errorf("reading embedded template %s: %w", name, err)
		}

		if err := os.WriteFile(filepath.Join(promptsDir, name), content, 0644); err != nil {
			return fmt.Errorf("writing template %s: %w", name, err)
		}
	}

	return nil
}
