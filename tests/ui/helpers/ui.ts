import { Page, expect } from '@playwright/test';
import { env } from './env';

/**
 * Writes the auth values that the Next.js app reads out of localStorage on
 * boot. Using this primes a logged-in session without going through the
 * login form (faster, more reliable, doesn't burn retries on flaky form
 * interactions).
 */
export async function loginAsUser(page: Page, token: string, user: unknown): Promise<void> {
  await page.goto(`${env.UI_URL}/login`);
  await page.evaluate(
    ({ token, user }) => {
      localStorage.removeItem('gatewayllm_master_key');
      localStorage.setItem('gatewayllm_token', token);
      localStorage.setItem('gatewayllm_user', JSON.stringify(user));
      localStorage.removeItem('gatewayllm_onboarding_pending');
    },
    { token, user },
  );
}

export async function loginAsMaster(page: Page): Promise<void> {
  await page.goto(`${env.UI_URL}/login`);
  await page.evaluate((key) => {
    localStorage.removeItem('gatewayllm_token');
    localStorage.removeItem('gatewayllm_user');
    localStorage.setItem('gatewayllm_master_key', key);
  }, env.MASTER_KEY);
}

export async function logout(page: Page): Promise<void> {
  await page.evaluate(() => {
    localStorage.clear();
  });
}

/**
 * Navigates and waits for the page to be ready. Rejects if the app
 * bounced to /login (signalling an auth/session failure) unless the
 * caller explicitly expected that.
 */
export async function gotoAndSettle(
  page: Page,
  route: string,
  opts: { expectRedirectToLogin?: boolean } = {},
): Promise<void> {
  await page.goto(`${env.UI_URL}${route}`);
  await page.waitForLoadState('networkidle').catch(() => {});
  if (opts.expectRedirectToLogin) {
    await expect(page).toHaveURL(/\/login/);
    return;
  }
  // Surface the first error banner (if any) so screenshots are easier to read.
  const banner = page.locator('text=/^Error|Failed to load|Unauthorized/i').first();
  if (await banner.isVisible().catch(() => false)) {
    throw new Error(`Visible error banner on ${route}: ${await banner.innerText()}`);
  }
}

/**
 * Collects browser-console errors and failing network responses during a
 * navigation, for the smoke test to assert against.
 */
export function attachPageWatchers(page: Page): {
  consoleErrors: string[];
  networkFailures: { url: string; status: number }[];
} {
  const consoleErrors: string[] = [];
  const networkFailures: { url: string; status: number }[] = [];

  page.on('console', (msg) => {
    if (msg.type() === 'error') {
      const text = msg.text();
      // ResizeObserver spam + dev-tools Next.js warnings are noisy and not
      // actionable; filter them out so the assertion stays meaningful.
      if (
        /ResizeObserver|Download the React DevTools|hydration/i.test(text) ||
        // Next.js prints this as console.error whenever a speculative RSC
        // prefetch is cancelled (very common with Playwright navigations).
        // It does NOT represent a broken page; the actual user-facing
        // navigation still succeeds via the browser fallback.
        /Failed to fetch RSC payload.*Falling back to browser navigation/i.test(text)
      ) {
        return;
      }
      consoleErrors.push(text);
    }
  });
  page.on('response', (resp) => {
    const status = resp.status();
    if (status >= 500) {
      networkFailures.push({ url: resp.url(), status });
    }
  });
  return { consoleErrors, networkFailures };
}
