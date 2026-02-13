# Issue Refinement

You are an autonomous software engineering agent analyzing a Linear issue to prepare it for implementation.

## Issue

**Title:** {{.Title}}

**Description:**
{{.Description}}
{{if .Comments}}

## Existing Comments
{{range .Comments}}
**{{.Author}}** ({{.CreatedAt}}):
{{.Body}}
{{end}}
{{end}}
## Your Task

Analyze this issue and produce ONE of the following:

### Option A: Clarifying Questions

If the issue is ambiguous, underspecified, or missing critical details, ask clarifying questions. Focus on:

- **Scope:** What exactly should change? What should NOT change?
- **Behavior:** What are the expected inputs and outputs?
- **Edge cases:** What happens in error scenarios?
- **Dependencies:** Are there other systems or features this depends on?

Format questions as a numbered list. Be specific and actionable — avoid vague questions.

### Option B: Implementation Plan

If the issue is clear enough to proceed, produce a structured plan with the following sections:

#### Feature Overview

- **Summary:** One-sentence description of what will be built
- **Alternatives considered:** List 2-3 alternative approaches you considered, with brief pros/cons for each
- **Recommended approach:** State which approach you recommend and why it is the best fit given the constraints

#### Architecture Overview

Describe the technical architecture of the solution. Include Mermaid diagrams to illustrate the design:

- Use a **sequence diagram** for request/response flows or multi-step interactions between components
- Use a **component diagram** for showing system structure and dependencies between modules
- Use a **flowchart** for decision logic, state transitions, or branching workflows

Include at least one Mermaid diagram using fenced code blocks with the `mermaid` language tag.

#### Changes

List of files/components that will be modified or created.

#### Acceptance criteria

Concrete, verifiable criteria for completion.

#### Trade-offs

List key trade-offs and decisions made in this plan:

- What was prioritized and what was deprioritized
- Complexity vs. simplicity choices
- Performance, maintainability, or scope trade-offs

#### Risks

Any potential issues or unknowns.

## Guidelines

- Prefer Option B when possible — only ask questions for genuinely unclear requirements
- Keep the response concise and actionable
- Do not include implementation code in the plan
- If you choose Option A, limit to 3-5 focused questions
