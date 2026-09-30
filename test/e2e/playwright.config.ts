import { defineConfig, devices } from '@playwright/test';

// The stack (hub + Postgres + GitHub mock + OIDC + stub LLM) is started once; specs run in file order on one
// worker because the first sign-in becomes the owner. Build the hub with `make release` first.
const HUB = process.env.DTH_E2E_HUB ?? 'http://127.0.0.1:18090';
export const CONTROL = process.env.DTH_E2E_CONTROL ?? 'http://127.0.0.1:18099';

export default defineConfig({
  testDir: './specs',
  fullyParallel: false,
  workers: 1,
  timeout: 180_000,
  expect: { timeout: 15_000 },
  retries: 0,
  reporter: process.env.CI ? [['list'], ['html', { open: 'never' }]] : 'list',
  use: {
    baseURL: HUB,
    trace: 'retain-on-failure',
    screenshot: 'only-on-failure',
    ...devices['Desktop Chrome'],
    viewport: { width: 1360, height: 900 },
  },
  webServer: {
    command: 'go run ./stack -hub ../../bin/dth-hub -state-file .stack-state.json -work-dir .stack',
    url: `${CONTROL}/state`,
    reuseExistingServer: !process.env.CI,
    timeout: 240_000,
    stdout: 'pipe',
    stderr: 'pipe',
  },
});
