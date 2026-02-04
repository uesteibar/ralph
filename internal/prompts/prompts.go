package prompts

import (
	"bytes"
	"embed"
	"fmt"
	"text/template"

	"github.com/uesteibar/ralph/internal/prd"
)

//go:embed templates/*.md
var templateFS embed.FS

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
func RenderLoopIteration(story *prd.Story, qualityChecks []string, progressPath, prdPath string) (string, error) {
	data := LoopIterationData{
		StoryID:            story.ID,
		StoryTitle:         story.Title,
		StoryDescription:   story.Description,
		AcceptanceCriteria: story.AcceptanceCriteria,
		QualityChecks:      qualityChecks,
		ProgressPath:       progressPath,
		PRDPath:            prdPath,
	}
	return render("templates/loop_iteration.md", data)
}

// PRDNewData holds the context for the interactive PRD creation prompt.
type PRDNewData struct {
	ProjectName     string
	PRDPath         string
	WorkspaceBranch string
}

// RenderPRDNew renders the prompt for interactive PRD creation.
func RenderPRDNew(data PRDNewData) (string, error) {
	return render("templates/prd_new.md", data)
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
func RenderChatSystem(data ChatSystemData) (string, error) {
	return render("templates/chat_system.md", data)
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
func RenderRebaseConflict(data RebaseConflictData) (string, error) {
	return render("templates/rebase_conflict.md", data)
}

// QAVerificationData holds the context for the QA verification prompt.
type QAVerificationData struct {
	PRDPath       string
	ProgressPath  string
	QualityChecks []string
}

// RenderQAVerification renders the prompt for QA integration test verification.
func RenderQAVerification(data QAVerificationData) (string, error) {
	return render("templates/qa_verification.md", data)
}

// QAFixData holds the context for the QA fix prompt.
type QAFixData struct {
	PRDPath       string
	ProgressPath  string
	QualityChecks []string
	FailedTests   []prd.IntegrationTest
}

// RenderQAFix renders the prompt for fixing integration test failures.
func RenderQAFix(data QAFixData) (string, error) {
	return render("templates/qa_fix.md", data)
}

func render(name string, data any) (string, error) {
	content, err := templateFS.ReadFile(name)
	if err != nil {
		return "", fmt.Errorf("reading template %s: %w", name, err)
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
