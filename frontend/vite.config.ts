import { defineConfig } from "vitest/config";
import react from "@vitejs/plugin-react";

// https://vitejs.dev/config/
export default defineConfig({
  plugins: [react()],
  // 単体テスト（Vitest）。src の *.test.ts だけを対象にする（e2e の *.spec.ts は Playwright のテスト）
  test: {
    include: ["src/**/*.test.ts"],
  },
  build: {
    // BigInt support: Chrome 67+, Firefox 68+, Safari 14+, Edge 79+
    target: ["chrome87", "edge88", "firefox78", "safari14"],
  },
});
