package commands

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/uesteibar/ralph/internal/config"
	"github.com/uesteibar/ralph/internal/prd"
)

// AddProjectConfigFlag adds the --project-config flag to a FlagSet.
func AddProjectConfigFlag(fs *flag.FlagSet) *string {
	return fs.String("project-config", "", "Path to project config YAML (default: discover .ralph/ralph.yaml)")
}

// ResolveConfig loads the project config from the explicit flag value or by discovery.
func ResolveConfig(flagValue string) (*config.Config, error) {
	return config.Resolve(flagValue)
}

// RequirePositionalInt extracts a required integer positional argument.
func RequirePositionalInt(args []string, name string) (int, error) {
	if len(args) == 0 {
		return 0, fmt.Errorf("missing required argument: <%s>", name)
	}
	n, err := strconv.Atoi(args[0])
	if err != nil {
		return 0, fmt.Errorf("invalid %s: %q (must be a number)", name, args[0])
	}
	return n, nil
}

// archiveStagedPRD moves the current .ralph/state/prd.json to the archive
// directory if it exists, using the branch name and current date.
func archiveStagedPRD(cfg *config.Config) {
	stagedPath := cfg.StatePRDPath()
	data, err := os.ReadFile(stagedPath)
	if err != nil {
		return
	}

	var p prd.PRD
	if err := json.Unmarshal(data, &p); err != nil || p.BranchName == "" {
		return
	}

	sanitized := sanitizeBranchForArchive(p.BranchName)
	archiveDir := filepath.Join(cfg.StateArchiveDir(),
		time.Now().Format("2006-01-02")+"-"+sanitized)
	if err := os.MkdirAll(archiveDir, 0755); err != nil {
		log.Printf("[ralph] warning: could not create archive dir: %v", err)
		return
	}

	if err := os.WriteFile(filepath.Join(archiveDir, "prd.json"), data, 0644); err != nil {
		log.Printf("[ralph] warning: could not archive PRD: %v", err)
		return
	}

	os.Remove(stagedPath)
	log.Printf("[ralph] archived previous PRD to %s", archiveDir)
}

func sanitizeBranchForArchive(branch string) string {
	result := make([]byte, 0, len(branch))
	for i := 0; i < len(branch); i++ {
		c := branch[i]
		if c == '/' {
			result = append(result, '_', '_')
		} else {
			result = append(result, c)
		}
	}
	return string(result)
}
