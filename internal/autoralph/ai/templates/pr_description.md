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
## Diff Stats

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
<1-3 bullet points describing what changed and why>

## Changes
<Bullet list of key changes, grouped by area>

## Testing
<How the changes were tested — unit tests, integration tests, manual verification>
```

## Output Format

Output the title on the first line, followed by a blank line, followed by the body. Example:

```
feat(auth): add user login flow

## Summary
- Add user login with email/password authentication
- Include session management with JWT tokens

## Changes
- Added login API endpoint at POST /api/auth/login
- Created JWT token generation and validation
- Added login form component with validation

## Testing
- Unit tests for auth service and JWT utils
- Integration test for login flow end-to-end
```

Output ONLY the title and body. No extra explanation.
