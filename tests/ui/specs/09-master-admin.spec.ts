import { test, expect } from '@playwright/test';
import { loginAsMaster } from '../helpers/ui';
import { authenticatedRoutes } from '../helpers/routes';
import { loadAccounts } from '../helpers/accounts';

/**
 * Master key should be a superset view of everything: full sidebar,
 * Organizations page reachable, and both the ui-test primary org and
 * secondary org visible (cross-tenant view works).
 */

test('master key sees the full sidebar including Organizations', async ({ page }) => {
  await loginAsMaster(page);
  await page.goto('/');
  await expect(page.getByRole('link', { name: /Organizations/ })).toBeVisible();
  await expect(page.getByRole('link', { name: /Users/ })).toBeVisible();
  await expect(page.getByRole('link', { name: /Provider Keys/ })).toBeVisible();
  await expect(page.getByText(/Master Key/).first()).toBeVisible();
});

test('master key sees every provisioned org on /organizations', async ({ page }) => {
  const accounts = loadAccounts();
  await loginAsMaster(page);
  await page.goto('/organizations');
  // The orgs page renders names + slugs rather than UUIDs. Assert on the
  // secondary org's deterministic name prefix, plus the primary org admin's
  // email (which appears in its auto-generated workspace name).
  await expect(page.locator('body')).toContainText(/ui-tests-secondary-/);
  // Primary org name is derived from the admin's email local-part.
  const localPart = accounts.orgAdmin.email.split('@')[0];
  await expect(page.locator('body')).toContainText(new RegExp(localPart.slice(0, 20)));
});

test('master key can open every authenticated page', async ({ page }) => {
  await loginAsMaster(page);
  for (const p of authenticatedRoutes()) {
    const resp = await page.goto(p.route);
    expect(resp!.status(), `master key blocked on ${p.route}`).toBeLessThan(400);
    expect(new URL(page.url()).pathname, `master redirected on ${p.route}`).not.toMatch(/\/login/);
  }
});
