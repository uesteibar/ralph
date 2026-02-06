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
	Name    string
	Detail  string
	WorkDir string
}

func (ToolUse) eventTag() {}

// AgentText is emitted when Claude produces text output.
type AgentText struct {
	Text string
}

func (AgentText) eventTag() {}

// InvocationDone is emitted when a Claude invocation completes.
type InvocationDone struct {
	NumTurns   int
	DurationMS int
}

func (InvocationDone) eventTag() {}

// IterationStart is emitted at the beginning of each loop iteration.
type IterationStart struct {
	Iteration     int
	MaxIterations int
}

func (IterationStart) eventTag() {}

// StoryStarted is emitted when the loop begins working on a user story.
type StoryStarted struct {
	StoryID string
	Title   string
}

func (StoryStarted) eventTag() {}

// QAPhaseStarted is emitted when the QA verification or fix phase begins.
type QAPhaseStarted struct {
	Phase string // "verification" or "fix"
}

func (QAPhaseStarted) eventTag() {}

// UsageLimitWait is emitted when a usage limit is hit and the loop is waiting.
type UsageLimitWait struct {
	WaitDuration time.Duration
	ResetAt      time.Time
}

func (UsageLimitWait) eventTag() {}

// PRDRefresh is emitted after events that may have changed the PRD on disk
// (e.g. after each invocation or iteration). The TUI uses this signal to
// re-read prd.json and update the story/test list.
type PRDRefresh struct{}

func (PRDRefresh) eventTag() {}
