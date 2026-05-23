import { readFileSync } from "node:fs";
import { join } from "node:path";
import { describe, it, expect } from "vitest";
import { DISPLAY_NAME, CLIENT_NAME } from "./branding";

// Wire-contract assertions — if these drift, the fork's identity is split
// across files and shortcuts/device-registry collisions with upstream
// become possible again. Read the JSON files at runtime via fs instead of
// `import … from "*.json"` so the rollup bundle never has to follow these
// imports (it would otherwise try to compile the JSON into dist/).

function readJson(relPath: string): Record<string, unknown> {
  const text = readFileSync(join(__dirname, "..", relPath), "utf-8");
  return JSON.parse(text) as Record<string, unknown>;
}

describe("branding", () => {
  it("DISPLAY_NAME equals plugin.json:name", () => {
    expect(DISPLAY_NAME).toBe(readJson("plugin.json").name);
  });

  it("CLIENT_NAME equals package.json:name", () => {
    expect(CLIENT_NAME).toBe(readJson("package.json").name);
  });
});
