package orchestrator

import (
	"fmt"

	"github.com/uesteibar/ralph/internal/autoralph/db"
)

// IssueState represents a state in the issue lifecycle.
type IssueState string

const (
	StateQueued             IssueState = "queued"
	StateRefining           IssueState = "refining"
	StateWaitingApproval    IssueState = "waiting_approval"
	StateApproved           IssueState = "approved"
	StateBuilding           IssueState = "building"
	StateInReview           IssueState = "in_review"
	StateAddressingFeedback IssueState = "addressing_feedback"
	StateFixingChecks       IssueState = "fixing_checks"
	StateCompleted          IssueState = "completed"
	StateFailed             IssueState = "failed"
	StatePaused             IssueState = "paused"
)

var validStates = map[IssueState]bool{
	StateQueued:             true,
	StateRefining:           true,
	StateWaitingApproval:    true,
	StateApproved:           true,
	StateBuilding:           true,
	StateInReview:           true,
	StateAddressingFeedback: true,
	StateFixingChecks:       true,
	StateCompleted:          true,
	StateFailed:             true,
	StatePaused:             true,
}

// ValidState returns true if s is a recognized IssueState.
func ValidState(s IssueState) bool {
	return validStates[s]
}

// ConditionFunc evaluates whether a transition should fire for the given issue.
type ConditionFunc func(issue db.Issue) bool

// ActionFunc performs the side-effect of a transition (e.g. call AI, post comment).
// It receives the issue and the database for any writes it needs to make.
// Actions run outside the state-transition transaction to avoid holding a
// write lock during long-running operations like AI invocations.
type ActionFunc func(issue db.Issue, database *db.DB) error

// Transition defines a valid state change in the issue lifecycle.
type Transition struct {
	From      IssueState
	To        IssueState
	Condition ConditionFunc
	Action    ActionFunc
}

// StateMachine holds registered transitions and evaluates/executes them.
type StateMachine struct {
	transitions []Transition
	database    *db.DB
}

// New creates a StateMachine backed by the given database.
func New(database *db.DB) *StateMachine {
	return &StateMachine{database: database}
}

// Register adds a transition to the state machine. It returns an error if
// From or To is not a valid IssueState.
func (sm *StateMachine) Register(t Transition) error {
	if !ValidState(t.From) {
		return fmt.Errorf("invalid from state: %q", t.From)
	}
	if !ValidState(t.To) {
		return fmt.Errorf("invalid to state: %q", t.To)
	}
	sm.transitions = append(sm.transitions, t)
	return nil
}

// Evaluate checks all registered transitions for the given issue and returns
// the first one whose From state matches and whose Condition returns true.
// If no transition matches, it returns (Transition{}, false).
func (sm *StateMachine) Evaluate(issue db.Issue) (Transition, bool) {
	current := IssueState(issue.State)
	for _, t := range sm.transitions {
		if t.From != current {
			continue
		}
		if t.Condition != nil && !t.Condition(issue) {
			continue
		}
		return t, true
	}
	return Transition{}, false
}

// Execute runs a transition: calls the Action (if any) outside any
// transaction, then updates the issue state and logs activity in a short
// transaction. This avoids holding a write lock during long-running
// operations like AI invocations.
func (sm *StateMachine) Execute(t Transition, issue db.Issue) error {
	if IssueState(issue.State) != t.From {
		return fmt.Errorf("issue %s is in state %q, expected %q", issue.ID, issue.State, t.From)
	}

	// Run the action outside any transaction so long-running operations
	// (AI calls, API calls) don't hold a SQLite write lock.
	if t.Action != nil {
		if err := t.Action(issue, sm.database); err != nil {
			return fmt.Errorf("running transition action: %w", err)
		}
	}

	// Short transaction for the state update + activity log.
	return sm.database.Tx(func(tx *db.Tx) error {
		// Re-read the issue to preserve any fields the action modified
		// (e.g. LastCommentID, WorkspaceName). Without this, the original
		// value-copy of issue would overwrite those changes.
		current, err := tx.GetIssue(issue.ID)
		if err != nil {
			return fmt.Errorf("re-reading issue after action: %w", err)
		}

		current.State = string(t.To)
		if err := tx.UpdateIssue(current); err != nil {
			return fmt.Errorf("updating issue state: %w", err)
		}

		if err := tx.LogActivity(
			issue.ID,
			"state_change",
			string(t.From),
			string(t.To),
			fmt.Sprintf("Transitioned from %s to %s", t.From, t.To),
		); err != nil {
			return fmt.Errorf("logging activity: %w", err)
		}

		return nil
	})
}
