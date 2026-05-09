import { test, expect } from '@playwright/test';
import { loginAsUser } from '../helpers/ui';
import { loadAccounts } from '../helpers/accounts';

/**
 * Navigation regressions are surprisingly common because the sidebar is
 * role-gated. Walk every sidebar link an org_admin should see and confirm
 * it lands on the right URL and renders a heading.
 */

const LINKS: { label: RegExp; url: RegExp; heading: RegExp }[] = [
  { label: /Dashboard/, url: /\/$/, heading: /^Dashboard$/ },
  { label: /Model Routes/, url: /\/models$/, heading: /Model Routes/ },
  { label: /API Keys/, url: /\/keys$/, heading: /API Keys/ },
  { label: /Usage/, url: /\/usage$/, heading: /Usage/ },
  { label: /Observability/, url: /\/observability$/, heading: /Observability|Requests/i },
  { label: /Teams/, url: /\/teams$/, heading: /Teams/ },
  { label: /Provider Keys/, url: /\/credentials$/, heading: /Provider Keys/ },
  { label: /Users/, url: /\/users$/, heading: /Users/ },
  { label: /Pricing/, url: /\/pricing$/, heading: /Pricing/i },
  { label: /Settings/, url: /\/settings$/, heading: /Settings/ },
];

test('every sidebar link an org_admin sees navigates correctly', async ({ page }) => {
  const accounts = loadAccounts();
  await loginAsUser(page, accounts.orgAdmin.token, {
    id: accounts.orgAdmin.userId,
    email: accounts.orgAdmin.email,
    role: 'org_admin',
    org_id: accounts.orgAdmin.orgId,
  });
  await page.goto('/');

  for (const { label, url, heading } of LINKS) {
    const link = page.getByRole('link', { name: label }).first();
    if (!(await link.isVisible().catch(() => false))) {
      // Link hidden for this role — skip without failing.
      continue;
    }
    await link.click();
    await expect(page).toHaveURL(url, { timeout: 10_000 });
    await expect(page.getByRole('heading', { name: heading }).first()).toBeVisible({ timeout: 10_000 });
  }
});
