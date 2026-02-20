export interface Project {
  id: string
  name: string
  local_path: string
  github_owner: string
  github_repo: string
  active_issue_count: number
  state_breakdown: Record<string, number>
}

export interface Issue {
  id: string
  project_id: string
  identifier: string
  title: string
  state: string
  pr_number?: number
  pr_url?: string
  error_message?: string
  workspace_name?: string
  branch_name?: string
  build_active: boolean
  model?: string
  created_at: string
  updated_at: string
}

export interface Activity {
  id: string
  issue_id: string
  event_type: string
  from_state?: string
  to_state?: string
  detail?: string
  created_at: string
}

export interface StoryInfo {
  id: string
  title: string
  passes: boolean
}

export interface IntegrationTestInfo {
  id: string
  description: string
  passes: boolean
}

const BASE = '/api'

async function fetchJSON<T>(path: string): Promise<T> {
  const resp = await fetch(BASE + path)
  if (!resp.ok) {
    const body = await resp.json().catch(() => ({ error: resp.statusText }))
    throw new Error(body.error || resp.statusText)
  }
  return resp.json()
}

export function fetchProjects(): Promise<Project[]> {
  return fetchJSON<Project[]>('/projects')
}

export function fetchIssues(): Promise<Issue[]> {
  return fetchJSON<Issue[]>('/issues')
}

export function fetchIssuesByProject(projectId: string): Promise<Issue[]> {
  return fetchJSON<Issue[]>(`/issues?project_id=${projectId}`)
}

export interface IssueDetail {
  id: string
  project_id: string
  project_name: string
  linear_issue_id: string
  identifier: string
  title: string
  description: string
  state: string
  plan_text?: string
  workspace_name?: string
  branch_name?: string
  pr_number?: number
  pr_url?: string
  error_message?: string
  build_active: boolean
  model?: string
  stories: StoryInfo[]
  integration_tests: IntegrationTestInfo[]
  current_story?: string
  iteration?: number
  max_iterations?: number
  created_at: string
  updated_at: string
  activity: Activity[]
  build_activity: Activity[]
}

export function fetchIssue(id: string): Promise<IssueDetail> {
  return fetchJSON<IssueDetail>(`/issues/${id}?build_limit=200&timeline_limit=50`)
}

async function postJSON<T>(path: string): Promise<T> {
  const resp = await fetch(BASE + path, { method: 'POST' })
  if (!resp.ok) {
    const body = await resp.json().catch(() => ({ error: resp.statusText }))
    throw new Error(body.error || resp.statusText)
  }
  return resp.json()
}

export function resumeIssue(id: string): Promise<{ status: string; state: string }> {
  return postJSON(`/issues/${id}/resume`)
}

export function retryIssue(id: string): Promise<{ status: string; state: string }> {
  return postJSON(`/issues/${id}/retry`)
}

export async function deleteIssue(id: string): Promise<{ status: string }> {
  const resp = await fetch(BASE + `/issues/${id}`, { method: 'DELETE' })
  if (!resp.ok) {
    const body = await resp.json().catch(() => ({ error: resp.statusText }))
    throw new Error(body.error || resp.statusText)
  }
  return resp.json()
}

export interface CCUsageLine {
  label: string
  percentage: number
  reset_duration: string
}

export interface CCUsageGroup {
  group_label: string
  lines: CCUsageLine[]
}

export interface CCUsage {
  available: boolean
  groups?: CCUsageGroup[]
}

export function fetchCCUsage(): Promise<CCUsage> {
  return fetchJSON<CCUsage>('/cc-usage')
}
