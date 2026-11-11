import { defineConfig } from "@playwright/test";

// 画面の確認用テスト。先に `task dev` で開発サーバーを起動しておく（e2e/README.md 参照）。
export default defineConfig({
  testDir: "./e2e",
  outputDir: "./e2e/.results",
  // テストは 1 つのアプリ（1 つのバックエンド）を共有するため、並列にしない
  workers: 1,
  fullyParallel: false,
  retries: 0,
  reporter: [["list"]],
  use: {
    baseURL: process.env.KARAKURI_DEV_URL || "http://localhost:34115",
    viewport: { width: 1400, height: 900 },
    screenshot: "only-on-failure",
  },
  projects: [{ name: "chromium", use: { browserName: "chromium" } }],
});
