import { test, expect } from '@playwright/test';
import { loginAsUser } from '../helpers/ui';
import { loadAccounts } from '../helpers/accounts';

/**
 * Sidebar must hide admin-only items for members + viewers, and the
 * "contact the team" overlay must appear when a non-admin lands on a
 * restricted page directly by URL.
 */

test('member sees a restricted sidebar', async ({ page }) => {
  const accounts = loadAccounts();
  await loginAsUser(page, accounts.member.token, {
    id: accounts.member.userId,
    email: accounts.member.email,
    role: 'member',
    org_id: accounts.orgAdmin.orgId,
  });
  await page.goto('/');
  await expect(page.getByRole('link', { name: /Model Routes/ })).toBeVisible();
  await expect(page.getByRole('link', { name: /API Keys/ })).toBeVisible();
  await expect(page.getByRole('link', { name: /Users/ })).toHaveCount(0);
  await expect(page.getByRole('link', { name: /Provider Keys/ })).toHaveCount(0);
  await expect(page.getByRole('link', { name: /Organizations/ })).toHaveCount(0);
});

test('viewer sees the strictest sidebar', async ({ page }) => {
  const accounts = loadAccounts();
  await loginAsUser(page, accounts.viewer.token, {
    id: accounts.viewer.userId,
    email: accounts.viewer.email,
    role: 'viewer',
    org_id: accounts.orgAdmin.orgId,
  });
  await page.goto('/');
  await expect(page.getByRole('link', { name: /Dashboard/ })).toBeVisible();
  await expect(page.getByRole('link', { name: /Teams/ })).toHaveCount(0);
  await expect(page.getByRole('link', { name: /Users/ })).toHaveCount(0);
  await expect(page.getByRole('link', { name: /Provider Keys/ })).toHaveCount(0);
});

test('viewer landing on /keys sees read-only banner', async ({ page }) => {
  const accounts = loadAccounts();
  await loginAsUser(page, accounts.viewer.token, {
    id: accounts.viewer.userId,
    email: accounts.viewer.email,
    role: 'viewer',
    org_id: accounts.orgAdmin.orgId,
  });
  await page.goto('/keys');
  // Two elements mention "Read-only access" (page header subtitle + the info
  // banner). We care about the banner specifically.
  await expect(page.locator('div', { hasText: /Read-only access — contact an admin/i }).first()).toBeVisible();
});
