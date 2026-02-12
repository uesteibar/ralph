package events

import (
	"encoding/json"
	"fmt"
)

// Type discriminator values for JSON serialization.
const (
	typeToolUse         = "tool_use"
	typeAgentText       = "agent_text"
	typeInvocationDone  = "invocation_done"
	typeIterationStart  = "iteration_start"
	typeStoryStarted    = "story_started"
	typeQAPhaseStarted  = "qa_phase_started"
	typeUsageLimitWait  = "usage_limit_wait"
	typeLogMessage      = "log_message"
	typePRDRefresh      = "prd_refresh"
)

// envelope wraps an event with a type discriminator for JSON serialization.
type envelope struct {
	Type string          `json:"type"`
	Data json.RawMessage `json:"data,omitempty"`
}

// MarshalEvent serializes an Event to JSON with a "type" discriminator field.
func MarshalEvent(e Event) ([]byte, error) {
	var typeName string
	switch e.(type) {
	case ToolUse:
		typeName = typeToolUse
	case AgentText:
		typeName = typeAgentText
	case InvocationDone:
		typeName = typeInvocationDone
	case IterationStart:
		typeName = typeIterationStart
	case StoryStarted:
		typeName = typeStoryStarted
	case QAPhaseStarted:
		typeName = typeQAPhaseStarted
	case UsageLimitWait:
		typeName = typeUsageLimitWait
	case LogMessage:
		typeName = typeLogMessage
	case PRDRefresh:
		typeName = typePRDRefresh
	default:
		return nil, fmt.Errorf("unknown event type: %T", e)
	}

	data, err := json.Marshal(e)
	if err != nil {
		return nil, err
	}

	env := envelope{Type: typeName, Data: data}
	return json.Marshal(env)
}

// UnmarshalEvent deserializes an Event from JSON using the "type" discriminator field.
func UnmarshalEvent(b []byte) (Event, error) {
	var env envelope
	if err := json.Unmarshal(b, &env); err != nil {
		return nil, err
	}

	if env.Type == "" {
		return nil, fmt.Errorf("missing event type field")
	}

	switch env.Type {
	case typeToolUse:
		var e ToolUse
		if err := json.Unmarshal(env.Data, &e); err != nil {
			return nil, err
		}
		return e, nil
	case typeAgentText:
		var e AgentText
		if err := json.Unmarshal(env.Data, &e); err != nil {
			return nil, err
		}
		return e, nil
	case typeInvocationDone:
		var e InvocationDone
		if err := json.Unmarshal(env.Data, &e); err != nil {
			return nil, err
		}
		return e, nil
	case typeIterationStart:
		var e IterationStart
		if err := json.Unmarshal(env.Data, &e); err != nil {
			return nil, err
		}
		return e, nil
	case typeStoryStarted:
		var e StoryStarted
		if err := json.Unmarshal(env.Data, &e); err != nil {
			return nil, err
		}
		return e, nil
	case typeQAPhaseStarted:
		var e QAPhaseStarted
		if err := json.Unmarshal(env.Data, &e); err != nil {
			return nil, err
		}
		return e, nil
	case typeUsageLimitWait:
		var e UsageLimitWait
		if err := json.Unmarshal(env.Data, &e); err != nil {
			return nil, err
		}
		return e, nil
	case typeLogMessage:
		var e LogMessage
		if err := json.Unmarshal(env.Data, &e); err != nil {
			return nil, err
		}
		return e, nil
	case typePRDRefresh:
		return PRDRefresh{}, nil
	default:
		return nil, fmt.Errorf("unknown event type: %q", env.Type)
	}
}
