package ai

import (
	"bytes"
	"embed"
	"fmt"
	"os"
	"path/filepath"
	"text/template"
)

//go:embed templates/*.md
var templateFS embed.FS

// TemplateFS returns the embedded template filesystem for external access.
func TemplateFS() embed.FS {
	return templateFS
}

// TemplateNames lists all embedded template filenames (without the templates/ prefix).
var TemplateNames = []string{
	"refine_issue.md",
	"generate_prd.md",
	"pr_description.md",
	"address_feedback.md",
	"fix_checks.md",
}

// --- Data structs ---

// RefineIssueComment represents a single comment on a Linear issue.
type RefineIssueComment struct {
	Author    string
	CreatedAt string
	Body      string
}

// RefineIssueData holds the context for rendering the refine_issue prompt.
type RefineIssueData struct {
	Title       string
	Description string
	Comments    []RefineIssueComment
}

// GeneratePRDData holds the context for rendering the generate_prd prompt.
type GeneratePRDData struct {
	PlanText             string
	ProjectName          string
	FeatureOverview      string
	ArchitectureOverview string
	PRDPath              string
	BranchName           string
}

// PRDescriptionStory represents a story for the PR description prompt.
type PRDescriptionStory struct {
	ID    string
	Title string
}

// PRDescriptionData holds the context for rendering the pr_description prompt.
type PRDescriptionData struct {
	PRDSummary string
	Stories    []PRDescriptionStory
	DiffStats  string
}

// AddressFeedbackComment represents a single review comment.
type AddressFeedbackComment struct {
	Path   string
	Line   int
	Author string
	Body   string
}

// AddressFeedbackData holds the context for rendering the address_feedback prompt.
type AddressFeedbackData struct {
	Comments      []AddressFeedbackComment
	CodeContext   string
	QualityChecks []string
}

// FailedCheckRun represents a single failed CI check run.
type FailedCheckRun struct {
	Name       string
	Conclusion string
	Log        string
}

// FixChecksData holds the context for rendering the fix_checks prompt.
type FixChecksData struct {
	FailedChecks  []FailedCheckRun
	QualityChecks []string
}

// --- Render functions ---

// RenderRefineIssue renders the prompt for issue refinement.
// If overrideDir is non-empty and contains refine_issue.md, that file is used
// instead of the embedded template.
func RenderRefineIssue(data RefineIssueData, overrideDir string) (string, error) {
	return render("templates/refine_issue.md", data, overrideDir)
}

// RenderGeneratePRD renders the prompt for PRD generation.
func RenderGeneratePRD(data GeneratePRDData, overrideDir string) (string, error) {
	return render("templates/generate_prd.md", data, overrideDir)
}

// RenderPRDescription renders the prompt for PR title and body generation.
func RenderPRDescription(data PRDescriptionData, overrideDir string) (string, error) {
	return render("templates/pr_description.md", data, overrideDir)
}

// RenderAddressFeedback renders the prompt for addressing review feedback.
func RenderAddressFeedback(data AddressFeedbackData, overrideDir string) (string, error) {
	return render("templates/address_feedback.md", data, overrideDir)
}

// RenderFixChecks renders the prompt for fixing CI check failures.
func RenderFixChecks(data FixChecksData, overrideDir string) (string, error) {
	return render("templates/fix_checks.md", data, overrideDir)
}

// --- Internal rendering ---

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
	filename := filepath.Base(name)

	if overrideDir != "" {
		overridePath := filepath.Join(overrideDir, filename)
		if content, err := os.ReadFile(overridePath); err == nil {
			return content, nil
		}
	}

	content, err := templateFS.ReadFile(name)
	if err != nil {
		return nil, fmt.Errorf("reading template %s: %w", name, err)
	}
	return content, nil
}
