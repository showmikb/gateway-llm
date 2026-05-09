import { cleanupAccounts } from './accounts';

export default async function globalTeardown(): Promise<void> {
  if (process.env.KEEP_TEST_DATA === '1') {
    // eslint-disable-next-line no-console
    console.log('[gateway-llm-ui-tests] KEEP_TEST_DATA=1, skipping cleanup');
    return;
  }
  await cleanupAccounts();
}
