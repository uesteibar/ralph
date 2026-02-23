# Abilities

AutoRalph performs different actions at each stage of the issue lifecycle. Each
ability is implemented as a self-contained package under
`internal/autoralph/` with its own interfaces, tests, and error handling.

## Refine

**Transition**: `QUEUED` → `REFINING`

The refine ability reads an issue's title and description, uses AI to analyze
the requirements, and posts clarifying questions as a Linear comment. This
enables a back-and-forth refinement loop before any code is written.

- Loads the project and pulls the latest code for context
- Renders an AI prompt from a template with the issue details
- Invokes Claude to generate questions and a proposed plan
- Posts the AI response as a Linear comment
- Appends an approval hint so the user knows how to proceed

The refinement loop continues each time the user replies on Linear. AI
incorporates the new feedback and posts an updated plan until the user
comments `@autoralph approved`.

## Build

**Transition**: `APPROVED` → `BUILDING`

The build ability creates an isolated workspace and drives the Ralph
execution loop to implement the approved plan.

- Creates a git worktree with a dedicated branch (e.g., `autoralph/uni-42`)
- Generates a PRD (Product Requirements Document) from the approved plan
  using AI
- Invokes Ralph's execution loop inside the worktree
- Ralph iterates through user stories, running quality checks after each
- On success, the branch is ready for a pull request

The workspace name and branch name are stored on the issue record so
subsequent actions know where to find the worktree.

### Build Configuration

| Option | Default | Description |
|--------|---------|-------------|
| `max_iterations` | `20` | Maximum Ralph loop iterations per build |
| `ralph_config_path` | `.ralph/ralph.yaml` | Path to Ralph config relative to project |
| `branch_prefix` | `autoralph/` | Branch name prefix for worktrees |

## Rebase

When the target branch (usually `main`) has advanced since the feature branch
was created, AutoRalph rebases the workspace branch to incorporate upstream
changes.

- Fetches the latest target branch
- Performs a git rebase
- If conflicts are detected, pauses the issue for manual resolution
- On success, force-pushes the updated branch

The rebase ability handles push failures gracefully — if a push fails due to
upstream changes, it retries with a fresh rebase.

## Feedback

**Transition**: `IN_REVIEW` → `ADDRESSING_FEEDBACK`

When a GitHub reviewer requests changes, AutoRalph feeds the review comments
to AI and commits fixes.

- Detects `CHANGES_REQUESTED` or `COMMENTED` reviews via the GitHub poller
- Loads the review comments and diff context
- Renders an AI prompt with the feedback details
- Invokes Claude in the workspace worktree to make fixes
- Runs pre-commit and post-commit [hooks](../ralph/configuration.md#hooks)
  if configured
- Commits changes, pushes, and replies to each review comment on GitHub
- Updates the PR description to reflect the changes
- Transitions back to `IN_REVIEW`

The feedback loop repeats each time new changes are requested, until the
reviewer approves. Only reviews from [trusted users](security.md#trusted-reviewer)
are acted upon when `github_user_id` is configured.

## Fix Checks

**Transition**: `IN_REVIEW` → `FIXING_CHECKS`

When CI checks fail on a pull request, AutoRalph automatically analyzes the
failures and pushes fixes.

- The GitHub poller monitors check runs on the PR head SHA
- When all checks complete and at least one has failed, the issue transitions
  to `FIXING_CHECKS`
- Fetches check run results and logs from the GitHub API (logs truncated to
  200 lines: 30 head + 170 tail)
- Renders a prompt with the failure details and the project's `quality_checks`
  for AI analysis
- Invokes Claude in the workspace worktree to apply fixes
- Runs pre-commit and post-commit [hooks](../ralph/configuration.md#hooks)
  if configured
- Commits and pushes the changes
- Updates the PR description to reflect the fixes
- Transitions back to `IN_REVIEW` where checks re-run automatically

### Loop Protection

To prevent infinite fix loops, AutoRalph tracks the number of fix attempts
per issue. After 3 attempts (configurable), it:

1. Posts a comment on the PR asking for human help
2. Pauses the issue

The attempt counter resets when the head SHA changes externally (e.g., someone
pushes a manual fix or feedback is addressed).

## Complete

**Transition**: `IN_REVIEW` → `COMPLETED`

When a PR is merged on GitHub, the complete ability cleans up and closes out
the issue.

- Detects the merge event via the GitHub poller
- Cleans up the git worktree and local branch
- Updates the Linear issue state to "Done"
- Marks the issue as `COMPLETED` in the database
- Logs the completion in the activity timeline
