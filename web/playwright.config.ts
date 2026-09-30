import { defineConfig } from '@playwright/test'

export default defineConfig({
  testDir: './tests',
  fullyParallel: false,
  workers: 1, // Scenarios share one disposable workspace; keep mutations ordered.
  use: { baseURL: process.env.PCAS_TEST_BASE_URL ?? 'http://127.0.0.1:18090', headless: true, launchOptions: process.env.PCAS_TEST_CHROMIUM_PATH ? { executablePath: process.env.PCAS_TEST_CHROMIUM_PATH } : undefined, trace: 'retain-on-failure' },
  reporter: 'list',
})
