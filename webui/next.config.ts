import type { NextConfig } from "next";
import { readFileSync } from "node:fs";
import { resolve } from "node:path";

type ReleaseMetadata = { version: string; channel: "stable" | "beta" };

const release = JSON.parse(
  readFileSync(resolve(process.cwd(), "../release.json"), "utf8"),
) as ReleaseMetadata;

if (!release.version || !["stable", "beta"].includes(release.channel)) {
  throw new Error("release.json must contain a version and stable/beta channel");
}

const nextConfig: NextConfig = {
  output: "export",
  trailingSlash: true,
  images: { unoptimized: true },
  generateBuildId: async () => "orcheroute-static",
  env: {
    NEXT_PUBLIC_ORCHEROUTE_VERSION: release.version,
    NEXT_PUBLIC_ORCHEROUTE_RELEASE_CHANNEL: release.channel,
  },
};

export default nextConfig;
