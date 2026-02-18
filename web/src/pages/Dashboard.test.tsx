import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, waitFor } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import Dashboard from './Dashboard'
import type { Project, Issue, CCUsage } from '../api'

// Mock the api module
vi.mock('../api', () => ({
  fetchProjects: vi.fn(),
  fetchIssues: vi.fn(),
  fetchCCUsage: vi.fn(),
}))

// Mock the WebSocket hook — no-op in tests
vi.mock('../useWebSocket', () => ({
  useWebSocket: vi.fn(),
}))

import { fetchProjects, fetchIssues, fetchCCUsage } from '../api'

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

const mockCCUsageAvailable: CCUsage = {
  available: true,
  groups: [
    {
      group_label: 'Claude Code Usage Statistics',
      lines: [
        { label: '5-hour', percentage: 50, reset_duration: '3h 13m' },
        { label: '7-day', percentage: 83, reset_duration: '2d 5h' },
      ],
    },
  ],
}

const mockCCUsageUnavailable: CCUsage = { available: false }

beforeEach(() => {
  vi.mocked(fetchProjects).mockResolvedValue(mockProjects)
  vi.mocked(fetchIssues).mockResolvedValue(mockIssues)
  vi.mocked(fetchCCUsage).mockResolvedValue(mockCCUsageUnavailable)
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

  it('project cards link to /projects/:id', async () => {
    renderDashboard()
    await waitFor(() => {
      expect(screen.getByText('my-project')).toBeInTheDocument()
    })
    const projectLink = screen.getByText('my-project').closest('a')
    expect(projectLink).toHaveAttribute('href', '/projects/p1')
  })

  it('project card links have no underline', async () => {
    renderDashboard()
    await waitFor(() => {
      expect(screen.getByText('my-project')).toBeInTheDocument()
    })
    const projectLink = screen.getByText('my-project').closest('a')
    expect(projectLink).toHaveStyle({ textDecoration: 'none' })
  })

  it('each project card links to its own detail page', async () => {
    vi.mocked(fetchProjects).mockResolvedValue([
      ...mockProjects,
      {
        id: 'p2',
        name: 'second-project',
        local_path: '/tmp/proj2',
        github_owner: 'octocat',
        github_repo: 'second-repo',
        active_issue_count: 0,
        state_breakdown: {},
      },
    ])
    renderDashboard()
    await waitFor(() => {
      expect(screen.getByText('second-project')).toBeInTheDocument()
    })
    const firstLink = screen.getByText('my-project').closest('a')
    const secondLink = screen.getByText('second-project').closest('a')
    expect(firstLink).toHaveAttribute('href', '/projects/p1')
    expect(secondLink).toHaveAttribute('href', '/projects/p2')
  })

  it('shows Claude Code Usage section when data is available', async () => {
    vi.mocked(fetchCCUsage).mockResolvedValue(mockCCUsageAvailable)
    renderDashboard()
    await waitFor(() => {
      expect(screen.getByText('Claude Code Usage')).toBeInTheDocument()
    })
    expect(screen.getByText('Claude Code Usage Statistics')).toBeInTheDocument()
    expect(screen.getByText('5-hour')).toBeInTheDocument()
    expect(screen.getByText('50%')).toBeInTheDocument()
    expect(screen.getByText('resets in 3h 13m')).toBeInTheDocument()
    expect(screen.getByText('7-day')).toBeInTheDocument()
    expect(screen.getByText('83%')).toBeInTheDocument()
  })

  it('hides usage section when API returns available: false', async () => {
    vi.mocked(fetchCCUsage).mockResolvedValue(mockCCUsageUnavailable)
    renderDashboard()
    await waitFor(() => {
      expect(screen.getByText('my-project')).toBeInTheDocument()
    })
    expect(screen.queryByText('Claude Code Usage')).not.toBeInTheDocument()
  })

  it('hides usage section when fetchCCUsage fails', async () => {
    vi.mocked(fetchCCUsage).mockRejectedValue(new Error('fail'))
    renderDashboard()
    await waitFor(() => {
      expect(screen.getByText('my-project')).toBeInTheDocument()
    })
    expect(screen.queryByText('Claude Code Usage')).not.toBeInTheDocument()
  })

  it('fetches CC usage on mount', async () => {
    vi.mocked(fetchCCUsage).mockResolvedValue(mockCCUsageUnavailable)
    renderDashboard()
    await waitFor(() => {
      expect(fetchCCUsage).toHaveBeenCalled()
    })
  })
})
