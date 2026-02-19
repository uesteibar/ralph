package events

import "time"

// Event is the interface satisfied by all event types emitted during agent execution.
type Event interface {
	eventTag()
}

// EventHandler processes events emitted by the agent loop and Claude invocations.
type EventHandler interface {
	Handle(event Event)
}

// ToolUse is emitted when Claude invokes a tool (Read, Edit, Bash, etc.).
type ToolUse struct {
	Name    string `json:"name"`
	Detail  string `json:"detail"`
	WorkDir string `json:"workDir"`
}

func (ToolUse) eventTag() {}

// AgentText is emitted when Claude produces text output.
type AgentText struct {
	Text string `json:"text"`
}

func (AgentText) eventTag() {}

// InvocationDone is emitted when a Claude invocation completes.
type InvocationDone struct {
	NumTurns     int `json:"numTurns"`
	DurationMS   int `json:"durationMs"`
	InputTokens  int `json:"inputTokens"`
	OutputTokens int `json:"outputTokens"`
}

func (InvocationDone) eventTag() {}

// IterationStart is emitted at the beginning of each loop iteration.
type IterationStart struct {
	Iteration     int `json:"iteration"`
	MaxIterations int `json:"maxIterations"`
}

func (IterationStart) eventTag() {}

// StoryStarted is emitted when the loop begins working on a user story.
type StoryStarted struct {
	StoryID string `json:"storyId"`
	Title   string `json:"title"`
}

func (StoryStarted) eventTag() {}

// QAPhaseStarted is emitted when the QA verification or fix phase begins.
type QAPhaseStarted struct {
	Phase string `json:"phase"` // "verification" or "fix"
}

func (QAPhaseStarted) eventTag() {}

// UsageLimitWait is emitted when a usage limit is hit and the loop is waiting.
type UsageLimitWait struct {
	WaitDuration time.Duration `json:"waitDuration"`
	ResetAt      time.Time     `json:"resetAt"`
}

func (UsageLimitWait) eventTag() {}

// LogMessage is emitted for diagnostic messages that were previously written
// to stderr. This allows library callers to capture loop status messages
// without relying on stderr.
type LogMessage struct {
	Level   string `json:"level"`   // "info" or "warning"
	Message string `json:"message"`
}

func (LogMessage) eventTag() {}

// PRDRefresh is emitted after events that may have changed the PRD on disk
// (e.g. after each invocation or iteration). The TUI uses this signal to
// re-read prd.json and update the story/test list.
type PRDRefresh struct{}

func (PRDRefresh) eventTag() {}
