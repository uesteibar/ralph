import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, waitFor } from '@testing-library/react'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import ProjectDetail from './ProjectDetail'
import type { Project, Issue } from '../api'

vi.mock('../api', () => ({
  fetchProjects: vi.fn(),
  fetchIssuesByProject: vi.fn(),
}))

vi.mock('../useWebSocket', () => ({
  useWebSocket: vi.fn(),
}))

import { fetchProjects, fetchIssuesByProject } from '../api'
import { useWebSocket } from '../useWebSocket'

const mockProjects: Project[] = [
  {
    id: 'p1',
    name: 'my-project',
    local_path: '/tmp/proj',
    github_owner: 'octocat',
    github_repo: 'hello-world',
    active_issue_count: 3,
    state_breakdown: { queued: 1, building: 1, in_review: 1 },
  },
]

const mockIssues: Issue[] = [
  {
    id: 'iss1',
    project_id: 'p1',
    identifier: 'PROJ-1',
    title: 'Add user avatars',
    state: 'building',
    build_active: true,
    created_at: '2025-02-11T12:00:00Z',
    updated_at: '2025-02-11T12:30:00Z',
  },
  {
    id: 'iss2',
    project_id: 'p1',
    identifier: 'PROJ-2',
    title: 'Fix login bug',
    state: 'in_review',
    build_active: false,
    created_at: '2025-02-11T11:00:00Z',
    updated_at: '2025-02-11T12:00:00Z',
  },
  {
    id: 'iss3',
    project_id: 'p1',
    identifier: 'PROJ-3',
    title: 'Old completed task',
    state: 'completed',
    build_active: false,
    created_at: '2025-02-10T10:00:00Z',
    updated_at: '2025-02-10T12:00:00Z',
  },
  {
    id: 'iss4',
    project_id: 'p1',
    identifier: 'PROJ-4',
    title: 'Failed deployment',
    state: 'failed',
    build_active: false,
    created_at: '2025-02-10T09:00:00Z',
    updated_at: '2025-02-10T10:00:00Z',
  },
  {
    id: 'iss5',
    project_id: 'p1',
    identifier: 'PROJ-5',
    title: 'Dismissed issue',
    state: 'dismissed',
    build_active: false,
    created_at: '2025-02-10T08:00:00Z',
    updated_at: '2025-02-10T09:00:00Z',
  },
]

function renderProjectDetail(projectId = 'p1') {
  return render(
    <MemoryRouter initialEntries={[`/projects/${projectId}`]}>
      <Routes>
        <Route path="/projects/:id" element={<ProjectDetail />} />
        <Route path="/issues/:id" element={<div>Issue Detail Page</div>} />
        <Route path="/" element={<div>Dashboard Page</div>} />
      </Routes>
    </MemoryRouter>,
  )
}

beforeEach(() => {
  vi.mocked(fetchProjects).mockResolvedValue(mockProjects)
  vi.mocked(fetchIssuesByProject).mockResolvedValue(mockIssues)
})

describe('ProjectDetail', () => {
  it('shows loading state initially', () => {
    vi.mocked(fetchProjects).mockReturnValue(new Promise(() => {}))
    renderProjectDetail()
    expect(screen.getByText('Loading...')).toBeInTheDocument()
  })

  it('renders project name and github info in header', async () => {
    renderProjectDetail()
    await waitFor(() => {
      expect(screen.getByText('my-project')).toBeInTheDocument()
    })
    expect(screen.getByText('octocat/hello-world')).toBeInTheDocument()
  })

  it('renders back-to-dashboard link', async () => {
    renderProjectDetail()
    await waitFor(() => {
      expect(screen.getByText('my-project')).toBeInTheDocument()
    })
    const backLink = screen.getByText(/Dashboard/)
    expect(backLink.closest('a')).toHaveAttribute('href', '/')
  })

  it('renders active issues in the main section', async () => {
    renderProjectDetail()
    await waitFor(() => {
      expect(screen.getByText('Add user avatars')).toBeInTheDocument()
    })
    expect(screen.getByText('Fix login bug')).toBeInTheDocument()
    expect(screen.getByText('PROJ-1')).toBeInTheDocument()
    expect(screen.getByText('PROJ-2')).toBeInTheDocument()
  })

  it('renders state badges for active issues', async () => {
    renderProjectDetail()
    await waitFor(() => {
      expect(screen.getByText('building')).toBeInTheDocument()
    })
    expect(screen.getByText('in review')).toBeInTheDocument()
  })

  it('renders completed issues in a collapsed details section', async () => {
    renderProjectDetail()
    await waitFor(() => {
      expect(screen.getByText('my-project')).toBeInTheDocument()
    })
    const details = screen.getByTestId('completed-section')
    expect(details).not.toHaveAttribute('open')
    expect(screen.getByText(/Completed/)).toBeInTheDocument()
    expect(screen.getByText('Old completed task')).toBeInTheDocument()
    expect(screen.getByText('Failed deployment')).toBeInTheDocument()
    expect(screen.getByText('Dismissed issue')).toBeInTheDocument()
  })

  it('links issue rows to /issues/:id', async () => {
    renderProjectDetail()
    await waitFor(() => {
      expect(screen.getByText('PROJ-1')).toBeInTheDocument()
    })
    const link = screen.getByText('PROJ-1').closest('a')
    expect(link).toHaveAttribute('href', '/issues/iss1')
  })

  it('shows error state on API failure', async () => {
    vi.mocked(fetchProjects).mockRejectedValue(new Error('network error'))
    renderProjectDetail()
    await waitFor(() => {
      expect(screen.getByText('Error: network error')).toBeInTheDocument()
    })
  })

  it('shows empty state when project has no issues', async () => {
    vi.mocked(fetchIssuesByProject).mockResolvedValue([])
    renderProjectDetail()
    await waitFor(() => {
      expect(screen.getByText('my-project')).toBeInTheDocument()
    })
    expect(screen.getByText('No issues found for this project')).toBeInTheDocument()
  })

  it('refreshes data on WebSocket message', async () => {
    let wsCallback: (msg: unknown) => void = () => {}
    vi.mocked(useWebSocket).mockImplementation((cb) => {
      wsCallback = cb as (msg: unknown) => void
    })

    renderProjectDetail()
    await waitFor(() => {
      expect(screen.getByText('my-project')).toBeInTheDocument()
    })

    // Reset mock call count
    vi.mocked(fetchIssuesByProject).mockClear()

    // Simulate a WebSocket message
    wsCallback({ type: 'state_change', payload: {}, timestamp: new Date().toISOString() })

    await waitFor(() => {
      expect(fetchIssuesByProject).toHaveBeenCalled()
    })
  })

  it('shows timestamps for issues', async () => {
    renderProjectDetail()
    await waitFor(() => {
      expect(screen.getByText('my-project')).toBeInTheDocument()
    })
    // Timestamps are rendered as formatted dates — multiple cells share same date
    const dateCells = screen.getAllByText('2/11/2025')
    expect(dateCells.length).toBeGreaterThan(0)
  })
})
