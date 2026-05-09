import { test, expect } from '@playwright/test';
import { loginAsUser } from '../helpers/ui';
import { loadAccounts } from '../helpers/accounts';
import { ApiClient } from '../helpers/api';

/**
 * Sidebar / shell UX regressions:
 *  1. Logout stays reachable on long pages because the shell pins the sidebar.
 *  2. The collapse toggle persists across reload via localStorage.
 *  3. The setup-required pulse clears without a manual refresh after a
 *     credential is provisioned (driven by the gatewayllm:setup-changed event).
 */

test('logout button stays reachable on a long page', async ({ page }) => {
  const accounts = loadAccounts();
  await loginAsUser(page, accounts.orgAdmin.token, {
    id: accounts.orgAdmin.userId,
    email: accounts.orgAdmin.email,
    role: 'org_admin',
    org_id: accounts.orgAdmin.orgId,
  });
  await page.goto('/');

  await page.setViewportSize({ width: 1280, height: 720 });
  // Scroll the main content area — the sidebar (and logout) must stay put.
  await page.evaluate(() => {
    const main = document.querySelector('main');
    if (main) main.scrollTop = main.scrollHeight;
  });

  const logout = page.getByRole('button', { name: /Logout/i }).first();
  await expect(logout).toBeVisible();
});

test('sidebar collapse state persists across reload', async ({ page }) => {
  const accounts = loadAccounts();
  await loginAsUser(page, accounts.orgAdmin.token, {
    id: accounts.orgAdmin.userId,
    email: accounts.orgAdmin.email,
    role: 'org_admin',
    org_id: accounts.orgAdmin.orgId,
  });
  await page.goto('/');

  const toggle = page.getByRole('button', { name: /Collapse sidebar|Expand sidebar/i }).first();
  if (!(await toggle.isVisible().catch(() => false))) {
    test.skip(true, 'sidebar toggle not exposed in this build');
  }

  await toggle.click();
  // Persist happens synchronously via localStorage.setItem.
  const stored = await page.evaluate(() => localStorage.getItem('gatewayllm_sidebar_collapsed'));
  expect(stored).toBeTruthy();

  await page.reload();
  const storedAfter = await page.evaluate(() => localStorage.getItem('gatewayllm_sidebar_collapsed'));
  expect(storedAfter).toBe(stored);
});

test('provider-key pulse clears after a credential is created', async ({ page }) => {
  const accounts = loadAccounts();
  const client = new ApiClient(accounts.orgAdmin.token);

  await loginAsUser(page, accounts.orgAdmin.token, {
    id: accounts.orgAdmin.userId,
    email: accounts.orgAdmin.email,
    role: 'org_admin',
    org_id: accounts.orgAdmin.orgId,
  });
  await page.goto('/credentials');
  await page.waitForLoadState('networkidle').catch(() => {});

  // Provision via the API so we don't depend on the exact form markup.
  const cred = await client.request<any>('POST', '/v1/management/credentials', {
    name: `ui-sidebar-${Date.now()}`,
    provider: 'openai',
    api_key: 'sk-uitest-placeholder',
  });

  // Force the setup-state listener to refetch. The UI also fires this event
  // on mutations; dispatching it here simulates an external refresh
  // deterministically for the test.
  await page.evaluate(() => {
    window.dispatchEvent(new Event('gatewayllm:setup-changed'));
  });

  try {
    // The pulse attached to the Provider Keys link should clear.
    await expect
      .poll(async () => {
        return page.locator('a[href="/credentials"] [aria-label="Setup required"]').count();
      }, { timeout: 10_000 })
      .toBe(0);
  } finally {
    await client.request('DELETE', `/v1/management/credentials/${cred.id}`).catch(() => {});
  }
});
