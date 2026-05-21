import { defineConfig, devices } from '@playwright/test';

// SCRIBBLE_E2E_PORT is set by the `just test-e2e` recipe to a
// branch-specific hashed port (so two worktrees on the same machine
// don't collide, and so `just web` and `just test-e2e` can run in
// parallel on the same branch — they hash to different namespaces).
// When unset (CI, or direct `pnpm test:e2e` invocation) the test
// server uses port 3030.
const port = process.env.SCRIBBLE_E2E_PORT ?? '3030';
const baseURL = `http://localhost:${port}`;

export default defineConfig({
  testDir: './tests/e2e',
  fullyParallel: true,
  forbidOnly: !!process.env.CI,
  retries: process.env.CI ? 2 : 0,
  workers: process.env.CI ? 1 : undefined,
  reporter: 'html',
  use: {
    baseURL,
    trace: 'on-first-retry',
  },
  webServer: {
    command: 'go run .',
    env: {
      ADDR: `:${port}`,
      SCRIBBLE_HOST_DISCONNECT_GRACE_SECONDS: '1',
    },
    url: `${baseURL}/healthz`,
    reuseExistingServer: !process.env.CI,
    timeout: 60_000,
  },
  projects: [
    {
      name: 'chromium',
      use: { ...devices['Desktop Chrome'] },
    },
    {
      name: 'firefox',
      use: { ...devices['Desktop Firefox'] },
    },
    {
      name: 'webkit',
      use: { ...devices['Desktop Safari'] },
    },
  ],
});
