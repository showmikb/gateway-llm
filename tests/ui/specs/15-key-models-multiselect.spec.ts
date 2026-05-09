import { test, expect } from '@playwright/test';
import { loadAccounts } from '../helpers/accounts';
import { ApiClient } from '../helpers/api';
import { env } from '../helpers/env';

/**
 * API-key `models` scoping, covering the new behaviours:
 *
 *  1. `models: ["*"]` acts as a wildcard — the key can call any alias its
 *     org owns.
 *  2. A narrowly-scoped key (`models: ["alias-a"]`) is rejected with 403 on
 *     a different alias.
 *
 * The request path is `/v1/chat/completions` with an unreachable upstream;
 * we only care that the gateway's auth middleware lets the request through
 * (past 403) vs rejects it. Any upstream-provider error is acceptable for
 * the wildcard case — that signals the auth check passed.
 */

async function post(
  token: string,
  model: string,
  base: string,
): Promise<number> {
  const res = await fetch(`${base}/v1/chat/completions`, {
    method: 'POST',
    headers: {
      'Content-Type': 'application/json',
      Authorization: `Bearer ${token}`,
    },
    body: JSON.stringify({
      model,
      messages: [{ role: 'user', content: 'ping' }],
      max_tokens: 1,
    }),
  });
  // Drain the body so the socket recycles; value itself doesn't matter.
  await res.text().catch(() => '');
  return res.status;
}

test('wildcard key can call any alias; scoped key gets 403 on others', async () => {
  const accounts = loadAccounts();
  const client = new ApiClient(accounts.orgAdmin.token);

  const aliasA = `wc-a-${Date.now()}`;
  const aliasB = `wc-b-${Date.now()}`;

  const cred = await client.request<any>('POST', '/v1/management/credentials', {
    name: `wc-${Date.now()}`,
    provider: 'openai',
    api_key: 'sk-uitest-placeholder',
  });

  const depA = await client.request<any>('POST', '/v1/management/deployments', {
    model_alias: aliasA,
    provider: 'openai',
    provider_model: 'gpt-4o-mini',
    credential_id: cred.id,
    priority: 1,
  });
  const depB = await client.request<any>('POST', '/v1/management/deployments', {
    model_alias: aliasB,
    provider: 'openai',
    provider_model: 'gpt-4o-mini',
    credential_id: cred.id,
    priority: 1,
  });

  const wildcardKey = await client.request<any>('POST', '/v1/management/keys', {
    name: `wc-any-${Date.now()}`,
    models: ['*'],
  });
  const scopedKey = await client.request<any>('POST', '/v1/management/keys', {
    name: `wc-scoped-${Date.now()}`,
    models: [aliasA],
  });

  try {
    const base = env.API_URL;

    const wildA = await post(wildcardKey.key, aliasA, base);
    const wildB = await post(wildcardKey.key, aliasB, base);
    expect(wildA).not.toBe(403);
    expect(wildB).not.toBe(403);

    const scopedA = await post(scopedKey.key, aliasA, base);
    const scopedB = await post(scopedKey.key, aliasB, base);
    expect(scopedA).not.toBe(403);
    expect(scopedB).toBe(403);
  } finally {
    await client.request('DELETE', `/v1/management/keys/${wildcardKey.id}`).catch(() => {});
    await client.request('DELETE', `/v1/management/keys/${scopedKey.id}`).catch(() => {});
    await client.request('DELETE', `/v1/management/deployments/${depA.id}`).catch(() => {});
    await client.request('DELETE', `/v1/management/deployments/${depB.id}`).catch(() => {});
    await client.request('DELETE', `/v1/management/credentials/${cred.id}`).catch(() => {});
  }
});
