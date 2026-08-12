import { defineConfig, devices } from '@playwright/test'

const appOrigin = process.env.APP_ORIGIN ?? 'https://localhost:8443'

export default defineConfig({
  testDir: './e2e',
  fullyParallel: false,
  workers: 1,
  timeout: 30_000,
  expect: { timeout: 8_000 },
  reporter: [['list']],
  use: {
    baseURL: appOrigin,
    ignoreHTTPSErrors: true,
    trace: 'retain-on-failure',
    screenshot: 'only-on-failure',
  },
  projects: [
    {
      name: 'chromium',
      use: { ...devices['Desktop Chrome'] },
    },
  ],
  webServer: {
    command: 'go run ./cmd/server',
    url: `${appOrigin}/healthz`,
    ignoreHTTPSErrors: true,
    // A reused server could point at the developer database instead of the
    // per-run database created by scripts/e2e.sh.
    reuseExistingServer: false,
    timeout: 120_000,
  },
})
