import fs from 'fs';
import path from 'path';
import { ApiClient } from './api';
import { env, randomSuffix } from './env';

/**
 * Shape of the account bundle that global-setup writes to disk. Spec files
 * read this through `loadAccounts()` instead of re-creating fixtures every
 * test, which keeps the suite fast and avoids burning the public /register
 * rate limit.
 */
export interface TestAccounts {
  runId: string;
  orgAdmin: { email: string; password: string; token: string; orgId: string; teamId: string; userId: string };
  member: { email: string; password: string; token: string; userId: string };
  viewer: { email: string; password: string; token: string; userId: string };
  secondaryOrg: { id: string; adminEmail: string; adminPassword: string };
}

const STATE_FILE = path.join(__dirname, '..', '.auth', 'accounts.json');

export async function provisionAccounts(): Promise<TestAccounts> {
  const admin = new ApiClient();
  const runId = env.TAG;
  const stamp = Date.now();
  const orgAdminEmail = `uitest-admin-${stamp}-${randomSuffix(4)}@gateway-llm.test`;

  // --- primary org: register through the public endpoint so we exercise the
  // same provisioning path a real user hits. This is the ONE unavoidable use
  // of /register in a run; everything else is seeded via master key.
  const reg = await ApiClient.register(orgAdminEmail, env.TEST_PASSWORD, `UI Test Admin ${stamp}`);
  const orgAdminToken: string = reg.token;
  const orgId: string = reg.organization.id;
  const teamId: string = reg.team.id;
  const userId: string = reg.user.id;

  // --- extra users inside that same org, created by master key (bypasses
  // the public rate limit and lets us cover RBAC paths deterministically).
  const memberEmail = `uitest-member-${stamp}-${randomSuffix(4)}@gateway-llm.test`;
  const viewerEmail = `uitest-viewer-${stamp}-${randomSuffix(4)}@gateway-llm.test`;
  const member = await admin.request<any>('POST', '/v1/management/users', {
    email: memberEmail,
    password: env.TEST_PASSWORD,
    role: 'member',
    org_id: orgId,
    team_id: teamId,
  });
  const viewer = await admin.request<any>('POST', '/v1/management/users', {
    email: viewerEmail,
    password: env.TEST_PASSWORD,
    role: 'viewer',
    org_id: orgId,
    team_id: teamId,
  });

  const memberLogin = await ApiClient.login(memberEmail, env.TEST_PASSWORD);
  const viewerLogin = await ApiClient.login(viewerEmail, env.TEST_PASSWORD);

  // --- secondary org: super-admin-only. Used by the cross-tenant isolation
  // checks to confirm org_admin of the primary org can't see it in the UI.
  const secondaryOrgName = `ui-tests-secondary-${stamp}`;
  const secondaryOrg = await admin.request<any>('POST', '/v1/management/organizations', {
    name: secondaryOrgName,
    slug: `ui-tests-sec-${stamp}`,
  });

  const accounts: TestAccounts = {
    runId,
    orgAdmin: {
      email: orgAdminEmail,
      password: env.TEST_PASSWORD,
      token: orgAdminToken,
      orgId,
      teamId,
      userId,
    },
    member: { email: memberEmail, password: env.TEST_PASSWORD, token: memberLogin.token, userId: member.id },
    viewer: { email: viewerEmail, password: env.TEST_PASSWORD, token: viewerLogin.token, userId: viewer.id },
    secondaryOrg: {
      id: secondaryOrg.id,
      adminEmail: 'n/a',
      adminPassword: 'n/a',
    },
  };

  fs.mkdirSync(path.dirname(STATE_FILE), { recursive: true });
  fs.writeFileSync(STATE_FILE, JSON.stringify(accounts, null, 2));
  return accounts;
}

export function loadAccounts(): TestAccounts {
  if (!fs.existsSync(STATE_FILE)) {
    throw new Error(`accounts.json missing; did global-setup run? Expected ${STATE_FILE}`);
  }
  return JSON.parse(fs.readFileSync(STATE_FILE, 'utf-8')) as TestAccounts;
}

export async function cleanupAccounts(): Promise<void> {
  if (!fs.existsSync(STATE_FILE)) return;
  const accounts = loadAccounts();
  const admin = new ApiClient();

  const userIds = [accounts.orgAdmin.userId, accounts.member.userId, accounts.viewer.userId];
  for (const id of userIds) {
    await admin.request('DELETE', `/v1/management/users/${id}`).catch(() => {});
  }
  await admin.request('DELETE', `/v1/management/organizations/${accounts.orgAdmin.orgId}`).catch(() => {});
  await admin.request('DELETE', `/v1/management/organizations/${accounts.secondaryOrg.id}`).catch(() => {});

  fs.rmSync(path.dirname(STATE_FILE), { recursive: true, force: true });
}
