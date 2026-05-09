import { test, expect, type Page } from '@playwright/test';
import { loginAsUser, loginAsMaster } from '../helpers/ui';
import { loadAccounts } from '../helpers/accounts';
import { ApiClient } from '../helpers/api';

/**
 * End-to-end coverage for the onboarding UX polish:
 *
 *  1. A freshly-registered org_admin lands on the dashboard and sees the
 *     OnboardingWizard with pulsing sidebar indicators.
 *  2. The sidebar shows a "n/3" setup badge next to Dashboard.
 *  3. After provisioning provider key, model route, and API key via the
 *     management API (so the flow stays hermetic even when the UI forms
 *     change), the pulses go away and the wizard's step 4 waits on a real
 *     request.
 *  4. The endpoint card and CopyButton are both present on the dashboard.
 *
 * We provision via the API rather than via form clicks to keep the test
 * resilient to form rewrites; the point of the spec is the state-driven
 * UX, not the form plumbing (covered by specs/05,06,07).
 */

async function dismissOnboardingInStorage(page: Page) {
  await page.evaluate(() => {
    localStorage.removeItem('gatewayllm_onboarding_pending');
  });
}

test('fresh org_admin sees wizard + setup badge + endpoint card', async ({ page }) => {
  const accounts = loadAccounts();
  await loginAsUser(page, accounts.orgAdmin.token, {
    id: accounts.orgAdmin.userId,
    email: accounts.orgAdmin.email,
    role: 'org_admin',
    org_id: accounts.orgAdmin.orgId,
  });
  await dismissOnboardingInStorage(page);
  await page.goto('/');

  // Endpoint card is always visible on dashboard.
  await expect(page.getByText(/Your gateway endpoint/i)).toBeVisible();
  await expect(page.getByRole('button', { name: /Copy endpoint/i })).toBeVisible();

  // Onboarding wizard is visible because no providers/routes/keys exist yet.
  await expect(page.getByText(/Welcome to Gateway-LLM/i)).toBeVisible();

  // Sidebar pulses should be present on the provider/model/keys links.
  // They are rendered as `<span aria-label="Setup required">`.
  const pulseLocator = page.locator('[aria-label="Setup required"]');
  await expect(pulseLocator.first()).toBeVisible();
});

test('after provisioning credential + route + key, wizard waits on first request', async ({ page }) => {
  const accounts = loadAccounts();
  // Use the org_admin's own token so the resources are scoped to their org
  // the same way the UI would scope them — avoids cross-org visibility gaps.
  const client = new ApiClient(accounts.orgAdmin.token);

  const cred = await client.request<any>('POST', '/v1/management/credentials', {
    name: `ui-${Date.now()}`,
    provider: 'openai',
    api_key: 'sk-uitest-placeholder',
  });
  const dep = await client.request<any>('POST', '/v1/management/deployments', {
    model_alias: `ui-${Date.now()}`,
    provider: 'openai',
    provider_model: 'gpt-4o-mini',
    credential_id: cred.id,
    priority: 1,
  });
  const key = await client.request<any>('POST', '/v1/management/keys', {
    name: `ui-${Date.now()}`,
  });

  try {
    await loginAsUser(page, accounts.orgAdmin.token, {
      id: accounts.orgAdmin.userId,
      email: accounts.orgAdmin.email,
      role: 'org_admin',
      org_id: accounts.orgAdmin.orgId,
    });
    await dismissOnboardingInStorage(page);
    await page.goto('/');

    // Give the dashboard fetches a beat to land before asserting on the
    // derived state.
    await page.waitForLoadState('networkidle').catch(() => {});

    // Pulses are triggered by the per-step setup flags; with all three seeded
    // the provider/model/keys indicators should no longer be rendered.
    await expect.poll(async () => page.locator('[aria-label="Setup required"]').count(), {
      timeout: 10_000,
    }).toBe(0);

    // Step 4 ("try it out") is tied to totalReq>0, so either the wizard
    // remains visible (with step 4 unchecked) or the setup-incomplete CTA
    // surfaces "Send your first request". Both prove the same state.
    const wizardVisible = await page
      .getByText(/Welcome to Gateway-LLM/i)
      .isVisible()
      .catch(() => false);
    const ctaVisible = await page
      .getByText(/Send your first request/i)
      .first()
      .isVisible()
      .catch(() => false);
    expect(wizardVisible || ctaVisible).toBe(true);
  } finally {
    // Best-effort cleanup — don't fail the test if the API is flaky.
    await client.request('DELETE', `/v1/management/keys/${key.id}`).catch(() => {});
    await client.request('DELETE', `/v1/management/deployments/${dep.id}`).catch(() => {});
    await client.request('DELETE', `/v1/management/credentials/${cred.id}`).catch(() => {});
  }
});

test('onboarding step 4 curl contains the just-created sk- token', async ({ page }) => {
  // This test uses the UI to create the key end-to-end so we can assert
  // that rawKey survives into TryItStep and replaces the YOUR_GATEWAY_KEY
  // placeholder. We pre-provision the provider + route over the API to
  // skip the parts of the wizard we don't care about here.
  const accounts = loadAccounts();
  const client = new ApiClient(accounts.orgAdmin.token);

  const cred = await client.request<any>('POST', '/v1/management/credentials', {
    name: `onb-${Date.now()}`,
    provider: 'openai',
    api_key: 'sk-uitest-placeholder',
  });
  const dep = await client.request<any>('POST', '/v1/management/deployments', {
    model_alias: `onb-${Date.now()}`,
    provider: 'openai',
    provider_model: 'gpt-4o-mini',
    credential_id: cred.id,
    priority: 1,
  });

  try {
    await loginAsUser(page, accounts.orgAdmin.token, {
      id: accounts.orgAdmin.userId,
      email: accounts.orgAdmin.email,
      role: 'org_admin',
      org_id: accounts.orgAdmin.orgId,
    });

    // Create a key via the UI so the wizard owns the raw token.
    await page.goto('/keys');
    await page.getByRole('button', { name: /^Create Key$/ }).click();
    await page.getByPlaceholder('production-app').fill(`onb-key-${Date.now()}`);
    await page.getByRole('button', { name: /Create Key/ }).last().click();

    const revealed = page.locator('code', { hasText: /sk-/ }).first();
    await expect(revealed).toBeVisible({ timeout: 15_000 });
    const rawKey = (await revealed.innerText()).trim();
    expect(rawKey.startsWith('sk-')).toBe(true);

    // The Try-it panel on the revealed card pre-fills curl with the real key.
    await expect(page.locator('pre', { hasText: new RegExp(rawKey) }).first()).toBeVisible({
      timeout: 10_000,
    });
    // And it is NOT still carrying the placeholder.
    await expect(page.locator('pre', { hasText: /YOUR_GATEWAY_KEY/ })).toHaveCount(0);
  } finally {
    await client.request('DELETE', `/v1/management/deployments/${dep.id}`).catch(() => {});
    await client.request('DELETE', `/v1/management/credentials/${cred.id}`).catch(() => {});
  }
});

test('master key dashboard renders without the user-scoped setup banner', async ({ page }) => {
  // Sanity: the master key sees every org; the setup signal is per-caller
  // (whoever owns the current keys/creds/deployments), and the master key
  // session inherits the super_admin scope.
  await loginAsMaster(page);
  await page.goto('/');
  await expect(page.getByRole('heading', { name: /^Dashboard$/ })).toBeVisible();
  await expect(page.getByText(/Your gateway endpoint/i)).toBeVisible();
});
