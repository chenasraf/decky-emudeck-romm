import { defineConfig } from "vite";
import { fileURLToPath, URL } from "node:url";

// Local browser playground for previewing components from src/.
// NOT used in production builds (rollup.config.js handles that) and not
// used in tests (vitest has its own config). Run with `pnpm playground`.
//
// @decky/api is aliased to a browser mock so callable() returns a stub
// instead of trying to bridge to a non-existent Decky loader.
// @decky/ui resolves normally — it's a plain React component library and
// renders in any browser, though styles target Steam's Big Picture
// context so visuals are approximate.

export default defineConfig({
  root: "playground",
  resolve: {
    alias: {
      "@decky/api": fileURLToPath(new URL("./playground/mocks/decky-api.ts", import.meta.url)),
      "@decky/ui": fileURLToPath(new URL("./playground/mocks/decky-ui.tsx", import.meta.url)),
    },
  },
  esbuild: {
    jsx: "automatic",
    jsxImportSource: "react",
  },
  server: {
    port: 5174,
    open: true,
    proxy: {
      "/api": "http://localhost:5175",
      "/events": { target: "ws://localhost:5175", ws: true },
    },
  },
});
