package prompts

import (
	"bytes"
	"embed"
	"fmt"
	"os"
	"path/filepath"
	"text/template"

	"github.com/uesteibar/ralph/internal/prd"
)

//go:embed templates/*.md
var templateFS embed.FS

// TemplateFS returns the embedded template filesystem for external access
// (e.g. the eject command).
func TemplateFS() embed.FS {
	return templateFS
}

// TemplateNames lists all embedded template filenames (without the templates/ prefix).
var TemplateNames = []string{
	"loop_iteration.md",
	"qa_verification.md",
	"qa_fix.md",
	"chat_system.md",
	"prd_new.md",
	"rebase_conflict.md",
}

// LoopIterationData holds the context for rendering a loop iteration prompt.
type LoopIterationData struct {
	StoryID            string
	StoryTitle         string
	StoryDescription   string
	AcceptanceCriteria []string
	QualityChecks      []string
	ProgressPath       string
	PRDPath            string
}

// RenderLoopIteration renders the prompt for a single Ralph loop iteration.
// If overrideDir is non-empty and contains loop_iteration.md, that file is used
// instead of the embedded template.
func RenderLoopIteration(story *prd.Story, qualityChecks []string, progressPath, prdPath, overrideDir string) (string, error) {
	data := LoopIterationData{
		StoryID:            story.ID,
		StoryTitle:         story.Title,
		StoryDescription:   story.Description,
		AcceptanceCriteria: story.AcceptanceCriteria,
		QualityChecks:      qualityChecks,
		ProgressPath:       progressPath,
		PRDPath:            prdPath,
	}
	return render("templates/loop_iteration.md", data, overrideDir)
}

// PRDNewData holds the context for the interactive PRD creation prompt.
type PRDNewData struct {
	ProjectName     string
	PRDPath         string
	WorkspaceBranch string
}

// RenderPRDNew renders the prompt for interactive PRD creation.
func RenderPRDNew(data PRDNewData, overrideDir string) (string, error) {
	return render("templates/prd_new.md", data, overrideDir)
}

// ChatSystemData holds the context for the chat system prompt.
type ChatSystemData struct {
	ProjectName   string
	Config        string
	Progress      string
	RecentCommits string
	PRDContext    string
}

// RenderChatSystem renders the system prompt for a free-form chat session.
func RenderChatSystem(data ChatSystemData, overrideDir string) (string, error) {
	return render("templates/chat_system.md", data, overrideDir)
}

// RebaseConflictData holds the context for the rebase conflict resolution prompt.
type RebaseConflictData struct {
	PRDDescription string
	Stories        string
	Progress       string
	FeatureDiff    string
	BaseDiff       string
	ConflictFiles  string
}

// RenderRebaseConflict renders the prompt for rebase conflict resolution.
func RenderRebaseConflict(data RebaseConflictData, overrideDir string) (string, error) {
	return render("templates/rebase_conflict.md", data, overrideDir)
}

// QAVerificationData holds the context for the QA verification prompt.
type QAVerificationData struct {
	PRDPath       string
	ProgressPath  string
	QualityChecks []string
}

// RenderQAVerification renders the prompt for QA integration test verification.
func RenderQAVerification(data QAVerificationData, overrideDir string) (string, error) {
	return render("templates/qa_verification.md", data, overrideDir)
}

// QAFixData holds the context for the QA fix prompt.
type QAFixData struct {
	PRDPath       string
	ProgressPath  string
	QualityChecks []string
	FailedTests   []prd.IntegrationTest
}

// RenderQAFix renders the prompt for fixing integration test failures.
func RenderQAFix(data QAFixData, overrideDir string) (string, error) {
	return render("templates/qa_fix.md", data, overrideDir)
}

func render(name string, data any, overrideDir string) (string, error) {
	content, err := readTemplate(name, overrideDir)
	if err != nil {
		return "", err
	}

	tmpl, err := template.New(name).Parse(string(content))
	if err != nil {
		return "", fmt.Errorf("parsing template %s: %w", name, err)
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("executing template %s: %w", name, err)
	}

	return buf.String(), nil
}

// readTemplate returns the template content, preferring an override file on
// disk (overrideDir/<filename>) and falling back to the embedded version.
func readTemplate(name, overrideDir string) ([]byte, error) {
	// name is "templates/<filename>"; extract the base filename.
	filename := filepath.Base(name)

	if overrideDir != "" {
		overridePath := filepath.Join(overrideDir, filename)
		if content, err := os.ReadFile(overridePath); err == nil {
			return content, nil
		}
		// File missing in override dir is not an error — fall through to embedded.
	}

	content, err := templateFS.ReadFile(name)
	if err != nil {
		return nil, fmt.Errorf("reading template %s: %w", name, err)
	}
	return content, nil
}
