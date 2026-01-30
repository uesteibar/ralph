package commands

import (
	"flag"
	"fmt"
)

// Validate loads and validates the project config, printing any issues found.
func Validate(args []string) error {
	fs := flag.NewFlagSet("validate", flag.ExitOnError)
	configFlag := AddProjectConfigFlag(fs)
	if err := fs.Parse(args); err != nil {
		return err
	}

	cfg, err := ResolveConfig(*configFlag)
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	issues := cfg.Validate()
	if len(issues) == 0 {
		fmt.Println("Config is valid.")
		return nil
	}

	fmt.Printf("Found %d issue(s):\n", len(issues))
	for _, issue := range issues {
		fmt.Printf("  - %s\n", issue)
	}
	return fmt.Errorf("config has %d issue(s)", len(issues))
}
