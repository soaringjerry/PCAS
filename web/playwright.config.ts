import { defineConfig } from '@playwright/test'

export default defineConfig({
  testDir: './tests',
  fullyParallel: false,
  use: { baseURL: process.env.PCAS_TEST_BASE_URL ?? 'http://127.0.0.1:18090', headless: true, trace: 'retain-on-failure' },
  reporter: 'list',
})
