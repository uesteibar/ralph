import { useEffect, useState, useCallback } from 'react'
import { useParams, Link } from 'react-router-dom'
import type { Project, Issue } from '../api'
import { fetchProjects, fetchIssuesByProject } from '../api'
import { useWebSocket } from '../useWebSocket'
import type { WSMessage } from '../useWebSocket'
import { StateBadge } from '../components/StateBadge'

const COMPLETED_STATES = ['completed', 'failed', 'dismissed']

function formatDate(dateStr: string): string {
  return new Date(dateStr).toLocaleDateString()
}

function IssueRow({ issue }: { issue: Issue }) {
  return (
    <tr style={{ borderBottom: '1px solid #f3f4f6' }}>
      <td style={{ padding: '10px 12px', fontSize: '13px', fontWeight: 500, color: '#6b7280' }}>
        <Link to={`/issues/${issue.id}`} style={{ color: '#6b7280', textDecoration: 'none' }}>
          {issue.identifier}
        </Link>
      </td>
      <td style={{ padding: '10px 12px', fontSize: '14px' }}>
        <Link to={`/issues/${issue.id}`} style={{ color: '#111827', textDecoration: 'none' }}>
          {issue.title}
        </Link>
      </td>
      <td style={{ padding: '10px 12px' }}>
        <StateBadge state={issue.state} />
      </td>
      <td style={{ padding: '10px 12px', fontSize: '12px', color: '#6b7280' }}>
        {formatDate(issue.created_at)}
      </td>
      <td style={{ padding: '10px 12px', fontSize: '12px', color: '#6b7280' }}>
        {formatDate(issue.updated_at)}
      </td>
    </tr>
  )
}

function IssueTable({ issues }: { issues: Issue[] }) {
  return (
    <div style={{ border: '1px solid #e5e7eb', borderRadius: '8px', overflow: 'hidden', backgroundColor: '#fff' }}>
      <table style={{ width: '100%', borderCollapse: 'collapse' }}>
        <thead>
          <tr style={{ backgroundColor: '#f9fafb' }}>
            <th style={{ padding: '10px 12px', textAlign: 'left', fontSize: '12px', fontWeight: 600, color: '#6b7280', textTransform: 'uppercase' }}>ID</th>
            <th style={{ padding: '10px 12px', textAlign: 'left', fontSize: '12px', fontWeight: 600, color: '#6b7280', textTransform: 'uppercase' }}>Title</th>
            <th style={{ padding: '10px 12px', textAlign: 'left', fontSize: '12px', fontWeight: 600, color: '#6b7280', textTransform: 'uppercase' }}>State</th>
            <th style={{ padding: '10px 12px', textAlign: 'left', fontSize: '12px', fontWeight: 600, color: '#6b7280', textTransform: 'uppercase' }}>Created</th>
            <th style={{ padding: '10px 12px', textAlign: 'left', fontSize: '12px', fontWeight: 600, color: '#6b7280', textTransform: 'uppercase' }}>Updated</th>
          </tr>
        </thead>
        <tbody>
          {issues.map(issue => (
            <IssueRow key={issue.id} issue={issue} />
          ))}
        </tbody>
      </table>
    </div>
  )
}

export default function ProjectDetail() {
  const { id } = useParams<{ id: string }>()
  const [project, setProject] = useState<Project | null>(null)
  const [issues, setIssues] = useState<Issue[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)

  const loadData = useCallback(async () => {
    if (!id) return
    try {
      const [projects, issueList] = await Promise.all([
        fetchProjects(),
        fetchIssuesByProject(id),
      ])
      const found = projects.find(p => p.id === id)
      setProject(found || null)
      setIssues(issueList)
      setError(null)
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to load data')
    } finally {
      setLoading(false)
    }
  }, [id])

  useEffect(() => {
    loadData()
  }, [loadData])

  const handleWSMessage = useCallback((_msg: WSMessage) => {
    loadData()
  }, [loadData])

  useWebSocket(handleWSMessage)

  if (loading) {
    return (
      <div style={{ padding: '40px', textAlign: 'center', color: '#6b7280' }}>
        Loading...
      </div>
    )
  }

  if (error) {
    return (
      <div style={{ padding: '40px', textAlign: 'center', color: '#dc2626' }}>
        Error: {error}
      </div>
    )
  }

  const activeIssues = issues.filter(i => !COMPLETED_STATES.includes(i.state))
  const completedIssues = issues.filter(i => COMPLETED_STATES.includes(i.state))

  return (
    <div style={{ maxWidth: '1200px', margin: '0 auto', padding: '24px', fontFamily: 'system-ui, -apple-system, sans-serif' }}>
      <Link to="/" style={{ fontSize: '13px', color: '#6b7280', textDecoration: 'none', marginBottom: '16px', display: 'inline-block' }}>
        {'\u2190'} Dashboard
      </Link>

      <header style={{ marginBottom: '24px' }}>
        <h1 style={{ margin: '0 0 4px', fontSize: '24px', fontWeight: 700, color: '#111827' }}>
          {project?.name ?? 'Unknown Project'}
        </h1>
        {project && (
          <p style={{ margin: 0, fontSize: '14px', color: '#6b7280' }}>
            {project.github_owner}/{project.github_repo}
          </p>
        )}
      </header>

      {issues.length === 0 ? (
        <p style={{ color: '#9ca3af', fontSize: '14px' }}>No issues found for this project</p>
      ) : (
        <>
          {activeIssues.length > 0 && (
            <section style={{ marginBottom: '24px' }}>
              <h2 style={{ fontSize: '16px', fontWeight: 600, color: '#374151', marginBottom: '12px' }}>
                Active Issues ({activeIssues.length})
              </h2>
              <IssueTable issues={activeIssues} />
            </section>
          )}

          {completedIssues.length > 0 && (
            <section>
              <details data-testid="completed-section">
                <summary style={{ fontSize: '16px', fontWeight: 600, color: '#374151', marginBottom: '12px', cursor: 'pointer' }}>
                  Completed ({completedIssues.length})
                </summary>
                <IssueTable issues={completedIssues} />
              </details>
            </section>
          )}
        </>
      )}
    </div>
  )
}
