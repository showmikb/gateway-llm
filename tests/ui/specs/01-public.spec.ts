import { test, expect } from '@playwright/test';

/**
 * The publicly-reachable pages (login + signup) need to stay fast and
 * functional for brand-new users. This spec asserts their key affordances
 * independent of any logged-in state.
 */

test('login page renders the user-first form', async ({ page }) => {
  await page.goto('/login');
  await expect(page.getByRole('heading', { name: /Gateway-?LLM/i }).first()).toBeVisible();

  // User form is rendered by default — no tab click required.
  await expect(page.getByPlaceholder('you@company.com')).toBeVisible();
  await expect(page.getByPlaceholder('Password')).toBeVisible();
  await expect(page.getByRole('button', { name: /^Sign in$/i })).toBeVisible();

  // Admin key should be hidden behind the Advanced disclosure by default.
  await expect(page.getByLabel(/Gateway admin key/i)).toHaveCount(0);

  await expect(page.getByRole('link', { name: /Create a free account/i })).toBeVisible();
});

test('signup page renders the registration form', async ({ page }) => {
  await page.goto('/signup');
  await expect(page.getByPlaceholder('you@company.com')).toBeVisible();
  await expect(page.getByPlaceholder('At least 8 characters')).toBeVisible();
  await expect(page.getByPlaceholder('Repeat password')).toBeVisible();
  await expect(page.getByRole('button', { name: /Create account/ })).toBeVisible();
  await expect(page.getByRole('link', { name: /Sign in/ })).toBeVisible();
});

test('signup client-side validation blocks mismatched passwords', async ({ page }) => {
  await page.goto('/signup');
  await page.getByPlaceholder('you@company.com').fill('mismatch@example.com');
  await page.getByPlaceholder('At least 8 characters').fill('longenoughpw');
  await page.getByPlaceholder('Repeat password').fill('different-pw!');
  await page.getByRole('button', { name: /Create account/ }).click();
  await expect(page.getByText(/Passwords do not match/i)).toBeVisible();
});

test('login with wrong password surfaces a clear error', async ({ page }) => {
  await page.goto('/login');
  await page.getByPlaceholder('you@company.com').fill('nobody@nowhere.test');
  await page.getByPlaceholder('Password').fill('definitelywrongpassword');
  await page.getByRole('button', { name: /^Sign in$/i }).click();
  await expect(page.locator('p.text-red-400').first()).toBeVisible({ timeout: 15000 });
});
