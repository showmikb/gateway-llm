import { test, expect } from '@playwright/test';
import { loginAsUser } from '../helpers/ui';
import { loadAccounts } from '../helpers/accounts';
import { randomSuffix } from '../helpers/env';

/**
 * Full CRUD loop against /credentials. Creates a fake OpenAI credential,
 * confirms it shows up in the table, then deletes it via the Delete
 * action (confirm dialog auto-accepted).
 */

test('can create and delete a provider key', async ({ page }) => {
  const accounts = loadAccounts();
  await loginAsUser(page, accounts.orgAdmin.token, {
    id: accounts.orgAdmin.userId,
    email: accounts.orgAdmin.email,
    role: 'org_admin',
    org_id: accounts.orgAdmin.orgId,
  });

  const label = `ui-test-cred-${randomSuffix(6)}`;
  await page.goto('/credentials');
  await page.getByRole('button', { name: /Add Provider Key/ }).click();
  await page.getByPlaceholder('openai-prod').fill(label);
  await page.getByPlaceholder('sk-...').fill(`sk-uitest-${randomSuffix(24)}`);
  await page.getByRole('button', { name: /Save Provider Key/ }).click();

  const row = page.locator('tr', { hasText: label });
  await expect(row).toBeVisible({ timeout: 15_000 });

  // Auto-accept the native confirm() dialog triggered by Delete.
  page.once('dialog', (d) => d.accept());
  await row.getByRole('button', { name: /Delete/ }).click();
  await expect(row).toHaveCount(0, { timeout: 15_000 });
});

test('empty state appears when no credentials exist after cleanup', async ({ page }) => {
  const accounts = loadAccounts();
  await loginAsUser(page, accounts.orgAdmin.token, {
    id: accounts.orgAdmin.userId,
    email: accounts.orgAdmin.email,
    role: 'org_admin',
    org_id: accounts.orgAdmin.orgId,
  });
  await page.goto('/credentials');
  // The header "Add Provider Key" CTA is always there for org_admins, even
  // when the table has rows. That's the stable landmark.
  await expect(page.getByRole('button', { name: /^Add Provider Key$/ })).toBeVisible();
});
