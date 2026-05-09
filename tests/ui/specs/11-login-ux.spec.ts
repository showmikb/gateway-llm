import { test, expect } from '@playwright/test';
import { attachPageWatchers } from '../helpers/ui';

/**
 * Regression coverage for the UX polish pass on /login:
 * - user form is the default surface
 * - admin key is hidden behind an Advanced disclosure and toggles cleanly
 * - the animated emerald hero renders without console errors
 */

test('login defaults to the user form', async ({ page }) => {
  await page.goto('/login');
  await expect(page.getByPlaceholder('you@company.com')).toBeVisible();
  await expect(page.getByPlaceholder('Password')).toBeVisible();

  // Admin key input must NOT be in the DOM before the user opts in.
  await expect(page.getByLabel(/Gateway admin key/i)).toHaveCount(0);

  // The disclosure trigger should be visible and collapsed.
  const trigger = page.getByRole('button', { name: /Advanced:.*admin key/i });
  await expect(trigger).toBeVisible();
  await expect(trigger).toHaveAttribute('aria-expanded', 'false');
});

test('admin key disclosure reveals and hides the master-key form', async ({ page }) => {
  await page.goto('/login');
  const trigger = page.getByRole('button', { name: /Advanced:.*admin key/i });
  await trigger.click();
  await expect(trigger).toHaveAttribute('aria-expanded', 'true');

  const adminInput = page.getByLabel(/Gateway admin key/i);
  await expect(adminInput).toBeVisible();
  await expect(adminInput).toHaveAttribute('type', 'password');
  await expect(page.getByRole('button', { name: /Continue as admin/i })).toBeVisible();

  // Collapsing it removes the admin input again so users can't submit it by
  // accident.
  await trigger.click();
  await expect(trigger).toHaveAttribute('aria-expanded', 'false');
  await expect(page.getByLabel(/Gateway admin key/i)).toHaveCount(0);
});

test('animated hero renders without console errors', async ({ page }) => {
  const { consoleErrors, networkFailures } = attachPageWatchers(page);
  await page.goto('/login');
  await expect(page.getByRole('heading', { name: /Gateway-?LLM/i }).first()).toBeVisible();
  await page.waitForTimeout(400);

  expect(consoleErrors, `Unexpected console errors: ${consoleErrors.join('\n')}`).toEqual([]);
  expect(
    networkFailures,
    `Unexpected 5xx responses: ${networkFailures.map((f) => `${f.status} ${f.url}`).join('\n')}`,
  ).toEqual([]);
});
