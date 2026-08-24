export type PlatformKind = "server" | "desktop" | "android";

export type PlatformCapabilities = {
  kind: PlatformKind;
  embedded: boolean;
  dashboardPollMs: number;
  liveDashboard: boolean;
  networkEditor: "host" | "vpn-service";
  showAccessSettings: boolean;
  editServerLists: boolean;
  controlWhitelistScan: boolean;
  cancelLongOperations: boolean;
  revealFieldsAfterKeyboard: boolean;
  appUpdater: "server" | "android";
  rebuildEmergencyOnSelection: boolean;
};

const sharedCapabilities = {
  editServerLists: true,
  controlWhitelistScan: true,
  cancelLongOperations: true,
  rebuildEmergencyOnSelection: false,
} as const;

const hostPlatform: PlatformCapabilities = {
  ...sharedCapabilities,
  kind: "server",
  embedded: false,
  dashboardPollMs: 5000,
  liveDashboard: false,
  networkEditor: "host",
  showAccessSettings: true,
  revealFieldsAfterKeyboard: false,
  appUpdater: "server",
};

export function platformCapabilities(): PlatformCapabilities {
  if (typeof window === "undefined") return hostPlatform;
  const host = window as Window & { runtime?: unknown; OrcheRouteAndroid?: unknown };
  if (typeof host.OrcheRouteAndroid === "object") {
    return {
      ...hostPlatform,
      kind: "android",
      embedded: true,
      dashboardPollMs: 1000,
      liveDashboard: true,
      networkEditor: "vpn-service",
      showAccessSettings: false,
      revealFieldsAfterKeyboard: true,
      appUpdater: "android",
    };
  }
  if (typeof host.runtime === "object") {
    return {
      ...hostPlatform,
      kind: "desktop",
      embedded: true,
      showAccessSettings: false,
    };
  }
  return hostPlatform;
}
