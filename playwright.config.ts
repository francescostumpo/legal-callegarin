import { defineConfig, devices } from "@playwright/test"

const origin = "https://127.0.0.1:4173"

export default defineConfig({
  testDir: "./e2e",
  outputDir: "artifacts/playwright/test-results",
  snapshotPathTemplate:
    "artifacts/playwright/snapshots/{testFilePath}/{arg}{ext}",
  workers: 1,
  fullyParallel: false,
  retries: process.env.CI ? 1 : 0,
  reporter: [
    ["list"],
    [
      "html",
      { outputFolder: "artifacts/playwright/html-report", open: "never" },
    ],
  ],
  use: {
    baseURL: origin,
    ignoreHTTPSErrors: true,
    trace: "retain-on-failure",
    screenshot: "only-on-failure",
    video: "off",
  },
  webServer: {
    command: "node scripts/start-e2e-server.mjs",
    url: `${origin}/health/ready`,
    ignoreHTTPSErrors: true,
    reuseExistingServer: false,
    timeout: 120_000,
    env: {
      APP_ENV: "test",
      STORAGE_MODE: "memory",
      HTTP_ADDRESS: "127.0.0.1:4173",
      PUBLIC_BASE_URL: origin,
    },
  },
  projects: [
    { name: "chromium", use: { ...devices["Desktop Chrome"] } },
    { name: "firefox", use: { ...devices["Desktop Firefox"] } },
    { name: "webkit", use: { ...devices["Desktop Safari"] } },
  ],
})
