import { test, expect } from '@playwright/test';
import { loginAsUser } from '../helpers/ui';
import { loadAccounts } from '../helpers/accounts';
import { randomSuffix } from '../helpers/env';

/**
 * CRUD against /models. We create a virtual model with one OpenAI target
 * (random alias), confirm the card appears, then delete it.
 */

test('can create and remove a virtual model', async ({ page }) => {
  const accounts = loadAccounts();
  await loginAsUser(page, accounts.orgAdmin.token, {
    id: accounts.orgAdmin.userId,
    email: accounts.orgAdmin.email,
    role: 'org_admin',
    org_id: accounts.orgAdmin.orgId,
  });

  const alias = `ui-test-alias-${randomSuffix(6)}`;
  const backend = `gpt-4o-mini`;

  await page.goto('/models');
  await page.getByRole('button', { name: /Create Virtual Model/ }).click();

  // Fill the virtual model name in the composer
  await page.getByPlaceholder('smart').fill(alias);
  // Fill the first target's backend model
  await page.getByPlaceholder('gpt-4o-mini').first().fill(backend);

  await page.getByRole('button', { name: /Create virtual model with/ }).click();

  // Each virtual model renders as an <h2> heading above its backend table.
  const aliasHeading = page.getByRole('heading', { name: alias });
  await expect(aliasHeading).toBeVisible({ timeout: 15_000 });

  // The delete button lives in the backend-row inside the same card.
  const aliasCard = page.locator('div', { has: aliasHeading }).first();
  page.once('dialog', (d) => d.accept());
  await aliasCard.getByRole('button', { name: /^Delete$/ }).click();
  await expect(aliasHeading).toHaveCount(0, { timeout: 15_000 });
});

/**
 * Multi-target composer: create one virtual model with two targets in one flow.
 */
test('can create a virtual model with multiple targets', async ({ page }) => {
  const accounts = loadAccounts();
  await loginAsUser(page, accounts.orgAdmin.token, {
    id: accounts.orgAdmin.userId,
    email: accounts.orgAdmin.email,
    role: 'org_admin',
    org_id: accounts.orgAdmin.orgId,
  });

  const alias = `multi-${randomSuffix(6)}`;

  await page.goto('/models');
  await page.getByRole('button', { name: /Create Virtual Model/ }).click();

  await page.getByPlaceholder('smart').fill(alias);

  // Fill first target
  const backendInputs = page.getByPlaceholder('gpt-4o-mini');
  await backendInputs.first().fill('gpt-4o-mini');

  // Add a second target
  await page.getByText('+ Add another target').click();
  await backendInputs.nth(1).fill('gpt-4.1-mini');

  await page.getByRole('button', { name: /Create virtual model with 2 target/ }).click();

  const aliasHeading = page.getByRole('heading', { name: alias });
  await expect(aliasHeading).toBeVisible({ timeout: 15_000 });

  // Verify "2 targets" is shown
  const card = page.locator('div', { has: aliasHeading }).first();
  await expect(card.getByText('2 targets')).toBeVisible();

  // Both backend models should be visible
  await expect(card.getByText('gpt-4o-mini')).toBeVisible();
  await expect(card.getByText('gpt-4.1-mini')).toBeVisible();

  // Cleanup — delete both targets
  const deleteButtons = card.getByRole('button', { name: /^Delete$/ });
  const count = await deleteButtons.count();
  for (let i = count - 1; i >= 0; i--) {
    page.once('dialog', (d) => d.accept());
    await deleteButtons.nth(i).click();
    await page.waitForTimeout(500);
  }
  await expect(aliasHeading).toHaveCount(0, { timeout: 15_000 });
});
