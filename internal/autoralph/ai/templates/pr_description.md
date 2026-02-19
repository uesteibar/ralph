# Pull Request Description

You are an autonomous software engineering agent writing a pull request description.

{{if .LinearIssueIdentifier}}
## Linear Issue

[{{.LinearIssueIdentifier}}](https://linear.app/issue/{{.LinearIssueIdentifier}})
{{end}}
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

Use this format. You may omit any section that does not add value for this specific PR.

```
## Overall Approach
<High-level summary of what was done and why, in 2-4 sentences>

## Architecture Diagram (ASCII)
<ASCII diagram showing how the key components relate to each other>

## Flow Chart (ASCII)
<ASCII flow chart showing the main execution path or data flow>

## Technical Implications
<Notable consequences: performance, security, backward compatibility, migration needs, etc.>

## Other Notes
<Anything else a reviewer should know: trade-offs, follow-up work, open questions, etc.>
```

## Output Format

Output the title on the first line, followed by a blank line, followed by the body. Example:

```
feat(auth): add user login flow

## Overall Approach
Adds user login with email/password authentication and session management.
The implementation uses JWT tokens for stateless auth and a dedicated
AuthService to keep the HTTP layer thin.

## Architecture Diagram (ASCII)
┌──────────┐     ┌───────────┐     ┌─────────────┐
│  React   │────>│  POST     │────>│ AuthService  │
│  Form    │     │ /api/login│     │ (validate +  │
└──────────┘     └───────────┘     │  issue JWT)  │
                                   └─────────────┘

## Flow Chart (ASCII)
User submits form
       │
       v
POST /api/auth/login
       │
       v
AuthService.validate(credentials)
       │
  ┌────┴────┐
  │ valid?  │
  ├─yes─────┤
  │ issue   │──> 200 + JWT token
  │ JWT     │
  ├─no──────┤
  │ reject  │──> 401 Unauthorized
  └─────────┘

## Technical Implications
- JWT tokens are stateless, simplifying horizontal scaling but requiring
  a deny-list for token revocation.
- The AuthContext provider manages client-side session state.

## Other Notes
- Follow-up: add refresh token rotation.
```

Output ONLY the title and body. No extra explanation.
