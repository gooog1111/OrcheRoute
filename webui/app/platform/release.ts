export type ReleaseChannel = "stable" | "beta";

const configuredChannel = process.env.NEXT_PUBLIC_ORCHEROUTE_RELEASE_CHANNEL;
const channel: ReleaseChannel = configuredChannel === "beta" ? "beta" : "stable";

export const releaseBranding = Object.freeze({
  version: process.env.NEXT_PUBLIC_ORCHEROUTE_VERSION ?? "0.0.0-dev",
  channel,
  prerelease: channel === "beta",
  badge: channel === "beta" ? "BETA" : "",
  applicationName: channel === "beta" ? "OrcheRoute BETA" : "OrcheRoute",
});
