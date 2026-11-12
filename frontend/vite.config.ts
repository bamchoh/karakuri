import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";

// https://vitejs.dev/config/
export default defineConfig({
  plugins: [react()],
  build: {
    // BigInt support: Chrome 67+, Firefox 68+, Safari 14+, Edge 79+
    target: ["chrome87", "edge88", "firefox78", "safari14"],
  },
});
