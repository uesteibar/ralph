import { useEffect, useState, useCallback } from 'react'
import { Link } from 'react-router-dom'
import type { Project, Issue, CCUsage } from '../api'
import { fetchProjects, fetchIssues, fetchCCUsage } from '../api'
import { CCUsageSection } from '../components/CCUsageBar'
import { useWebSocket } from '../useWebSocket'
import { StateBadge, STATE_COLORS } from '../components/StateBadge'

function ProjectCard({ project }: { project: Project }) {
  const breakdown = project.state_breakdown || {}
  const states = Object.entries(breakdown).filter(([, count]) => count > 0)

  return (
    <div
      style={{
        border: '1px solid #e5e7eb',
        borderRadius: '8px',
        padding: '16px',
        backgroundColor: '#fff',
      }}
    >
      <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: '8px' }}>
        <h3 style={{ margin: 0, fontSize: '16px', fontWeight: 600 }}>{project.name}</h3>
        <span style={{ fontSize: '13px', color: '#6b7280' }}>
          {project.active_issue_count} active
        </span>
      </div>
      <div style={{ fontSize: '12px', color: '#9ca3af', marginBottom: '8px' }}>
        {project.github_owner}/{project.github_repo}
      </div>
      {states.length > 0 && (
        <div style={{ display: 'flex', gap: '6px', flexWrap: 'wrap' }}>
          {states.map(([state, count]) => (
            <span
              key={state}
              style={{
                fontSize: '11px',
                padding: '2px 6px',
                borderRadius: '4px',
                backgroundColor: (STATE_COLORS[state] || '#6b7280') + '20',
                color: STATE_COLORS[state] || '#6b7280',
                fontWeight: 500,
              }}
            >
              {count} {state.replace(/_/g, ' ')}
            </span>
          ))}
        </div>
      )}
    </div>
  )
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
      <td style={{ padding: '10px 12px' }}>
        {issue.state === 'building' && (
          <span
            style={{
              display: 'inline-flex',
              alignItems: 'center',
              gap: '4px',
              fontSize: '12px',
              fontWeight: 500,
              color: issue.build_active ? '#065f46' : '#92400e',
            }}
          >
            <span
              style={{
                width: '6px',
                height: '6px',
                borderRadius: '50%',
                backgroundColor: issue.build_active ? '#10b981' : '#f59e0b',
                display: 'inline-block',
              }}
            />
            {issue.build_active ? 'Running' : 'Stopped'}
          </span>
        )}
        {issue.pr_url && (
          <a
            href={issue.pr_url}
            target="_blank"
            rel="noopener noreferrer"
            style={{ fontSize: '13px', color: '#3b82f6', textDecoration: 'none' }}
          >
            PR #{issue.pr_number}
          </a>
        )}
      </td>
    </tr>
  )
}

export default function Dashboard() {
  const [projects, setProjects] = useState<Project[]>([])
  const [issues, setIssues] = useState<Issue[]>([])
  const [ccUsage, setCCUsage] = useState<CCUsage | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)

  const loadData = useCallback(async () => {
    try {
      const [p, i] = await Promise.all([
        fetchProjects(),
        fetchIssues(),
      ])
      setProjects(p)
      setIssues(i)
      setError(null)
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to load data')
    } finally {
      setLoading(false)
    }
  }, [])

  const loadCCUsage = useCallback(async () => {
    try {
      const data = await fetchCCUsage()
      setCCUsage(data)
    } catch {
      // Silently ignore — usage display is optional
    }
  }, [])

  useEffect(() => {
    loadData()
    loadCCUsage()
  }, [loadData, loadCCUsage])

  const handleWSMessage = useCallback(() => {
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

  const activeIssues = issues.filter(i => !['completed', 'failed', 'dismissed'].includes(i.state))

  return (
    <div style={{ maxWidth: '1200px', margin: '0 auto', padding: '24px', fontFamily: 'system-ui, -apple-system, sans-serif' }}>
      <header style={{ marginBottom: '32px' }}>
        <h1 style={{ margin: 0, fontSize: '24px', fontWeight: 700, color: '#111827' }}>autoralph</h1>
        <p style={{ margin: '4px 0 0', fontSize: '14px', color: '#6b7280' }}>
          {projects.length} project{projects.length !== 1 ? 's' : ''} &middot; {activeIssues.length} active issue{activeIssues.length !== 1 ? 's' : ''}
        </p>
      </header>

      {/* CC Usage */}
      {ccUsage?.available && ccUsage.groups && (
        <section style={{ marginBottom: '32px' }}>
          <h2 style={{ fontSize: '16px', fontWeight: 600, color: '#374151', marginBottom: '12px' }}>Claude Code Usage</h2>
          <div style={{ border: '1px solid #e5e7eb', borderRadius: '8px', padding: '16px', backgroundColor: '#fff' }}>
            <CCUsageSection groups={ccUsage.groups} />
          </div>
        </section>
      )}

      {/* Project Summary Cards */}
      {projects.length > 0 && (
        <section style={{ marginBottom: '32px' }}>
          <h2 style={{ fontSize: '16px', fontWeight: 600, color: '#374151', marginBottom: '12px' }}>Projects</h2>
          <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fill, minmax(280px, 1fr))', gap: '12px' }}>
            {projects.map(p => (
              <Link key={p.id} to={`/projects/${p.id}`} style={{ textDecoration: 'none', color: 'inherit' }}>
                <ProjectCard project={p} />
              </Link>
            ))}
          </div>
        </section>
      )}

      {/* Active Issues */}
      <section>
        <h2 style={{ fontSize: '16px', fontWeight: 600, color: '#374151', marginBottom: '12px' }}>Active Issues</h2>
        {activeIssues.length === 0 ? (
          <p style={{ color: '#9ca3af', fontSize: '14px' }}>No active issues</p>
        ) : (
          <div style={{ border: '1px solid #e5e7eb', borderRadius: '8px', overflow: 'hidden', backgroundColor: '#fff' }}>
            <table style={{ width: '100%', borderCollapse: 'collapse' }}>
              <thead>
                <tr style={{ backgroundColor: '#f9fafb' }}>
                  <th style={{ padding: '10px 12px', textAlign: 'left', fontSize: '12px', fontWeight: 600, color: '#6b7280', textTransform: 'uppercase' }}>ID</th>
                  <th style={{ padding: '10px 12px', textAlign: 'left', fontSize: '12px', fontWeight: 600, color: '#6b7280', textTransform: 'uppercase' }}>Title</th>
                  <th style={{ padding: '10px 12px', textAlign: 'left', fontSize: '12px', fontWeight: 600, color: '#6b7280', textTransform: 'uppercase' }}>State</th>
                  <th style={{ padding: '10px 12px', textAlign: 'left', fontSize: '12px', fontWeight: 600, color: '#6b7280', textTransform: 'uppercase' }}>Info</th>
                </tr>
              </thead>
              <tbody>
                {activeIssues.map(issue => (
                  <IssueRow key={issue.id} issue={issue} />
                ))}
              </tbody>
            </table>
          </div>
        )}
      </section>
    </div>
  )
}
