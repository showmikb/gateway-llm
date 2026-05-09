import { test, expect } from '@playwright/test';
import { discoverPages, publicRoutes } from '../helpers/routes';
import { loginAsUser, attachPageWatchers } from '../helpers/ui';
import { loadAccounts } from '../helpers/accounts';

/**
 * Auto-growing smoke suite: one test per `page.tsx` found under
 * ui/src/app/. Adding a new page anywhere in the Next.js app tree makes
 * it show up here the next time `npm test` runs. No manual wiring.
 *
 * We assert that each page:
 *   1. Renders (HTTP 200, no router-level crash)
 *   2. Surfaces no browser-console errors
 *   3. Emits no 5xx from any fetch it fires during load
 *   4. Shows something other than the generic loading skeleton
 */

const pages = discoverPages();
const publicSet = new Set(publicRoutes().map((p) => p.route));

test.describe('Smoke: every page renders', () => {
  test.beforeEach(async ({ page }) => {
    const accounts = loadAccounts();
    if (accounts) {
      await loginAsUser(page, accounts.orgAdmin.token, {
        id: accounts.orgAdmin.userId,
        email: accounts.orgAdmin.email,
        role: 'org_admin',
        org_id: accounts.orgAdmin.orgId,
      });
    }
  });

  for (const p of pages) {
    test(`GET ${p.route}${p.isPublic ? ' (public)' : ''}`, async ({ page }) => {
      const { consoleErrors, networkFailures } = attachPageWatchers(page);
      const resp = await page.goto(p.route);
      expect(resp, `no response for ${p.route}`).not.toBeNull();
      expect(resp!.status(), `status for ${p.route}`).toBeLessThan(400);

      // Give the page a breath to settle client-side data fetches.
      await page.waitForLoadState('networkidle').catch(() => {});

      // The app ships a full-page spinner that reads "Loading..." briefly.
      // If we never leave it, the page is broken.
      const stillLoading = await page.locator('body:has-text("Loading...")').count();
      if (stillLoading > 0) {
        const bodyText = (await page.locator('body').innerText()).slice(0, 400);
        // Only fail if "Loading..." is literally *all* we see.
        if (bodyText.trim() === 'Loading...') {
          throw new Error(`${p.route} stuck on Loading...`);
        }
      }

      // When authenticated pages silently redirect to /login, it's an auth bug.
      if (!publicSet.has(p.route)) {
        const url = new URL(page.url());
        expect(url.pathname, `auth page ${p.route} bounced to login`).not.toMatch(/\/login/);
      }

      expect(consoleErrors, `console errors on ${p.route}`).toEqual([]);
      expect(networkFailures, `5xx responses on ${p.route}`).toEqual([]);
    });
  }
});
