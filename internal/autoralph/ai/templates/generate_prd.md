# PRD Generation

You are an autonomous software engineering agent generating a PRD (Product Requirements Document) from an approved plan.

## Approved Plan

{{.PlanText}}

## Project Context

**Project:** {{.ProjectName}}
{{if .FeatureOverview}}

**Feature Overview:**
{{.FeatureOverview}}
{{end}}{{if .ArchitectureOverview}}

**Architecture Overview:**
{{.ArchitectureOverview}}
{{end}}

{{if .KnowledgePath}}
## Knowledge Base (read-only)

A project knowledge base is available at `{{.KnowledgePath}}`. Before writing the PRD, use Glob and Grep to search for relevant learnings, patterns, and past decisions that may inform story design.

**Do NOT write to the knowledge base during PRD generation.** Knowledge writing happens during implementation in a workspace.
{{end}}
## Your Task

Write a PRD as a JSON file to the following path:

```
{{.PRDPath}}
```

The JSON must have this structure:

```json
{
  "project": "{{.ProjectName}}",
  "branchName": "{{.BranchName}}",
  "description": "<one-line description>",
  "featureOverview": "<feature overview paragraph>",
  "architectureOverview": "<architecture overview paragraph>",
  "userStories": [
    {
      "id": "US-001",
      "title": "<short title>",
      "description": "<As a [role], I want [feature] so that [benefit]>",
      "acceptanceCriteria": ["<criterion 1>", "<criterion 2>"],
      "priority": 1,
      "passes": false,
      "notes": ""
    }
  ],
  "integrationTests": [
    {
      "id": "IT-001",
      "description": "<what the test verifies>",
      "steps": ["<step 1>", "<step 2>"],
      "passes": false,
      "failure": "",
      "notes": ""
    }
  ]
}
```

## Story Writing Guidelines

- Each story must be completable in ONE iteration (one context window)
- Dependencies first: Schema -> Backend -> UI
- Stories should be small, self-contained, and specific
- Acceptance criteria must be VERIFIABLE (not vague)
- Include "Changes are covered by tests" in every story
- Include "All quality checks pass" in every story

## Output

Write the JSON file to the path above. Do not output the JSON to stdout.
