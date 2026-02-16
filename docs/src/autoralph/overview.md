# Overview

AutoRalph is an autonomous agent that watches Linear for assigned issues and
drives them through a complete development lifecycle — from refinement and
planning to building, opening pull requests, and addressing review feedback —
without human intervention.

AutoRalph wraps Ralph's execution loop in a long-running daemon. It polls
Linear for new issues, uses AI to refine requirements, creates workspaces,
invokes Ralph to build features, opens GitHub pull requests, responds to code
review feedback, and marks issues as done when PRs are merged.

## How It Works

```
   Linear                     AutoRalph                         GitHub
   ------                     ---------                         ------
  Assign issue  ------>  Poll & ingest (QUEUED)
                         AI refinement (REFINING)
                         Post questions to Linear
  User approves ------>  Detect approval (APPROVED)
                         Create workspace + PRD (BUILDING)
                         Run Ralph loop (stories, QA)
                         Push branch                  ------>  Create PR
                         Wait for review              <------  Review comments
                         Address feedback (ADDRESSING_FEEDBACK)
                         Push fixes                   ------>  Updated PR
                         Detect merge                 <------  PR merged
                         Cleanup workspace (COMPLETED)
  Move to Done  <------  Update Linear state
```

AutoRalph bridges three systems — Linear (issue tracking), your local
codebase (via Ralph), and GitHub (code review and merging) — into a
continuous autonomous workflow.

## Prerequisites

- **Go 1.25+** (build from source)
- **[Claude Code](https://docs.anthropic.com/en/docs/claude-code)** (`claude`
  CLI) installed and authenticated
- **git** (workspace operations use worktrees)
- **Node.js 18+** (building the web dashboard)
- **A Linear account** with an API key
- **A GitHub account** with a personal access token or a GitHub App

## Installation

### One-liner

```bash
curl -fsSL https://raw.githubusercontent.com/uesteibar/ralph/main/install-autoralph.sh | sh
```

This detects your OS and architecture, downloads the latest release from
GitHub, verifies the SHA256 checksum, and installs to `/usr/local/bin/`.

### From source

```bash
git clone https://github.com/uesteibar/ralph.git
cd ralph
just web-build    # Build the React dashboard
just autoralph    # Build the Go binary
```

### Verify

```bash
autoralph --version
```

## What's Next

- [Lifecycle](lifecycle.md) — the full issue state machine
- [Abilities](abilities.md) — what AutoRalph can do at each stage
- [Configuration](configuration.md) — setting up credentials and projects
- [Security](security.md) — authentication and access controls
- [Dashboard](dashboard.md) — the web UI and API
