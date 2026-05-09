import { provisionAccounts } from './accounts';
import { env } from './env';

async function pingApi(): Promise<void> {
  const tries = 10;
  for (let i = 0; i < tries; i++) {
    try {
      const res = await fetch(`${env.API_URL}/health`);
      if (res.ok) return;
    } catch {
      /* retry */
    }
    await new Promise((r) => setTimeout(r, 1500));
  }
  throw new Error(`API not reachable at ${env.API_URL}`);
}

async function pingUi(): Promise<void> {
  const tries = 10;
  for (let i = 0; i < tries; i++) {
    try {
      const res = await fetch(env.UI_URL);
      if (res.ok || res.status === 307 || res.status === 308) return;
    } catch {
      /* retry */
    }
    await new Promise((r) => setTimeout(r, 1500));
  }
  throw new Error(`UI not reachable at ${env.UI_URL}`);
}

export default async function globalSetup(): Promise<void> {
  // eslint-disable-next-line no-console
  console.log(`[gateway-llm-ui-tests] UI=${env.UI_URL}  API=${env.API_URL}`);
  await Promise.all([pingApi(), pingUi()]);
  const accounts = await provisionAccounts();
  // eslint-disable-next-line no-console
  console.log(
    `[gateway-llm-ui-tests] provisioned orgAdmin=${accounts.orgAdmin.email} org=${accounts.orgAdmin.orgId}`,
  );
}
