import { test, expect } from '@playwright/test';
import { authenticatedRoutes } from '../helpers/routes';

/**
 * Every authenticated route must push unauthenticated visitors back to
 * /login. A single leaky page is a security problem, so we sweep all of
 * them in one shot.
 */

test.beforeEach(async ({ page }) => {
  await page.goto('/login');
  await page.evaluate(() => localStorage.clear());
});

for (const p of authenticatedRoutes()) {
  test(`${p.route} bounces anonymous visitors to /login`, async ({ page }) => {
    await page.goto(p.route);
    await page.waitForURL(/\/login/, { timeout: 10_000 }).catch(() => {});
    expect(new URL(page.url()).pathname, `auth missing on ${p.route}`).toMatch(/\/login/);
  });
}
