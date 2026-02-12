Take the plan we have discussed and agreed upon in this conversation and structure it into a PRD JSON file.

## Output Format

Write to the PRD path specified in the system prompt. If none specified write to `.ralph/state/prd.json`.

Use this exact schema:

```json
{
  "project": "<project name from .ralph/ralph.yaml>",
  "branchName": "ralph/<feature-name-kebab-case>",
  "description": "<one-line description of the feature>",
  "userStories": [
    {
      "id": "US-001",
      "title": "<short story title>",
      "description": "As a <user>, I want <feature> so that <benefit>",
      "acceptanceCriteria": [
        "Specific verifiable criterion",
        "All quality checks pass"
      ],
      "priority": 1,
      "passes": false,
      "notes": ""
    }
  ],
  "integrationTests": [
    {
      "id": "IT-001",
      "description": "<what this test verifies at a feature level>",
      "steps": [
        "Step 1: <action to perform>",
        "Step 2: <expected result to verify>"
      ],
      "passes": false,
      "failure": "",
      "notes": ""
    }
  ]
}
```

## Story Rules

- Each story must be completable in ONE context window (one Ralph iteration)
- Order by dependency: schema/data first, then backend logic, then UI
- Acceptance criteria must be specific and verifiable
- Include "All quality checks pass" in every story's acceptance criteria
- All stories start with `passes: false`
- Priority determines execution order (1 = first)

## Integration Test Rules

- Include integration tests agreed upon during PRD discussion
- Each test has: id (IT-xxx), description, steps (array), passes, failure, notes
- All integration tests start with `passes: false`
- `failure` field records why a test failed (empty string if not yet run)
- `notes` field captures observations or additional context

## After Writing

1. Read back the file to confirm it is valid JSON
2. Tell the user the PRD is ready and suggest: `ralph run`
