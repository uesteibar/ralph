import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, waitFor } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import Dashboard from './Dashboard'
import type { Project, Issue } from '../api'

// Mock the api module
vi.mock('../api', () => ({
  fetchProjects: vi.fn(),
  fetchIssues: vi.fn(),
}))

// Mock the WebSocket hook — no-op in tests
vi.mock('../useWebSocket', () => ({
  useWebSocket: vi.fn(),
}))

import { fetchProjects, fetchIssues } from '../api'

const mockProjects: Project[] = [
  {
    id: 'p1',
    name: 'my-project',
    local_path: '/tmp/proj',
    github_owner: 'octocat',
    github_repo: 'hello-world',
    active_issue_count: 2,
    state_breakdown: { queued: 1, building: 1 },
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
    pr_number: 42,
    pr_url: 'https://github.com/octocat/hello-world/pull/42',
    build_active: false,
    created_at: '2025-02-11T11:00:00Z',
    updated_at: '2025-02-11T12:00:00Z',
  },
  {
    id: 'iss3',
    project_id: 'p1',
    identifier: 'PROJ-3',
    title: 'Old completed',
    state: 'completed',
    build_active: false,
    created_at: '2025-02-10T10:00:00Z',
    updated_at: '2025-02-10T12:00:00Z',
  },
]

function renderDashboard() {
  return render(
    <MemoryRouter>
      <Dashboard />
    </MemoryRouter>,
  )
}

beforeEach(() => {
  vi.mocked(fetchProjects).mockResolvedValue(mockProjects)
  vi.mocked(fetchIssues).mockResolvedValue(mockIssues)
})

describe('Dashboard', () => {
  it('shows loading state initially', () => {
    // Make fetch hang so we can see loading state
    vi.mocked(fetchProjects).mockReturnValue(new Promise(() => {}))
    renderDashboard()
    expect(screen.getByText('Loading...')).toBeInTheDocument()
  })

  it('renders project cards with name and active count', async () => {
    renderDashboard()
    await waitFor(() => {
      expect(screen.getByText('my-project')).toBeInTheDocument()
    })
    expect(screen.getByText('2 active')).toBeInTheDocument()
    expect(screen.getByText('octocat/hello-world')).toBeInTheDocument()
  })

  it('renders state breakdown in project cards', async () => {
    renderDashboard()
    await waitFor(() => {
      expect(screen.getByText('1 queued')).toBeInTheDocument()
    })
    expect(screen.getByText('1 building')).toBeInTheDocument()
  })

  it('renders active issues (excludes completed/failed)', async () => {
    renderDashboard()
    await waitFor(() => {
      expect(screen.getByText('Add user avatars')).toBeInTheDocument()
    })
    expect(screen.getByText('Fix login bug')).toBeInTheDocument()
    expect(screen.queryByText('Old completed')).not.toBeInTheDocument()
  })

  it('shows issue identifiers', async () => {
    renderDashboard()
    await waitFor(() => {
      expect(screen.getByText('PROJ-1')).toBeInTheDocument()
    })
    expect(screen.getByText('PROJ-2')).toBeInTheDocument()
  })

  it('shows state badges', async () => {
    renderDashboard()
    // CSS text-transform: uppercase is applied, but DOM text is lowercase
    await waitFor(() => {
      expect(screen.getByText('building')).toBeInTheDocument()
    })
    expect(screen.getByText('in review')).toBeInTheDocument()
  })

  it('shows PR link for issues in review', async () => {
    renderDashboard()
    await waitFor(() => {
      expect(screen.getByText('PR #42')).toBeInTheDocument()
    })
    const link = screen.getByText('PR #42')
    expect(link).toHaveAttribute('href', 'https://github.com/octocat/hello-world/pull/42')
  })

  it('shows error state on API failure', async () => {
    vi.mocked(fetchProjects).mockRejectedValue(new Error('network error'))
    renderDashboard()
    await waitFor(() => {
      expect(screen.getByText('Error: network error')).toBeInTheDocument()
    })
  })

  it('shows summary counts in header', async () => {
    renderDashboard()
    await waitFor(() => {
      expect(screen.getByText(/1 project/)).toBeInTheDocument()
    })
    expect(screen.getByText(/2 active issues/)).toBeInTheDocument()
  })

  it('handles empty state gracefully', async () => {
    vi.mocked(fetchProjects).mockResolvedValue([])
    vi.mocked(fetchIssues).mockResolvedValue([])

    renderDashboard()
    await waitFor(() => {
      expect(screen.getByText('No active issues')).toBeInTheDocument()
    })
  })
})
