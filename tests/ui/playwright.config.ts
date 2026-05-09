import { defineConfig, devices } from '@playwright/test';
import { env } from './helpers/env';

/**
 * Playwright config for Gateway-LLM UI tests.
 *
 * By default this runs against production (https://app.gateway-llm.com +
 * https://api.gateway-llm.com). Override with UI_URL and API_URL env vars
 * to point at a staging or local stack.
 */
export default defineConfig({
  testDir: './specs',
  fullyParallel: false, // we share a single provisioned test org; keep order deterministic
  forbidOnly: !!process.env.CI,
  retries: process.env.CI ? 2 : 0,
  workers: 1,
  timeout: 60_000,
  expect: { timeout: 10_000 },
  reporter: [
    ['list'],
    ['html', { open: 'never', outputFolder: 'playwright-report' }],
    ['json', { outputFile: 'test-results/results.json' }],
  ],
  outputDir: 'test-results/artifacts',
  use: {
    baseURL: env.UI_URL,
    actionTimeout: 10_000,
    navigationTimeout: 20_000,
    screenshot: 'only-on-failure',
    trace: 'retain-on-failure',
    video: 'retain-on-failure',
    viewport: { width: 1440, height: 900 },
    ignoreHTTPSErrors: true,
  },
  globalSetup: require.resolve('./helpers/global-setup.ts'),
  globalTeardown: require.resolve('./helpers/global-teardown.ts'),
  projects: [
    {
      name: 'chromium',
      use: { ...devices['Desktop Chrome'] },
    },
  ],
});
