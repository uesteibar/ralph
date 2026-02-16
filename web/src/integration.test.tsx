import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { MemoryRouter, Routes, Route } from 'react-router-dom'
import Dashboard from './pages/Dashboard'
import ProjectDetail from './pages/ProjectDetail'
import IssueDetail from './pages/IssueDetail'
import type { Project, Issue, IssueDetail as IssueDetailType } from './api'

// ── Mocks ──────────────────────────────────────────────────────────

vi.mock('./api', () => ({
  fetchProjects: vi.fn(),
  fetchIssues: vi.fn(),
  fetchIssuesByProject: vi.fn(),
  fetchIssue: vi.fn(),
  resumeIssue: vi.fn(),
  retryIssue: vi.fn(),
  deleteIssue: vi.fn(),
}))

vi.mock('./useWebSocket', () => ({
  useWebSocket: vi.fn(),
}))

import {
  fetchProjects,
  fetchIssues,
  fetchIssuesByProject,
  fetchIssue,
} from './api'
import { useWebSocket } from './useWebSocket'

// ── Fixtures ───────────────────────────────────────────────────────

const projects: Project[] = [
  {
    id: 'p1',
    name: 'alpha-project',
    local_path: '/tmp/alpha',
    github_owner: 'acme',
    github_repo: 'alpha',
    active_issue_count: 3,
    state_breakdown: { queued: 1, building: 1, in_review: 1 },
  },
]

const issuesMixed: Issue[] = [
  {
    id: 'iss-q',
    project_id: 'p1',
    identifier: 'ALPHA-1',
    title: 'Queued task',
    state: 'queued',
    build_active: false,
    created_at: '2025-03-01T10:00:00Z',
    updated_at: '2025-03-01T10:00:00Z',
  },
  {
    id: 'iss-b',
    project_id: 'p1',
    identifier: 'ALPHA-2',
    title: 'Building feature',
    state: 'building',
    build_active: true,
    created_at: '2025-03-01T11:00:00Z',
    updated_at: '2025-03-01T11:30:00Z',
  },
  {
    id: 'iss-ir',
    project_id: 'p1',
    identifier: 'ALPHA-3',
    title: 'In review PR',
    state: 'in_review',
    build_active: false,
    created_at: '2025-03-01T09:00:00Z',
    updated_at: '2025-03-01T12:00:00Z',
  },
  {
    id: 'iss-c',
    project_id: 'p1',
    identifier: 'ALPHA-4',
    title: 'Completed work',
    state: 'completed',
    build_active: false,
    created_at: '2025-02-28T10:00:00Z',
    updated_at: '2025-02-28T14:00:00Z',
  },
  {
    id: 'iss-f',
    project_id: 'p1',
    identifier: 'ALPHA-5',
    title: 'Failed deployment',
    state: 'failed',
    build_active: false,
    created_at: '2025-02-28T08:00:00Z',
    updated_at: '2025-02-28T09:00:00Z',
  },
  {
    id: 'iss-d',
    project_id: 'p1',
    identifier: 'ALPHA-6',
    title: 'Dismissed ticket',
    state: 'dismissed',
    build_active: false,
    created_at: '2025-02-27T10:00:00Z',
    updated_at: '2025-02-27T11:00:00Z',
  },
]

const issueDetail: IssueDetailType = {
  id: 'iss-q',
  project_id: 'p1',
  project_name: 'alpha-project',
  linear_issue_id: 'lin-q',
  identifier: 'ALPHA-1',
  title: 'Queued task',
  description: 'A task in the queue',
  state: 'queued',
  build_active: false,
  stories: [],
  integration_tests: [],
  created_at: '2025-03-01T10:00:00Z',
  updated_at: '2025-03-01T10:00:00Z',
  activity: [],
  build_activity: [],
}

// ── Helpers ────────────────────────────────────────────────────────

function renderApp(initialRoute = '/') {
  return render(
    <MemoryRouter initialEntries={[initialRoute]}>
      <Routes>
        <Route path="/" element={<Dashboard />} />
        <Route path="/projects/:id" element={<ProjectDetail />} />
        <Route path="/issues/:id" element={<IssueDetail />} />
      </Routes>
    </MemoryRouter>,
  )
}

// ── Setup ──────────────────────────────────────────────────────────

beforeEach(() => {
  vi.mocked(fetchProjects).mockResolvedValue(projects)
  vi.mocked(fetchIssues).mockResolvedValue(issuesMixed.filter(i => !['completed', 'failed', 'dismissed'].includes(i.state)))
  vi.mocked(fetchIssuesByProject).mockResolvedValue(issuesMixed)
  vi.mocked(fetchIssue).mockResolvedValue(issueDetail)
  vi.mocked(useWebSocket).mockImplementation(() => {})
})

// ── IT-001: End-to-end navigation Dashboard → ProjectDetail → IssueDetail ──

describe('IT-001: End-to-end navigation flow', () => {
  it('navigates from Dashboard to ProjectDetail to IssueDetail', async () => {
    const user = userEvent.setup()
    renderApp('/')

    // Step 1-2: Dashboard renders with project card
    await waitFor(() => {
      expect(screen.getByText('alpha-project')).toBeInTheDocument()
    })

    // Step 3: Click on project card
    const projectLink = screen.getByText('alpha-project').closest('a')!
    await user.click(projectLink)

    // Step 4: Navigated to ProjectDetail — project name displayed
    await waitFor(() => {
      expect(screen.getByText('acme/alpha')).toBeInTheDocument()
    })
    expect(screen.getByText('alpha-project')).toBeInTheDocument()

    // Step 5: Active issues are visible
    expect(screen.getByText('Queued task')).toBeInTheDocument()
    expect(screen.getByText('Building feature')).toBeInTheDocument()
    expect(screen.getByText('In review PR')).toBeInTheDocument()

    // Step 6: Completed issues are in a collapsed details section
    const completedSection = screen.getByTestId('completed-section')
    expect(completedSection).not.toHaveAttribute('open')

    // Step 7: Click to expand completed section
    const summary = completedSection.querySelector('summary')!
    await user.click(summary)

    // Step 8: Completed issues are now visible after expansion
    expect(within(completedSection).getByText('Completed work')).toBeInTheDocument()
    expect(within(completedSection).getByText('Failed deployment')).toBeInTheDocument()
    expect(within(completedSection).getByText('Dismissed ticket')).toBeInTheDocument()

    // Step 9: Click on an issue row
    const issueLink = screen.getByText('ALPHA-1').closest('a')!
    await user.click(issueLink)

    // Step 10: Navigated to IssueDetail
    await waitFor(() => {
      expect(fetchIssue).toHaveBeenCalledWith('iss-q')
    })
    await waitFor(() => {
      expect(screen.getByText('Queued task')).toBeInTheDocument()
    })
  })
})

// ── IT-002: Issue grouping by state ────────────────────────────────

describe('IT-002: ProjectDetail groups issues correctly by state', () => {
  it('groups active and completed issues into correct sections', async () => {
    renderApp('/projects/p1')

    await waitFor(() => {
      expect(screen.getByText('alpha-project')).toBeInTheDocument()
    })

    // Active section: queued, building, in_review
    expect(screen.getByText('Queued task')).toBeInTheDocument()
    expect(screen.getByText('Building feature')).toBeInTheDocument()
    expect(screen.getByText('In review PR')).toBeInTheDocument()

    // Completed section: completed, failed, dismissed
    const completedSection = screen.getByTestId('completed-section')
    expect(within(completedSection).getByText('Completed work')).toBeInTheDocument()
    expect(within(completedSection).getByText('Failed deployment')).toBeInTheDocument()
    expect(within(completedSection).getByText('Dismissed ticket')).toBeInTheDocument()

    // Completed section is collapsed by default
    expect(completedSection).not.toHaveAttribute('open')

    // Issue rows display identifier, title, and state badge
    expect(screen.getByText('ALPHA-1')).toBeInTheDocument()
    expect(screen.getByText('ALPHA-2')).toBeInTheDocument()
    expect(screen.getByText('ALPHA-3')).toBeInTheDocument()

    // State badges rendered
    expect(screen.getByText('queued')).toBeInTheDocument()
    expect(screen.getByText('building')).toBeInTheDocument()
    expect(screen.getByText('in review')).toBeInTheDocument()
  })

  it('includes all completed-category states in the completed section', async () => {
    renderApp('/projects/p1')

    await waitFor(() => {
      expect(screen.getByText('alpha-project')).toBeInTheDocument()
    })

    const completedSection = screen.getByTestId('completed-section')
    // Verify state badges for completed items
    expect(within(completedSection).getByText('completed')).toBeInTheDocument()
    expect(within(completedSection).getByText('failed')).toBeInTheDocument()
    expect(within(completedSection).getByText('dismissed')).toBeInTheDocument()
  })
})

// ── IT-003: Loading, error, and empty states ───────────────────────

describe('IT-003: ProjectDetail loading, error, and empty states', () => {
  it('shows loading indicator before API resolves', () => {
    vi.mocked(fetchProjects).mockReturnValue(new Promise(() => {}))
    renderApp('/projects/p1')
    expect(screen.getByText('Loading...')).toBeInTheDocument()
  })

  it('shows error message when API rejects', async () => {
    vi.mocked(fetchProjects).mockRejectedValue(new Error('Server unreachable'))
    renderApp('/projects/p1')
    await waitFor(() => {
      expect(screen.getByText('Error: Server unreachable')).toBeInTheDocument()
    })
  })

  it('shows empty state when project has zero issues', async () => {
    vi.mocked(fetchIssuesByProject).mockResolvedValue([])
    renderApp('/projects/p1')
    await waitFor(() => {
      expect(screen.getByText('alpha-project')).toBeInTheDocument()
    })
    expect(screen.getByText('No issues found for this project')).toBeInTheDocument()
  })
})

// ── IT-004: WebSocket real-time updates ────────────────────────────

describe('IT-004: WebSocket updates refresh ProjectDetail issue list', () => {
  it('re-fetches issues after a WebSocket message', async () => {
    let wsCallback: (msg: unknown) => void = () => {}
    vi.mocked(useWebSocket).mockImplementation((cb) => {
      wsCallback = cb as (msg: unknown) => void
    })

    renderApp('/projects/p1')

    // Step 1-2: Initial render
    await waitFor(() => {
      expect(screen.getByText('alpha-project')).toBeInTheDocument()
    })
    expect(screen.getByText('Queued task')).toBeInTheDocument()

    // Clear call count to isolate WebSocket-triggered re-fetch
    vi.mocked(fetchIssuesByProject).mockClear()

    // Prepare updated data for after WS message
    const updatedIssues: Issue[] = [
      ...issuesMixed,
      {
        id: 'iss-new',
        project_id: 'p1',
        identifier: 'ALPHA-7',
        title: 'Newly added issue',
        state: 'refining',
        build_active: false,
        created_at: '2025-03-01T13:00:00Z',
        updated_at: '2025-03-01T13:00:00Z',
      },
    ]
    vi.mocked(fetchIssuesByProject).mockResolvedValue(updatedIssues)

    // Step 3: Simulate WebSocket message
    wsCallback({ type: 'state_change', payload: {}, timestamp: new Date().toISOString() })

    // Step 4: fetchIssuesByProject called again
    await waitFor(() => {
      expect(fetchIssuesByProject).toHaveBeenCalledWith('p1')
    })

    // Step 5: UI updates with new data
    await waitFor(() => {
      expect(screen.getByText('Newly added issue')).toBeInTheDocument()
    })
  })
})

// ── IT-005: StateBadge shared module across all pages ──────────────

describe('IT-005: StateBadge shared module works across all pages', () => {
  it('renders StateBadge correctly on Dashboard for state breakdowns', async () => {
    renderApp('/')
    await waitFor(() => {
      expect(screen.getByText('alpha-project')).toBeInTheDocument()
    })
    // Dashboard shows state breakdown as "1 queued", "1 building" badges
    expect(screen.getByText('1 queued')).toBeInTheDocument()
    expect(screen.getByText('1 building')).toBeInTheDocument()
  })

  it('renders StateBadge correctly on IssueDetail', async () => {
    const buildingIssue: IssueDetailType = {
      ...issueDetail,
      state: 'building',
      build_active: true,
      activity: [],
      build_activity: [],
    }
    vi.mocked(fetchIssue).mockResolvedValue(buildingIssue)
    renderApp('/issues/iss-q')

    await waitFor(() => {
      expect(screen.getByText('Queued task')).toBeInTheDocument()
    })
    // StateBadge renders the state text
    expect(screen.getAllByText('building').length).toBeGreaterThanOrEqual(1)
  })

  it('renders StateBadge correctly on ProjectDetail for each issue row', async () => {
    renderApp('/projects/p1')
    await waitFor(() => {
      expect(screen.getByText('alpha-project')).toBeInTheDocument()
    })
    // Active issues have state badges
    expect(screen.getByText('queued')).toBeInTheDocument()
    expect(screen.getByText('building')).toBeInTheDocument()
    expect(screen.getByText('in review')).toBeInTheDocument()
  })

  it('all three pages import StateBadge from the same shared module', async () => {
    const fs = await import('fs')
    const path = await import('path')

    // Verify the shared module exports StateBadge and STATE_COLORS
    const { StateBadge, STATE_COLORS } = await import('./components/StateBadge')
    expect(StateBadge).toBeDefined()
    expect(typeof StateBadge).toBe('function')
    expect(STATE_COLORS).toBeDefined()
    expect(STATE_COLORS.building).toBe('#f59e0b')

    const srcDir = path.resolve(__dirname)

    // Verify Dashboard source imports from shared module
    const dashSrc = fs.readFileSync(path.join(srcDir, 'pages', 'Dashboard.tsx'), 'utf-8')
    expect(dashSrc).toContain("from '../components/StateBadge'")

    // Verify IssueDetail source imports from shared module
    const issueSrc = fs.readFileSync(path.join(srcDir, 'pages', 'IssueDetail.tsx'), 'utf-8')
    expect(issueSrc).toContain("from '../components/StateBadge'")

    // Verify ProjectDetail source imports from shared module
    const projSrc = fs.readFileSync(path.join(srcDir, 'pages', 'ProjectDetail.tsx'), 'utf-8')
    expect(projSrc).toContain("from '../components/StateBadge'")
  })
})
