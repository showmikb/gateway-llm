/**
 * Resolves runtime configuration for the UI test toolkit.
 *
 * Everything has a sensible default for the production stack so the suite
 * can run with zero configuration, but each value can be overridden via
 * environment variables when pointing at staging/local.
 */
export const env = {
  UI_URL: (process.env.UI_URL || 'https://app.gateway-llm.com').replace(/\/$/, ''),
  API_URL: (process.env.API_URL || 'https://api.gateway-llm.com').replace(/\/$/, ''),
  MASTER_KEY: process.env.MASTER_KEY || 'showmikbose@1995',
  TEST_PASSWORD: process.env.TEST_PASSWORD || 'Hunter2-gatewayllm!',
  // When true, the suite also runs the real /signup form which consumes one
  // of the 5-per-hour registration slots. Set to false when iterating quickly.
  RUN_REAL_SIGNUP: (process.env.RUN_REAL_SIGNUP || 'true').toLowerCase() === 'true',
  // Label stamped onto provisioned test resources so teardown can find them.
  TAG: process.env.TEST_TAG || `ui-test-${Date.now()}`,
};

export function randomSuffix(n = 8): string {
  const alphabet = 'abcdefghijklmnopqrstuvwxyz0123456789';
  let s = '';
  for (let i = 0; i < n; i++) s += alphabet[Math.floor(Math.random() * alphabet.length)];
  return s;
}
