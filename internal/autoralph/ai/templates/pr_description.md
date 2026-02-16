# Pull Request Description

You are an autonomous software engineering agent writing a pull request description.

## PRD Summary

{{.PRDSummary}}
{{if .Stories}}

## Stories Implemented
{{range .Stories}}
- **{{.ID}}:** {{.Title}}
{{end}}
{{end}}
## Diff Stats (background context only)

The following diff stats are provided as background context for scope awareness. Do not list individual file changes in your output.

```
{{.DiffStats}}
```

## Your Task

Generate a pull request title and body.

### Title

- Short (under 70 characters)
- Use imperative mood: "Add feature" not "Added feature"
- Include the scope if relevant: "feat(auth): add login flow"

### Body

Use this format:

```
## Summary
<1-3 sentences describing what this feature does and why it matters>

## Technical Architecture
<Explain how the solution is structured, how the key components interact, and where the main changes live in the codebase>

## Key Design Decisions
<Describe the important choices made during implementation and alternatives that were considered>

## Testing
<How the changes were tested — unit tests, integration tests, manual verification>
```

## Output Format

Output the title on the first line, followed by a blank line, followed by the body. Example:

```
feat(auth): add user login flow

## Summary
Adds user login with email/password authentication and session management,
enabling users to securely access their accounts.

## Technical Architecture
The login flow is implemented as a three-layer stack: a React form component
submits credentials to a new POST /api/auth/login endpoint, which delegates
to an AuthService that validates credentials against the user store and issues
JWT tokens. Session state is managed client-side via an AuthContext provider.

## Key Design Decisions
- Chose JWT over server-side sessions to keep the API stateless and simplify
  horizontal scaling. The trade-off is token revocation requires a deny-list.
- Placed validation logic in a dedicated AuthService rather than the handler
  to keep the HTTP layer thin and improve testability.

## Testing
- Unit tests for AuthService credential validation and JWT generation
- Integration test for the full login flow end-to-end
- Manual verification of error states (wrong password, locked account)
```

Output ONLY the title and body. No extra explanation.
