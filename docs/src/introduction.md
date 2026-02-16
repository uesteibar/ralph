# Ralph

An autonomous coding agent loop. Ralph takes a feature description, breaks it
into right-sized user stories, and implements them one by one — running quality
checks and committing after each step — until the feature is done.

You work from your terminal. Ralph works in an isolated workspace.

This repository also contains **AutoRalph** — an autonomous daemon that watches
Linear for assigned issues and drives them through a complete development
lifecycle (refine, plan, build, open PR, address review feedback) using Ralph
under the hood.

## Quick Links

- **[Getting Started](ralph/getting-started.md)** — Prerequisites, installation, and a quick-start workflow
- **[Workflow](ralph/workflow.md)** — The Ralph development loop: init, new, run, done
- **[Commands](ralph/commands.md)** — CLI command reference
- **[Configuration](ralph/configuration.md)** — ralph.yaml reference and prompt customization
- **[Setup](ralph/setup.md)** — Setting up Ralph in an existing codebase
- **[Architecture](ralph/architecture.md)** — Execution loop and workspace isolation

### AutoRalph

- **[Overview](autoralph/overview.md)** — What AutoRalph is and its capabilities
- **[Lifecycle](autoralph/lifecycle.md)** — Issue lifecycle and state transitions
- **[Abilities](autoralph/abilities.md)** — Refine, build, rebase, feedback, fix checks, complete
- **[Configuration](autoralph/configuration.md)** — Credentials, project configs, and options
- **[Security](autoralph/security.md)** — Trusted users, GitHub App auth, credential isolation
- **[Dashboard](autoralph/dashboard.md)** — Web dashboard, REST API, and WebSocket protocol
