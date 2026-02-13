import { useEffect, useState, useCallback, useRef } from 'react'
import { useParams, Link, useNavigate } from 'react-router-dom'
import type { IssueDetail as IssueDetailType, Activity, StoryInfo, IntegrationTestInfo } from '../api'
import { fetchIssue, resumeIssue, retryIssue, deleteIssue } from '../api'
import { useWebSocket } from '../useWebSocket'
import type { WSMessage } from '../useWebSocket'
import { StateBadge } from '../components/StateBadge'

function timeAgo(dateStr: string): string {
  const date = new Date(dateStr)
  const now = new Date()
  const seconds = Math.floor((now.getTime() - date.getTime()) / 1000)

  if (seconds < 60) return 'just now'
  if (seconds < 3600) return `${Math.floor(seconds / 60)}m ago`
  if (seconds < 86400) return `${Math.floor(seconds / 3600)}h ago`
  return `${Math.floor(seconds / 86400)}d ago`
}

function eventIcon(eventType: string): string {
  switch (eventType) {
    case 'state_change': return '\u25B6'
    case 'build_event': return '\u2699'
    case 'pr_created': return '\u2197'
    case 'pr_merged': return '\u2714'
    case 'issue_completed': return '\u2705'
    case 'build_completed': return '\u2705'
    case 'build_failed': return '\u274C'
    case 'approval_detected': return '\u2705'
    case 'plan_iteration': return '\u270E'
    case 'changes_requested': return '\u21BB'
    case 'feedback_addressed': return '\u2714'
    case 'workspace_created': return '\uD83D\uDCC1'
    default: return '\u25CF'
  }
}

function TimelineItem({ activity }: { activity: Activity }) {
  const isBuildEvent = activity.event_type === 'build_event'

  return (
    <div
      style={{
        display: 'flex',
        gap: '12px',
        padding: '10px 0',
        borderBottom: '1px solid #f3f4f6',
        fontSize: isBuildEvent ? '12px' : '13px',
        color: isBuildEvent ? '#9ca3af' : '#374151',
      }}
    >
      <span style={{ flexShrink: 0, width: '20px', textAlign: 'center' }}>
        {eventIcon(activity.event_type)}
      </span>
      <div style={{ flex: 1, minWidth: 0 }}>
        {activity.from_state && activity.to_state ? (
          <span>
            <StateBadge state={activity.from_state} />{' '}
            <span style={{ color: '#9ca3af' }}>{'\u2192'}</span>{' '}
            <StateBadge state={activity.to_state} />
            {activity.detail && (
              <span style={{ marginLeft: '8px', color: '#6b7280' }}>
                {activity.detail}
              </span>
            )}
          </span>
        ) : (
          <span>{activity.detail || activity.event_type}</span>
        )}
      </div>
      <span style={{ flexShrink: 0, color: '#9ca3af', fontSize: '12px' }}>
        {timeAgo(activity.created_at)}
      </span>
    </div>
  )
}

function BuildLog({ activities }: { activities: Activity[] }) {
  const logRef = useRef<HTMLDivElement>(null)
  // Show build events in chronological order (oldest first).
  const buildEvents = activities
    .filter(a => a.event_type === 'build_event')
    .slice()
    .reverse()

  useEffect(() => {
    // Auto-scroll to bottom when new events arrive.
    if (logRef.current) {
      logRef.current.scrollTop = logRef.current.scrollHeight
    }
  }, [buildEvents.length])

  if (buildEvents.length === 0) return null

  return (
    <div
      ref={logRef}
      style={{
        backgroundColor: '#1e1e1e',
        color: '#d4d4d4',
        borderRadius: '8px',
        padding: '16px',
        fontFamily: 'monospace',
        fontSize: '12px',
        maxHeight: '400px',
        overflowY: 'auto',
        marginBottom: '16px',
      }}
    >
      {buildEvents.map(event => (
        <div key={event.id} style={{ padding: '2px 0', whiteSpace: 'pre-wrap', wordBreak: 'break-word' }}>
          <span style={{ color: '#6a9955' }}>
            {new Date(event.created_at).toLocaleTimeString()}
          </span>{' '}
          {event.detail || event.event_type}
        </div>
      ))}
    </div>
  )
}

function StoryList({
  stories,
  integrationTests,
  currentStory,
}: {
  stories: StoryInfo[]
  integrationTests: IntegrationTestInfo[]
  currentStory?: string
}) {
  if (stories.length === 0 && integrationTests.length === 0) return null

  return (
    <div
      style={{
        border: '1px solid #e5e7eb',
        borderRadius: '8px',
        padding: '16px',
        backgroundColor: '#fff',
        marginBottom: '16px',
      }}
    >
      {stories.length > 0 && (
        <div style={{ marginBottom: integrationTests.length > 0 ? '16px' : 0 }}>
          <h3 style={{ margin: '0 0 10px', fontSize: '14px', fontWeight: 600, color: '#374151' }}>
            User Stories ({stories.filter(s => s.passes).length}/{stories.length})
          </h3>
          {stories.map(story => {
            const isActive = currentStory === story.id
            const icon = story.passes ? '\u2705' : isActive ? '\u25B6' : '\u25CB'
            const color = story.passes ? '#22c55e' : isActive ? '#f59e0b' : '#9ca3af'
            return (
              <div
                key={story.id}
                style={{
                  display: 'flex',
                  alignItems: 'center',
                  gap: '8px',
                  padding: '5px 0',
                  fontSize: '13px',
                  color: isActive ? '#111827' : story.passes ? '#6b7280' : '#374151',
                  fontWeight: isActive ? 600 : 400,
                }}
              >
                <span style={{ color, flexShrink: 0 }}>{icon}</span>
                <span style={{ fontWeight: 500, color: '#6b7280', flexShrink: 0 }}>{story.id}</span>
                <span>{story.title}</span>
                {isActive && (
                  <span style={{
                    fontSize: '11px',
                    padding: '1px 6px',
                    borderRadius: '4px',
                    backgroundColor: '#fef3c7',
                    color: '#92400e',
                    fontWeight: 600,
                    marginLeft: 'auto',
                    flexShrink: 0,
                  }}>
                    IN PROGRESS
                  </span>
                )}
              </div>
            )
          })}
        </div>
      )}

      {integrationTests.length > 0 && (
        <div>
          <h3 style={{ margin: '0 0 10px', fontSize: '14px', fontWeight: 600, color: '#374151' }}>
            Integration Tests ({integrationTests.filter(t => t.passes).length}/{integrationTests.length})
          </h3>
          {integrationTests.map(test => {
            const icon = test.passes ? '\u2705' : '\u25CB'
            const color = test.passes ? '#22c55e' : '#9ca3af'
            return (
              <div
                key={test.id}
                style={{
                  display: 'flex',
                  alignItems: 'center',
                  gap: '8px',
                  padding: '5px 0',
                  fontSize: '13px',
                  color: test.passes ? '#6b7280' : '#374151',
                }}
              >
                <span style={{ color, flexShrink: 0 }}>{icon}</span>
                <span style={{ fontWeight: 500, color: '#6b7280', flexShrink: 0 }}>{test.id}</span>
                <span>{test.description}</span>
              </div>
            )
          })}
        </div>
      )}
    </div>
  )
}

export default function IssueDetail() {
  const { id } = useParams<{ id: string }>()
  const navigate = useNavigate()
  const [issue, setIssue] = useState<IssueDetailType | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [actionPending, setActionPending] = useState(false)
  // Accumulate real-time build events from WebSocket to avoid re-fetching the
  // entire issue on every tool use event. Cleared when a full reload happens.
  const [streamEvents, setStreamEvents] = useState<Activity[]>([])
  const streamIdCounter = useRef(0)

  const loadIssue = useCallback(async () => {
    if (!id) return
    try {
      const data = await fetchIssue(id)
      setIssue(data)
      setStreamEvents([]) // API response includes all persisted events
      setError(null)
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to load issue')
    } finally {
      setLoading(false)
    }
  }, [id])

  useEffect(() => {
    loadIssue()
  }, [loadIssue])

  const handleWSMessage = useCallback((msg: WSMessage) => {
    if (msg.type === 'build_event') {
      const payload = msg.payload as { issue_id?: string; detail?: string }
      if (payload.issue_id === id) {
        streamIdCounter.current += 1
        const syntheticActivity: Activity = {
          id: `stream-${streamIdCounter.current}`,
          issue_id: id!,
          event_type: 'build_event',
          detail: payload.detail ?? '',
          created_at: msg.timestamp || new Date().toISOString(),
        }
        setStreamEvents(prev => [...prev, syntheticActivity])
      }
      return
    }
    // For state changes and other events, do a full reload.
    loadIssue()
  }, [id, loadIssue])

  useWebSocket(handleWSMessage)

  const handleResume = async () => {
    if (!id || actionPending) return
    setActionPending(true)
    try {
      await resumeIssue(id)
      await loadIssue()
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Action failed')
    } finally {
      setActionPending(false)
    }
  }

  const handleRetry = async () => {
    if (!id || actionPending) return
    setActionPending(true)
    try {
      await retryIssue(id)
      await loadIssue()
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Action failed')
    } finally {
      setActionPending(false)
    }
  }

  const handleDelete = async () => {
    if (!id || actionPending) return
    if (!window.confirm('Delete this issue from autoralph? It will be re-ingested if still assigned in Linear.')) return
    setActionPending(true)
    try {
      await deleteIssue(id)
      navigate('/')
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Action failed')
      setActionPending(false)
    }
  }

  if (loading) {
    return (
      <div style={{ padding: '40px', textAlign: 'center', color: '#6b7280' }}>
        Loading...
      </div>
    )
  }

  if (error && !issue) {
    return (
      <div style={{ padding: '40px', textAlign: 'center', color: '#dc2626' }}>
        Error: {error}
      </div>
    )
  }

  if (!issue) {
    return (
      <div style={{ padding: '40px', textAlign: 'center', color: '#6b7280' }}>
        Issue not found
      </div>
    )
  }

  const buttonStyle = (bg: string): React.CSSProperties => ({
    padding: '6px 16px',
    borderRadius: '6px',
    border: 'none',
    fontSize: '13px',
    fontWeight: 600,
    color: '#fff',
    backgroundColor: bg,
    cursor: actionPending ? 'not-allowed' : 'pointer',
    opacity: actionPending ? 0.6 : 1,
  })

  return (
    <div style={{ maxWidth: '1000px', margin: '0 auto', padding: '24px', fontFamily: 'system-ui, -apple-system, sans-serif' }}>
      {/* Back link */}
      <Link to="/" style={{ fontSize: '13px', color: '#6b7280', textDecoration: 'none', marginBottom: '16px', display: 'inline-block' }}>
        {'\u2190'} Dashboard
      </Link>

      {/* Issue Header */}
      <header style={{ marginBottom: '24px' }}>
        <div style={{ display: 'flex', alignItems: 'center', gap: '12px', marginBottom: '8px' }}>
          <span style={{ fontSize: '14px', fontWeight: 500, color: '#6b7280' }}>{issue.identifier}</span>
          <StateBadge state={issue.state} />
          {issue.state === 'building' && (
            <span
              style={{
                display: 'inline-flex',
                alignItems: 'center',
                gap: '6px',
                padding: '2px 10px',
                borderRadius: '12px',
                fontSize: '12px',
                fontWeight: 600,
                color: issue.build_active ? '#065f46' : '#92400e',
                backgroundColor: issue.build_active ? '#d1fae5' : '#fef3c7',
              }}
            >
              <span
                style={{
                  width: '8px',
                  height: '8px',
                  borderRadius: '50%',
                  backgroundColor: issue.build_active ? '#10b981' : '#f59e0b',
                  display: 'inline-block',
                }}
              />
              {issue.build_active ? 'Daemon running' : 'Daemon stopped'}
            </span>
          )}
          {issue.iteration && issue.iteration > 0 && (
            <span style={{ fontSize: '12px', color: '#6b7280', fontWeight: 500 }}>
              Iteration {issue.iteration}{issue.max_iterations ? `/${issue.max_iterations}` : ''}
            </span>
          )}
          {issue.project_name && (
            <span style={{ fontSize: '13px', color: '#9ca3af' }}>{issue.project_name}</span>
          )}
        </div>
        <h1 style={{ margin: '0 0 12px', fontSize: '22px', fontWeight: 700, color: '#111827' }}>
          {issue.title}
        </h1>

        {/* PR link */}
        {issue.pr_url && (
          <div style={{ marginBottom: '8px' }}>
            <a
              href={issue.pr_url}
              target="_blank"
              rel="noopener noreferrer"
              style={{ fontSize: '13px', color: '#3b82f6', textDecoration: 'none' }}
            >
              PR #{issue.pr_number}
            </a>
          </div>
        )}

        {/* Error message */}
        {issue.error_message && (
          <div
            style={{
              padding: '8px 12px',
              borderRadius: '6px',
              backgroundColor: '#fef2f2',
              border: '1px solid #fecaca',
              color: '#dc2626',
              fontSize: '13px',
              marginBottom: '8px',
            }}
          >
            {issue.error_message}
          </div>
        )}

        {/* Action buttons */}
        <div style={{ display: 'flex', gap: '8px', marginTop: '12px' }}>
          {issue.state === 'paused' && (
            <button onClick={handleResume} disabled={actionPending} style={buttonStyle('#3b82f6')}>
              Resume
            </button>
          )}
          {issue.state === 'failed' && (
            <button onClick={handleRetry} disabled={actionPending} style={buttonStyle('#f59e0b')}>
              Retry
            </button>
          )}
          <button onClick={handleDelete} disabled={actionPending} style={buttonStyle('#dc2626')}>
            Delete
          </button>
        </div>
      </header>

      {/* User Stories & Integration Tests */}
      <StoryList
        stories={issue.stories ?? []}
        integrationTests={issue.integration_tests ?? []}
        currentStory={issue.state === 'building' ? issue.current_story : undefined}
      />

      {/* Build section — only when building */}
      {issue.state === 'building' && (
        <section style={{ marginBottom: '24px' }}>
          <h2 style={{ fontSize: '16px', fontWeight: 600, color: '#374151', marginBottom: '12px' }}>
            Live Build
          </h2>
          <BuildLog activities={[...issue.activity, ...streamEvents]} />
        </section>
      )}

      {/* Timeline */}
      <section>
        <h2 style={{ fontSize: '16px', fontWeight: 600, color: '#374151', marginBottom: '12px' }}>
          Timeline
        </h2>
        {issue.activity.length === 0 ? (
          <p style={{ color: '#9ca3af', fontSize: '14px' }}>No activity yet</p>
        ) : (
          <div
            style={{
              border: '1px solid #e5e7eb',
              borderRadius: '8px',
              padding: '12px 16px',
              backgroundColor: '#fff',
            }}
          >
            {issue.activity.map(a => (
              <TimelineItem key={a.id} activity={a} />
            ))}
          </div>
        )}
      </section>
    </div>
  )
}
