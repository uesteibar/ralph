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
}

// RenderLoopIteration renders the prompt for a single Ralph loop iteration.
func RenderLoopIteration(story *prd.Story, qualityChecks []string, progressPath string) (string, error) {
	data := LoopIterationData{
		StoryID:            story.ID,
		StoryTitle:         story.Title,
		StoryDescription:   story.Description,
		AcceptanceCriteria: story.AcceptanceCriteria,
		QualityChecks:      qualityChecks,
		ProgressPath:       progressPath,
	}
	return render("templates/loop_iteration.md", data)
}

// PRDNewData holds the context for the interactive PRD creation prompt.
type PRDNewData struct {
	ProjectName string
}

// RenderPRDNew renders the prompt for interactive PRD creation.
func RenderPRDNew(projectName string) (string, error) {
	return render("templates/prd_new.md", PRDNewData{ProjectName: projectName})
}

// ChatSystemData holds the context for the chat system prompt.
type ChatSystemData struct {
	ProjectName   string
	Config        string
	Progress      string
	RecentCommits string
}

// RenderChatSystem renders the system prompt for a free-form chat session.
func RenderChatSystem(data ChatSystemData) (string, error) {
	return render("templates/chat_system.md", data)
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
