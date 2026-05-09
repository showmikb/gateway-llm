import { test, expect } from '@playwright/test';
import { loginAsUser } from '../helpers/ui';
import { loadAccounts } from '../helpers/accounts';

/**
 * Follows the "happy path" a new org_admin is guided through after sign-up:
 * see the dashboard setup hints, jump to Provider Keys, Model Routes, and
 * API Keys via the sidebar, and confirm each page reacts to a logged-in
 * session. The specific add/delete flows for each resource live in their
 * own spec files; this test guards the *navigation* story.
 */

test('org_admin walks the dashboard -> credentials -> models -> keys path', async ({ page }) => {
  const accounts = loadAccounts();
  await loginAsUser(page, accounts.orgAdmin.token, {
    id: accounts.orgAdmin.userId,
    email: accounts.orgAdmin.email,
    role: 'org_admin',
    org_id: accounts.orgAdmin.orgId,
  });

  await page.goto('/');
  await expect(page.locator('aside')).toBeVisible();
  await expect(page.getByRole('link', { name: /Model Routes/ })).toBeVisible();
  await expect(page.getByRole('link', { name: /Provider Keys/ })).toBeVisible();
  await expect(page.getByRole('link', { name: /API Keys/ })).toBeVisible();

  // Provider Keys
  await page.getByRole('link', { name: /Provider Keys/ }).click();
  await expect(page).toHaveURL(/\/credentials$/);
  await expect(page.getByRole('heading', { name: /Provider Keys/ })).toBeVisible();

  // Model Routes
  await page.getByRole('link', { name: /Model Routes/ }).click();
  await expect(page).toHaveURL(/\/models$/);
  await expect(page.getByRole('heading', { name: /Model Routes/ })).toBeVisible();

  // API Keys
  await page.getByRole('link', { name: /API Keys/ }).click();
  await expect(page).toHaveURL(/\/keys$/);
  await expect(page.getByRole('heading', { name: /API Keys/ })).toBeVisible();
});
