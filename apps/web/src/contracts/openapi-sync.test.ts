/**
 * Guards D-021: src/contracts/generated/openapi.ts must be regenerated
 * whenever api/openapi.yaml changes (`pnpm gen:api`).
 */
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import openapiTS, { astToString } from "openapi-typescript";
import { format, resolveConfig } from "prettier";
import { expect, it } from "vitest";

it("generated contract types match api/openapi.yaml", async () => {
  const spec = new URL("../../../../api/openapi.yaml", import.meta.url);
  const generatedPath = fileURLToPath(new URL("./generated/openapi.ts", import.meta.url));
  const config = (await resolveConfig(generatedPath)) ?? {};
  const fresh = await format(astToString(await openapiTS(spec)), { ...config, filepath: generatedPath });
  // The CLI prepends a "do not edit" banner that the programmatic API omits.
  const stored = readFileSync(generatedPath, "utf8").replace(/^\/\*\*[\s\S]*?\*\/\s*/, "");
  expect(stored, "run `pnpm gen:api`").toBe(fresh);
}, 30_000);
