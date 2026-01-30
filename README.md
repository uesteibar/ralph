# Ralph Orchestrator (WIP)

Central Ralph loop + automation engine.

Goals:
- Single Go CLI that owns the Ralph loop (PRD → code → checks → PRs)
- Per-project YAML config (paths, quality checks, branch rules)
- Central prompts + PRD scripts (no copy-paste into each repo)
- Supports both automation (cron/ralphctl) and local interactive usage

## CLI (planned)

From anywhere (automation mode):

```bash
ralph issue --project-config /path/to/ralph.<project>.yaml --issue <n>
ralph review --project-config /path/to/ralph.<project>.yaml --pr <n>
ralph sync-branch --project-config /path/to/ralph.<project>.yaml --pr <n>
```

From a repo with `.ralph/ralph.yaml` (local mode):

```bash
# Set up defaults
ralph init

# Run a full loop for an issue or PR
ralph issue 14
ralph review 15
ralph sync-branch 15

# Interactive PRD flows
ralph prd new
ralph prd from-issue 14

# Free-form interactive coding
ralph chat
```

This repo is meant to be developed with Claude Opus as the coding agent.
The existing `uesteibar/ralph-starter-kit` serves as prior art for prompts
and rough behaviour, but the Ralph loop lives here, not in each project.
