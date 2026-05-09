import { test, expect } from '@playwright/test';
import { env, randomSuffix } from '../helpers/env';

/**
 * Exercises the real /signup form end-to-end so we catch regressions in the
 * public registration path. Gated behind RUN_REAL_SIGNUP because the backend
 * rate-limits /register to 5/hour/IP and running this aggressively during
 * local iteration will lock you out.
 */

const shouldRun = env.RUN_REAL_SIGNUP;

test.describe('sign up then log in', () => {
  test.skip(!shouldRun, 'RUN_REAL_SIGNUP=false');

  test('new user can register and lands on the dashboard', async ({ page }) => {
    const email = `uitest-signup-${Date.now()}-${randomSuffix(4)}@gateway-llm.test`;
    const password = env.TEST_PASSWORD;

    await page.goto('/signup');
    await page.getByPlaceholder('Jane Doe').fill('UI Signup Test');
    await page.getByPlaceholder('you@company.com').fill(email);
    await page.getByPlaceholder('At least 8 characters').fill(password);
    await page.getByPlaceholder('Repeat password').fill(password);
    await page.getByRole('button', { name: /Create account/ }).click();

    // After successful signup we land on /, which renders the Dashboard shell
    // with the sidebar. Assert on the sidebar brand as a stable landmark.
    await page.waitForURL(`${env.UI_URL}/`, { timeout: 20_000 });
    await expect(page.getByRole('link', { name: /Gateway-?LLM/ }).first()).toBeVisible();
    await expect(page.locator('text=Dashboard').first()).toBeVisible();
  });

});

/**
 * Log-in path uses the seed account that global-setup already created, so
 * we don't consume another registration slot. Runs unconditionally.
 */
test('pre-provisioned user can log in (user form is the default)', async ({ page }) => {
  const { loadAccounts } = await import('../helpers/accounts');
  const accounts = loadAccounts();
  await page.goto('/login');
  // User form is the default now; no tab click required.
  await page.getByPlaceholder('you@company.com').fill(accounts.orgAdmin.email);
  await page.getByPlaceholder('Password').fill(accounts.orgAdmin.password);
  await page.getByRole('button', { name: /^Sign in$/i }).click();
  await page.waitForURL(`${env.UI_URL}/`, { timeout: 20_000 });
  await expect(page.locator('text=Dashboard').first()).toBeVisible();
});
