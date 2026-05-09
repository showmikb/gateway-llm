import { test, expect } from '@playwright/test';
import { loadAccounts } from '../helpers/accounts';
import { ApiClient } from '../helpers/api';

/**
 * Multi-target virtual models.
 *
 *  1. An alias with two targets is created via the management API.
 *  2. Flipping the alias strategy propagates to every target row (backed by
 *     the new /deployments/alias-strategy endpoint).
 *  3. Posting a deployment with a mismatching strategy under the same alias
 *     is rejected with 409 strategy_mismatch.
 *
 * We test on the API level because the routing decision happens server-side
 * and doesn't depend on the UI. The UI grouping is covered visually in the
 * smoke pass (spec 00) via console/network assertions.
 */

test('alias with two targets accepts strategy + rejects mismatching follow-up', async () => {
  const accounts = loadAccounts();
  const client = new ApiClient(accounts.orgAdmin.token);

  const alias = `route-${Date.now()}`;

  const cred = await client.request<any>('POST', '/v1/management/credentials', {
    name: `mm-${Date.now()}`,
    provider: 'openai',
    api_key: 'sk-uitest-placeholder',
  });

  const depA = await client.request<any>('POST', '/v1/management/deployments', {
    model_alias: alias,
    provider: 'openai',
    provider_model: 'gpt-4o-mini',
    credential_id: cred.id,
    priority: 1,
    routing_strategy: 'round-robin',
  });

  const depB = await client.request<any>('POST', '/v1/management/deployments', {
    model_alias: alias,
    provider: 'openai',
    provider_model: 'gpt-4.1-mini',
    credential_id: cred.id,
    priority: 2,
    routing_strategy: 'round-robin',
  });

  try {
    // Posting a third target with a DIFFERENT strategy must fail with 409.
    let mismatchStatus = 0;
    try {
      await client.request('POST', '/v1/management/deployments', {
        model_alias: alias,
        provider: 'openai',
        provider_model: 'gpt-4o',
        credential_id: cred.id,
        priority: 3,
        routing_strategy: 'cheapest',
      });
    } catch (err) {
      mismatchStatus = Number(String(err).match(/-> (\d+):/)?.[1] ?? 0);
    }
    expect(mismatchStatus).toBe(409);

    // Flipping the strategy for the alias should update every target.
    await client.request('POST', '/v1/management/deployments/alias-strategy', {
      model_alias: alias,
      routing_strategy: 'cheapest',
    });

    const resp = await client.request<any>('GET', '/v1/management/deployments');
    const rows: any[] = Array.isArray(resp) ? resp : resp?.data ?? [];
    const mine = rows.filter((r: any) => r.model_alias === alias);
    expect(mine.length).toBe(2);
    for (const r of mine) {
      expect(r.routing_strategy).toBe('cheapest');
    }
  } finally {
    await client.request('DELETE', `/v1/management/deployments/${depA.id}`).catch(() => {});
    await client.request('DELETE', `/v1/management/deployments/${depB.id}`).catch(() => {});
    await client.request('DELETE', `/v1/management/credentials/${cred.id}`).catch(() => {});
  }
});
