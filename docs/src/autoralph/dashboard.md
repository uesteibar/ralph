# Dashboard

AutoRalph includes a built-in web dashboard for monitoring issues, viewing
build progress, and managing the autonomous workflow. The React-based SPA is
compiled into the Go binary via `go:embed`, so no separate static file
serving is needed.

Open `http://127.0.0.1:7749` in your browser after starting AutoRalph.

## Web Interface

### Dashboard (`/`)

- **Project cards**: Each configured project with active issue count, GitHub
  link, and state breakdown (how many issues in each state)
- **Active issues table**: All non-completed issues with identifier, title,
  state badge, progress indicator (when building), and PR link (when in
  review)
- **Activity feed**: Recent activity across all projects (state changes, build
  events, comments posted)

### Issue Detail (`/issues/:id`)

- **Header**: Issue identifier, state badge, project name, title
- **PR link**: When a PR exists
- **Error display**: When the issue has failed
- **Action buttons**:
  - **Pause**: Pauses any active issue
  - **Resume**: Resumes a paused issue to its previous state
  - **Retry**: Retries a failed issue from its pre-failure state
- **Live build log**: When the issue is in BUILDING state, shows a real-time
  terminal-style log of the Ralph execution loop
- **Story progress**: Sidebar showing which stories have passed/failed during
  the build
- **Timeline**: Chronological activity log with event icons, state
  transitions, and timestamps

### Live Updates

The dashboard connects via WebSocket and automatically refreshes when state
changes, build events, new issues, or activity entries occur. If the
connection drops, it reconnects automatically after 3 seconds.

## REST API

All endpoints return JSON. Errors use the format `{"error": "message"}`.

### Endpoints

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/api/status` | Health check with uptime and active build count |
| `GET` | `/api/projects` | List all projects with active issue counts and state breakdown |
| `GET` | `/api/issues` | List issues (filterable) |
| `GET` | `/api/issues/{id}` | Issue detail with activity timeline |
| `POST` | `/api/issues/{id}/pause` | Pause an active issue |
| `POST` | `/api/issues/{id}/resume` | Resume a paused issue |
| `POST` | `/api/issues/{id}/retry` | Retry a failed issue |
| `GET` | `/api/activity` | Recent activity across all projects |

### Query Parameters

**`GET /api/issues`**:
- `project_id` — Filter by project
- `state` — Filter by state (e.g., `building`, `in_review`)

**`GET /api/issues/{id}`**:
- `limit` — Activity entries per page (default: 20)
- `offset` — Activity pagination offset

**`GET /api/activity`**:
- `limit` — Max entries to return (default: 20)

### Examples

```bash
# Health check
curl http://127.0.0.1:7749/api/status

# List all projects
curl http://127.0.0.1:7749/api/projects

# List building issues
curl http://127.0.0.1:7749/api/issues?state=building

# Get issue detail
curl http://127.0.0.1:7749/api/issues/<issue-id>

# Pause an issue
curl -X POST http://127.0.0.1:7749/api/issues/<issue-id>/pause

# Resume a paused issue
curl -X POST http://127.0.0.1:7749/api/issues/<issue-id>/resume

# Retry a failed issue
curl -X POST http://127.0.0.1:7749/api/issues/<issue-id>/retry

# Recent activity
curl http://127.0.0.1:7749/api/activity?limit=10
```

## WebSocket

Connect to `ws://127.0.0.1:7749/api/ws` for real-time updates.

### Message Format

```json
{
  "type": "issue_state_changed",
  "payload": { ... },
  "timestamp": "2026-02-11T16:42:19Z"
}
```

### Message Types

| Type | Description |
|------|-------------|
| `issue_state_changed` | An issue transitioned to a new state |
| `build_event` | A build event occurred (story started, passed, failed, etc.) |
| `new_issue` | A new issue was ingested from Linear |
| `activity` | A new activity log entry was created |

The dashboard uses these events to refresh data in real time. You can build
custom integrations by connecting to the same WebSocket endpoint.
