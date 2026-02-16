import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, waitFor } from '@testing-library/react'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import IssueDetail from './IssueDetail'
import type { IssueDetail as IssueDetailType } from '../api'

vi.mock('../api', () => ({
  fetchIssue: vi.fn(),
  resumeIssue: vi.fn(),
  retryIssue: vi.fn(),
  deleteIssue: vi.fn(),
}))

vi.mock('../useWebSocket', () => ({
  useWebSocket: vi.fn(),
}))

import { fetchIssue, resumeIssue, retryIssue, deleteIssue } from '../api'

const mockIssue: IssueDetailType = {
  id: 'iss1',
  project_id: 'p1',
  project_name: 'my-project',
  linear_issue_id: 'lin-1',
  identifier: 'PROJ-1',
  title: 'Add user avatars',
  description: 'Add avatar support to user profiles',
  state: 'building',
  workspace_name: 'proj-1',
  branch_name: 'autoralph/proj-1',
  build_active: true,
  stories: [
    { id: 'US-001', title: 'Upload avatar', passes: true },
    { id: 'US-002', title: 'Display avatar', passes: false },
  ],
  integration_tests: [
    { id: 'IT-001', description: 'Full avatar flow', passes: false },
  ],
  current_story: 'US-002',
  iteration: 2,
  max_iterations: 5,
  created_at: '2025-02-11T12:00:00Z',
  updated_at: '2025-02-11T12:30:00Z',
  activity: [
    {
      id: 'act1',
      issue_id: 'iss1',
      event_type: 'state_change',
      from_state: 'queued',
      to_state: 'refining',
      detail: 'Auto refinement',
      created_at: '2025-02-11T12:00:00Z',
    },
    {
      id: 'act2',
      issue_id: 'iss1',
      event_type: 'state_change',
      from_state: 'refining',
      to_state: 'building',
      detail: 'Plan approved',
      created_at: '2025-02-11T12:15:00Z',
    },
  ],
  build_activity: [
    {
      id: 'act3',
      issue_id: 'iss1',
      event_type: 'build_event',
      detail: 'Story US-001: Upload avatar',
      created_at: '2025-02-11T12:20:00Z',
    },
    {
      id: 'act4',
      issue_id: 'iss1',
      event_type: 'build_event',
      detail: 'Story US-002: Display avatar',
      created_at: '2025-02-11T12:26:00Z',
    },
  ],
}

function renderIssueDetail(id = 'iss1') {
  return render(
    <MemoryRouter initialEntries={[`/issues/${id}`]}>
      <Routes>
        <Route path="/issues/:id" element={<IssueDetail />} />
      </Routes>
    </MemoryRouter>,
  )
}

beforeEach(() => {
  vi.mocked(fetchIssue).mockResolvedValue(mockIssue)
  vi.mocked(resumeIssue).mockResolvedValue({ status: 'resumed', state: 'building' })
  vi.mocked(retryIssue).mockResolvedValue({ status: 'retrying', state: 'building' })
  vi.mocked(deleteIssue).mockResolvedValue({ status: 'deleted' })
})

describe('IssueDetail', () => {
  it('shows loading state initially', () => {
    vi.mocked(fetchIssue).mockReturnValue(new Promise(() => {}))
    renderIssueDetail()
    expect(screen.getByText('Loading...')).toBeInTheDocument()
  })

  it('renders issue header with identifier, title, state, project name', async () => {
    renderIssueDetail()
    await waitFor(() => {
      expect(screen.getByText('PROJ-1')).toBeInTheDocument()
    })
    expect(screen.getByText('Add user avatars')).toBeInTheDocument()
    // "building" appears in header badge and timeline badges
    expect(screen.getAllByText('building').length).toBeGreaterThanOrEqual(1)
    expect(screen.getByText('my-project')).toBeInTheDocument()
  })

  it('renders activity timeline', async () => {
    renderIssueDetail()
    await waitFor(() => {
      expect(screen.getByText('Auto refinement')).toBeInTheDocument()
    })
    expect(screen.getByText('Plan approved')).toBeInTheDocument()
  })

  it('shows Agent Logs when build events exist', async () => {
    renderIssueDetail()
    await waitFor(() => {
      expect(screen.getByText('Agent Logs')).toBeInTheDocument()
    })
  })

  it('shows user stories list with pass/fail status', async () => {
    renderIssueDetail()
    await waitFor(() => {
      expect(screen.getByText('Upload avatar')).toBeInTheDocument()
    })
    expect(screen.getByText('Display avatar')).toBeInTheDocument()
    // Story count header
    expect(screen.getByText(/User Stories/)).toBeInTheDocument()
  })

  it('shows integration tests list', async () => {
    renderIssueDetail()
    await waitFor(() => {
      expect(screen.getByText('Full avatar flow')).toBeInTheDocument()
    })
    expect(screen.getByText(/Integration Tests/)).toBeInTheDocument()
  })

  it('highlights current story as in progress', async () => {
    renderIssueDetail()
    await waitFor(() => {
      expect(screen.getByText('IN PROGRESS')).toBeInTheDocument()
    })
  })

  it('does not show in progress badge when not building', async () => {
    const reviewIssue = {
      ...mockIssue,
      state: 'in_review',
      current_story: 'US-002',
      build_active: false,
      activity: [],
      build_activity: [],
    }
    vi.mocked(fetchIssue).mockResolvedValue(reviewIssue)

    renderIssueDetail()
    await waitFor(() => {
      expect(screen.getByText('Display avatar')).toBeInTheDocument()
    })
    expect(screen.queryByText('IN PROGRESS')).not.toBeInTheDocument()
  })

  it('shows iteration number', async () => {
    renderIssueDetail()
    await waitFor(() => {
      expect(screen.getByText(/Iteration 2\/5/)).toBeInTheDocument()
    })
  })

  it('shows Resume button when paused', async () => {
    const pausedIssue = { ...mockIssue, state: 'paused', build_active: false, activity: [], build_activity: [], stories: [], integration_tests: [] }
    vi.mocked(fetchIssue).mockResolvedValue(pausedIssue)

    renderIssueDetail()
    await waitFor(() => {
      expect(screen.getByText('Resume')).toBeInTheDocument()
    })
  })

  it('shows Retry button when failed', async () => {
    const failedIssue = {
      ...mockIssue,
      state: 'failed',
      error_message: 'Build timed out',
      build_active: false,
      activity: [],
      build_activity: [],
      stories: [],
      integration_tests: [],
    }
    vi.mocked(fetchIssue).mockResolvedValue(failedIssue)

    renderIssueDetail()
    await waitFor(() => {
      expect(screen.getByText('Retry')).toBeInTheDocument()
    })
    expect(screen.getByText('Build timed out')).toBeInTheDocument()
  })

  it('shows Delete button', async () => {
    renderIssueDetail()
    await waitFor(() => {
      expect(screen.getByText('Delete')).toBeInTheDocument()
    })
  })

  it('shows PR link when available', async () => {
    const reviewIssue = {
      ...mockIssue,
      state: 'in_review',
      pr_number: 42,
      pr_url: 'https://github.com/octocat/hello-world/pull/42',
      build_active: false,
      activity: [],
      build_activity: [],
      stories: [],
      integration_tests: [],
    }
    vi.mocked(fetchIssue).mockResolvedValue(reviewIssue)

    renderIssueDetail()
    await waitFor(() => {
      expect(screen.getByText('PR #42')).toBeInTheDocument()
    })
    expect(screen.getByText('PR #42')).toHaveAttribute(
      'href',
      'https://github.com/octocat/hello-world/pull/42',
    )
  })

  it('shows error state on API failure', async () => {
    vi.mocked(fetchIssue).mockRejectedValue(new Error('not found'))
    renderIssueDetail()
    await waitFor(() => {
      expect(screen.getByText('Error: not found')).toBeInTheDocument()
    })
  })

  it('shows Agent Logs for non-building states when build events exist', async () => {
    const completedIssue = {
      ...mockIssue,
      state: 'completed',
      build_active: false,
      activity: [],
      build_activity: [
        {
          id: 'act-b1',
          issue_id: 'iss1',
          event_type: 'build_event',
          detail: 'Story US-001: Upload avatar',
          created_at: '2025-02-11T12:20:00Z',
        },
      ],
      stories: [],
      integration_tests: [],
    }
    vi.mocked(fetchIssue).mockResolvedValue(completedIssue)

    renderIssueDetail()
    await waitFor(() => {
      expect(screen.getByText('Agent Logs')).toBeInTheDocument()
    })
  })

  it('hides Agent Logs when no build events exist', async () => {
    const queuedIssue = {
      ...mockIssue,
      state: 'queued',
      build_active: false,
      activity: [
        {
          id: 'act-sc1',
          issue_id: 'iss1',
          event_type: 'state_change',
          from_state: 'queued',
          to_state: 'refining',
          detail: 'Auto refinement',
          created_at: '2025-02-11T12:00:00Z',
        },
      ],
      build_activity: [],
      stories: [],
      integration_tests: [],
    }
    vi.mocked(fetchIssue).mockResolvedValue(queuedIssue)

    renderIssueDetail()
    await waitFor(() => {
      expect(screen.getByText('Add user avatars')).toBeInTheDocument()
    })
    expect(screen.queryByText('Agent Logs')).not.toBeInTheDocument()
  })

  it('shows empty timeline message when no activity', async () => {
    const emptyIssue = { ...mockIssue, state: 'queued', build_active: false, activity: [], build_activity: [], stories: [], integration_tests: [] }
    vi.mocked(fetchIssue).mockResolvedValue(emptyIssue)

    renderIssueDetail()
    await waitFor(() => {
      expect(screen.getByText('No activity yet')).toBeInTheDocument()
    })
  })

  it('renders back link to dashboard', async () => {
    renderIssueDetail()
    await waitFor(() => {
      expect(screen.getByText(/Dashboard/)).toBeInTheDocument()
    })
  })

  it('renders BuildLog from build_activity (not activity)', async () => {
    const issueWithSplitActivity = {
      ...mockIssue,
      activity: [
        {
          id: 'act-sc',
          issue_id: 'iss1',
          event_type: 'state_change',
          from_state: 'queued',
          to_state: 'building',
          detail: 'Started building',
          created_at: '2025-02-11T12:00:00Z',
        },
      ],
      build_activity: [
        {
          id: 'act-story',
          issue_id: 'iss1',
          event_type: 'build_event',
          detail: 'Story US-001: Upload avatar',
          created_at: '2025-02-11T12:10:00Z',
        },
        {
          id: 'act-tool',
          issue_id: 'iss1',
          event_type: 'build_event',
          detail: '→ Read file.go',
          created_at: '2025-02-11T12:11:00Z',
        },
      ],
    }
    vi.mocked(fetchIssue).mockResolvedValue(issueWithSplitActivity)

    renderIssueDetail()
    await waitFor(() => {
      expect(screen.getByText('Agent Logs')).toBeInTheDocument()
    })
    const agentLogs = screen.getByText('Agent Logs').closest('section')!
    expect(agentLogs.textContent).toContain('Story US-001: Upload avatar')
    expect(agentLogs.textContent).toContain('→ Read file.go')
    // Timeline should only show state_change, no build events
    const timeline = screen.getByText('Timeline').closest('section')!
    expect(timeline.textContent).toContain('Started building')
    expect(timeline.textContent).not.toContain('Story US-001: Upload avatar')
    expect(timeline.textContent).not.toContain('→ Read file.go')
  })

  it('renders Timeline directly from activity without filtering', async () => {
    const issueTimelineOnly = {
      ...mockIssue,
      activity: [
        {
          id: 'act-sc1',
          issue_id: 'iss1',
          event_type: 'state_change',
          from_state: 'queued',
          to_state: 'building',
          detail: 'Started building',
          created_at: '2025-02-11T12:00:00Z',
        },
        {
          id: 'act-pr',
          issue_id: 'iss1',
          event_type: 'pr_created',
          detail: 'PR #42 created',
          created_at: '2025-02-11T12:05:00Z',
        },
      ],
      build_activity: [],
    }
    vi.mocked(fetchIssue).mockResolvedValue(issueTimelineOnly)

    renderIssueDetail()
    await waitFor(() => {
      expect(screen.getByText('Started building')).toBeInTheDocument()
    })
    expect(screen.getByText('PR #42 created')).toBeInTheDocument()
    expect(screen.queryByText('Agent Logs')).not.toBeInTheDocument()
  })
})
