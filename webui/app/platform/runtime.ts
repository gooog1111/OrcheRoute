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

const hostPlatform: PlatformCapabilities = {
  kind: "server",
  embedded: false,
  dashboardPollMs: 5000,
  liveDashboard: false,
  networkEditor: "host",
  showAccessSettings: true,
  editServerLists: false,
  controlWhitelistScan: false,
  cancelLongOperations: false,
  revealFieldsAfterKeyboard: false,
  appUpdater: "server",
  rebuildEmergencyOnSelection: true,
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
      editServerLists: true,
      controlWhitelistScan: true,
      cancelLongOperations: true,
      revealFieldsAfterKeyboard: true,
      appUpdater: "android",
      rebuildEmergencyOnSelection: false,
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
