package events

import (
	"testing"
	"time"
)

func TestMarshalUnmarshal_RoundTrip(t *testing.T) {
	resetAt := time.Date(2026, 2, 5, 15, 30, 0, 0, time.UTC)

	tests := []struct {
		name  string
		event Event
		check func(t *testing.T, got Event)
	}{
		{
			name:  "ToolUse",
			event: ToolUse{Name: "Read", Detail: "./file.go", WorkDir: "/tmp/work"},
			check: func(t *testing.T, got Event) {
				e := got.(ToolUse)
				if e.Name != "Read" || e.Detail != "./file.go" || e.WorkDir != "/tmp/work" {
					t.Errorf("ToolUse mismatch: %+v", e)
				}
			},
		},
		{
			name:  "AgentText",
			event: AgentText{Text: "Hello\nWorld"},
			check: func(t *testing.T, got Event) {
				e := got.(AgentText)
				if e.Text != "Hello\nWorld" {
					t.Errorf("AgentText mismatch: %+v", e)
				}
			},
		},
		{
			name:  "InvocationDone",
			event: InvocationDone{NumTurns: 5, DurationMS: 12000},
			check: func(t *testing.T, got Event) {
				e := got.(InvocationDone)
				if e.NumTurns != 5 || e.DurationMS != 12000 {
					t.Errorf("InvocationDone mismatch: %+v", e)
				}
			},
		},
		{
			name:  "IterationStart",
			event: IterationStart{Iteration: 3, MaxIterations: 20},
			check: func(t *testing.T, got Event) {
				e := got.(IterationStart)
				if e.Iteration != 3 || e.MaxIterations != 20 {
					t.Errorf("IterationStart mismatch: %+v", e)
				}
			},
		},
		{
			name:  "StoryStarted",
			event: StoryStarted{StoryID: "US-001", Title: "Build auth"},
			check: func(t *testing.T, got Event) {
				e := got.(StoryStarted)
				if e.StoryID != "US-001" || e.Title != "Build auth" {
					t.Errorf("StoryStarted mismatch: %+v", e)
				}
			},
		},
		{
			name:  "QAPhaseStarted",
			event: QAPhaseStarted{Phase: "verification"},
			check: func(t *testing.T, got Event) {
				e := got.(QAPhaseStarted)
				if e.Phase != "verification" {
					t.Errorf("QAPhaseStarted mismatch: %+v", e)
				}
			},
		},
		{
			name:  "UsageLimitWait",
			event: UsageLimitWait{WaitDuration: 30 * time.Minute, ResetAt: resetAt},
			check: func(t *testing.T, got Event) {
				e := got.(UsageLimitWait)
				if e.WaitDuration != 30*time.Minute {
					t.Errorf("WaitDuration mismatch: got %v", e.WaitDuration)
				}
				if !e.ResetAt.Equal(resetAt) {
					t.Errorf("ResetAt mismatch: got %v", e.ResetAt)
				}
			},
		},
		{
			name:  "LogMessage",
			event: LogMessage{Level: "warning", Message: "something happened"},
			check: func(t *testing.T, got Event) {
				e := got.(LogMessage)
				if e.Level != "warning" || e.Message != "something happened" {
					t.Errorf("LogMessage mismatch: %+v", e)
				}
			},
		},
		{
			name:  "PRDRefresh",
			event: PRDRefresh{},
			check: func(t *testing.T, got Event) {
				if _, ok := got.(PRDRefresh); !ok {
					t.Errorf("expected PRDRefresh, got %T", got)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := MarshalEvent(tt.event)
			if err != nil {
				t.Fatalf("MarshalEvent: %v", err)
			}

			got, err := UnmarshalEvent(data)
			if err != nil {
				t.Fatalf("UnmarshalEvent: %v", err)
			}

			tt.check(t, got)
		})
	}
}

func TestMarshalEvent_ContainsTypeField(t *testing.T) {
	data, err := MarshalEvent(ToolUse{Name: "Read"})
	if err != nil {
		t.Fatalf("MarshalEvent: %v", err)
	}

	s := string(data)
	if !contains(s, `"type"`) {
		t.Errorf("expected type field in JSON, got %s", s)
	}
	if !contains(s, `"tool_use"`) {
		t.Errorf("expected type value 'tool_use', got %s", s)
	}
}

func TestUnmarshalEvent_UnknownType(t *testing.T) {
	data := []byte(`{"type":"unknown_event"}`)
	_, err := UnmarshalEvent(data)
	if err == nil {
		t.Fatal("expected error for unknown type")
	}
}

func TestUnmarshalEvent_InvalidJSON(t *testing.T) {
	_, err := UnmarshalEvent([]byte(`not json`))
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestUnmarshalEvent_MissingType(t *testing.T) {
	_, err := UnmarshalEvent([]byte(`{"name":"Read"}`))
	if err == nil {
		t.Fatal("expected error for missing type field")
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && searchString(s, substr)
}

func searchString(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
