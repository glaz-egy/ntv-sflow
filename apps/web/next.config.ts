import path from "node:path";
import type { NextConfig } from "next";

// Docker builds set NEXT_OUTPUT_STANDALONE=1 to get a self-contained server
// (see /Dockerfile). Local `next start` keeps the default output.
const standalone = process.env.NEXT_OUTPUT_STANDALONE === "1";

const nextConfig: NextConfig = {
  output: standalone ? "standalone" : undefined,
  // pnpm workspace: trace dependencies from the repository root.
  // `next build` runs with apps/web as the working directory.
  outputFileTracingRoot: standalone ? path.resolve(process.cwd(), "../..") : undefined,
};

export default nextConfig;
