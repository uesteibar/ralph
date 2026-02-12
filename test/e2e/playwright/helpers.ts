import { expect, type Page } from '@playwright/test';

const BASE_URL = 'http://127.0.0.1:7749';

/**
 * Wait for the API to be healthy by polling /api/status.
 */
export async function waitForHealth(page: Page, timeoutMs = 10_000): Promise<void> {
  const start = Date.now();
  while (Date.now() - start < timeoutMs) {
    try {
      const response = await page.request.get(`${BASE_URL}/api/status`);
      if (response.ok()) return;
    } catch {
      // Server not yet available — retry.
    }
    await page.waitForTimeout(200);
  }
  throw new Error(`autoralph did not become healthy within ${timeoutMs}ms`);
}

/**
 * Wait for an issue to reach a specific state by polling /api/issues/:id.
 */
export async function waitForIssueState(
  page: Page,
  issueId: string,
  expectedState: string,
  timeoutMs = 30_000,
): Promise<void> {
  const start = Date.now();
  while (Date.now() - start < timeoutMs) {
    try {
      const response = await page.request.get(`${BASE_URL}/api/issues/${issueId}`);
      if (response.ok()) {
        const data = await response.json();
        if (data.state === expectedState) return;
      }
    } catch {
      // Retry on error.
    }
    await page.waitForTimeout(500);
  }
  throw new Error(`Issue ${issueId} did not reach state '${expectedState}' within ${timeoutMs}ms`);
}

/**
 * Verify the dashboard loads and shows expected content.
 */
export async function verifyDashboard(page: Page): Promise<void> {
  await page.goto('/');
  await expect(page.locator('body')).not.toBeEmpty();
}

/**
 * Navigate to an issue detail page.
 */
export async function navigateToIssue(page: Page, issueId: string): Promise<void> {
  await page.goto(`/issues/${issueId}`);
  await expect(page.locator('body')).not.toBeEmpty();
}
