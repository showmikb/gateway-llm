import { test, expect } from '@playwright/test';
import { loginAsUser } from '../helpers/ui';
import { loadAccounts } from '../helpers/accounts';
import { randomSuffix } from '../helpers/env';

/**
 * Creating an API key should reveal the full key exactly once, then the
 * row should land in the table with a masked version.
 */

test('creating an API key reveals the full key and lists it', async ({ page }) => {
  const accounts = loadAccounts();
  await loginAsUser(page, accounts.orgAdmin.token, {
    id: accounts.orgAdmin.userId,
    email: accounts.orgAdmin.email,
    role: 'org_admin',
    org_id: accounts.orgAdmin.orgId,
  });

  const label = `ui-test-key-${randomSuffix(6)}`;
  await page.goto('/keys');
  await page.getByRole('button', { name: /^Create Key$/ }).click();
  await page.getByPlaceholder('production-app').fill(label);
  await page.getByRole('button', { name: /Create Key/ }).last().click();

  // The revealed block contains the full sk- key.
  await expect(page.getByText(/Key created — copy it now/i)).toBeVisible({ timeout: 15_000 });
  await expect(page.locator('code', { hasText: /sk-/ }).first()).toBeVisible();

  // Row shows up in the table.
  await expect(page.locator('tr', { hasText: label })).toBeVisible();
});
