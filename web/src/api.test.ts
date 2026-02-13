import { describe, it, expect, vi, beforeEach } from 'vitest'
import { fetchIssuesByProject } from './api'

const mockFetch = vi.fn()
globalThis.fetch = mockFetch

beforeEach(() => {
  mockFetch.mockReset()
})

describe('fetchIssuesByProject', () => {
  it('calls GET /api/issues?project_id=<projectId> and returns issues', async () => {
    const issues = [
      {
        id: 'iss1',
        project_id: 'p1',
        identifier: 'PROJ-1',
        title: 'Test issue',
        state: 'building',
        build_active: true,
        created_at: '2025-01-01T00:00:00Z',
        updated_at: '2025-01-01T00:00:00Z',
      },
    ]
    mockFetch.mockResolvedValueOnce({
      ok: true,
      json: () => Promise.resolve(issues),
    })

    const result = await fetchIssuesByProject('p1')

    expect(mockFetch).toHaveBeenCalledWith('/api/issues?project_id=p1')
    expect(result).toEqual(issues)
  })

  it('throws an error when the request fails', async () => {
    mockFetch.mockResolvedValueOnce({
      ok: false,
      statusText: 'Internal Server Error',
      json: () => Promise.resolve({ error: 'something went wrong' }),
    })

    await expect(fetchIssuesByProject('bad-id')).rejects.toThrow('something went wrong')
  })

  it('throws statusText when error body cannot be parsed', async () => {
    mockFetch.mockResolvedValueOnce({
      ok: false,
      statusText: 'Not Found',
      json: () => Promise.reject(new Error('invalid json')),
    })

    await expect(fetchIssuesByProject('missing')).rejects.toThrow('Not Found')
  })
})
