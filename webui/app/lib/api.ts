import { platformCapabilities } from "../platform/runtime";

export type ConnectionIdentity = {
  ip: string;
  country_code?: string;
  region?: string;
  flag?: string;
  updated_at?: number;
};

export type OrcheRouteStatus = {
  version: number;
  timestamp: number;
  updated_at: number;
  stale: boolean;
  connectivity: string;
  service?: { enabled: boolean };
  wan: { interface: string; available: boolean | null; mode?: "normal" | "allowlist" | "offline" | "unknown"; identity?: ConnectionIdentity };
  network: {
    capture_mode: "interfaces" | "system" | string;
    direct_interface: string;
    vpn_underlay_interface: string;
  };
  proxy: {
    mode: "auto" | "manual" | "emergency";
    active_node: string | null;
    active_pool: "primary" | "emergency" | "whitelist" | null;
    failure_streak: number;
    last_switch: number;
    manual_until: number;
    identity?: ConnectionIdentity;
  };
  mobile?: {
    state: string;
    message: string;
    permission_granted: boolean;
    engine_ready: boolean;
  };
};

export type Pool = {
  id: "primary" | "emergency" | "whitelist";
  priority: number;
  total: number;
  alive: number;
  selected: boolean;
};

export type Node = {
  id: string;
  display_name: string;
  pool: "primary" | "emergency" | "whitelist";
  priority: number;
  alive: boolean;
  delay_ms: number | null;
  speed_mbps?: number;
  stability_ratio?: number;
  score?: number;
  country?: string;
  selected: boolean;
  source_id?: string | null;
  source_name?: string | null;
};

export type Subscription = {
  id: string;
  name: string;
  group: "primary" | "emergency";
  parser: string;
  enabled: boolean;
  interval_seconds: number;
  last_status: string;
  last_links: number;
  last_error?: string | null;
  last_attempt?: number;
  last_success?: number;
  next_update?: number | null;
  last_tested?: number;
  last_available?: number;
  last_result?: string;
  builtin_default?: boolean;
  description?: string;
  repository?: string;
};

type AcceptedOperation = {
  accepted: boolean;
  already_running?: boolean;
  missing_or_disabled?: boolean;
};

function requireAccepted(result: AcceptedOperation): AcceptedOperation {
  if (result.accepted) return result;
  if (result.already_running) throw new Error("Проверка или обновление уже выполняется.");
  if (result.missing_or_disabled) throw new Error("Подписка выключена или больше не существует.");
  throw new Error("Контроллер не запустил проверку подписки.");
}

export type NetworkRole = { interface: string; gateway: string | null; source: string | null };
export type CaptureConfig = {
  mode: "interfaces" | "system";
  interfaces: string[];
  bypass_local: boolean;
  bypass_cidrs: string[];
  management_cidrs: string[];
  dns_hijack: boolean;
  strict_route: boolean;
};
export type NetworkProfile = {
  version: 1;
  revision: number;
  updated_at: number;
  roles: { direct: NetworkRole; vpn_underlay: NetworkRole };
  capture: CaptureConfig;
  dns: DnsConfig;
};
export type NetworkState = {
  desired: NetworkProfile;
  active: NetworkProfile;
  in_sync: boolean;
  apply: { status?: string; revision?: number; updated_at?: number; error?: string; rolled_back?: boolean; message?: string };
};

export type DnsConfig = {
  direct: string[];
  proxy: string[];
  vpn_underlay: string[];
  bootstrap: string[];
  cache_algorithm: "arc" | "lru";
  prefer_h3: boolean;
  use_hosts: boolean;
  ipv6: boolean;
};

export type RouteState = {
  revision: number;
  default: "proxy" | "direct" | "block";
  lists: { direct: string[]; proxy: string[]; block: string[] };
  stats: Record<string, { compiled_rules?: number; entries?: number }>;
};

export type InterfaceInfo = {
  name: string;
  kind: string;
  state: string;
  loopback: boolean;
  addresses: { family: string; cidr: string; scope: string }[];
};

export type WebAccess = {
  username: string;
  management_cidrs: string[];
  addresses: {
    interface: string;
    address: string;
    cidr: string;
    scope: string;
    http_url: string;
    https_url: string | null;
    certificate_matches: boolean;
  }[];
  https: {
    mode: "auto" | "custom" | "disabled";
    enabled: boolean;
    canonical_url: string | null;
    certificate_name: string | null;
    cert_path: string;
    key_path: string;
    local_ca_available: boolean;
    ca_download_url: string | null;
    error: string | null;
  };
};

export type QualificationPolicy = {
  version: 1;
  defaults: { excluded_countries: string[]; min_speed_mbps: number; stability_ratio: number; tcp_timeout_ms: number; url_timeout_ms: number; geo_timeout_ms: number; speed_timeout_ms: number; url_test_urls: string[]; allowlist_probe_url: string; open_internet_probe_url: string };
  pools: Record<"primary" | "emergency", { url_limit: number; speed_candidates: number; speed_candidates_per_source: number; keep: number; excluded_countries?: string[] | null; min_speed_mbps?: number | null; stability_ratio?: number | null }>;
};

export type QualificationReport = {
  input: number;
  tcp_alive: number;
  url_alive: number;
  speed_tested: number;
  geo_enabled?: boolean;
  geo_passed?: number;
  qualified: number;
  retained: number;
  baseline_mbps?: number;
  baseline_ratio?: number;
  threshold_mbps?: number;
  threshold_source?: "wan_baseline" | "configured_fallback";
  outcomes?: Record<string, number>;
  sources?: Record<string, {
    input: number;
    tcp_alive: number;
    url_alive: number;
    speed_tested: number;
    geo_passed?: number;
    qualified: number;
    retained: number;
    outcomes?: Record<string, number>;
  }>;
};
export type QualificationState = { policy: QualificationPolicy; effective: Record<string, unknown>; reports: Record<string, QualificationReport | null> };

export type ComponentStatus = {
  mihomo: {
    installed: boolean;
    version: string;
    installed_version: string;
    latest_version: string | null;
    update_available: boolean;
    checked_at: number;
    release_url?: string | null;
  };
  geoip: { installed: boolean; updated_at: number; size: number };
  geosite: { installed: boolean; updated_at: number; size: number };
  auto_update: boolean;
  interval_hours: number;
  next_geo_update?: number | null;
  geo_source: "metacubex" | "runetfreedom" | "loyalsoldier" | "custom";
  geoip_url?: string;
  geosite_url?: string;
  geo_sources: { id: string; name: string; description: string; geoip_url: string; geosite_url: string }[];
  installed_geo_source?: { id?: string; name?: string; updated_at?: number };
  catalog?: { geoip?: string[]; geosite?: string[] };
};

export type DashboardData = {
  status: OrcheRouteStatus;
  pools: Pool[];
  nodes: Node[];
  subscriptions: Subscription[];
  network: NetworkState | null;
  interfaces: InterfaceInfo[];
  qualification: QualificationState | null;
  dns: { active: DnsConfig; in_sync: boolean } | null;
  routes: RouteState | null;
  components: ComponentStatus | null;
  operations: OperationSnapshot | null;
  access: WebAccess | null;
  appUpdate: AndroidAppUpdateStatus | null;
};

export type OperationSnapshot = {
  subscription_update: {
    status: "idle" | "queued" | "running" | "cancelling" | "cancelled" | "success" | "warning" | "error";
    phase: string;
    message?: string;
    current?: number;
    total?: number;
    error?: string;
    failures?: string[];
    pools?: Record<string, {
      sources?: number;
      fetched?: number;
      accepted?: number;
      rejected?: number;
      reason?: string;
    }>;
    updated_at: number;
    active: boolean;
    allowlist_scan?: boolean;
    connectivity?: "normal" | "allowlist" | "offline" | "unknown";
  };
  network_apply: {
    status?: string;
    revision?: number;
    error?: string;
    rolled_back?: boolean;
    updated_at?: number;
    message?: string;
    active?: boolean;
  };
  component_update: {
    status: "idle" | "queued" | "running" | "success" | "error";
    phase: string;
    message?: string;
    error?: string;
    current?: number;
    total?: number;
    updated_at: number;
    active: boolean;
  };
};

type RequestOptions = RequestInit & { body?: string };

type AndroidRuntimeResponse = {
  status: number;
  body: unknown;
};

type AndroidRuntimeBridge = {
  request(method: string, path: string, body: string): string;
  scanQr?: () => void;
  openTextFile?: () => void;
  saveTextFile?: (filename: string, content: string) => void;
  appUpdateStatus?: () => string;
  checkAppUpdate?: () => boolean;
  installAppUpdate?: () => boolean;
	setAppUpdateBetaEnabled?: (enabled: boolean) => boolean;
};

export type AndroidAppUpdateStatus = {
  state: "idle" | "checking" | "current" | "available" | "downloading" | "permission" | "installer" | "error";
  message: string;
  current_version: string;
  current_version_code?: number;
  latest_version?: string;
  latest_version_code?: number;
  current?: number;
  total?: number;
  error?: string;
  active: boolean;
	current_prerelease?: boolean;
	beta_enabled: boolean;
	supported?: boolean;
	update_available?: boolean;
	channel?: "stable" | "beta";
};

function androidBridge() {
  return typeof window !== "undefined"
    ? (window as Window & { OrcheRouteAndroid?: AndroidRuntimeBridge }).OrcheRouteAndroid
    : undefined;
}

export function canScanQr() {
  return typeof androidBridge()?.scanQr === "function";
}

export function scanQr() {
  const bridge = androidBridge();
  if (typeof bridge?.scanQr !== "function") throw new Error("qr_scanner_unavailable");
  bridge.scanQr();
}

export function canOpenTextFile() {
  return typeof androidBridge()?.openTextFile === "function";
}

export function openTextFile() {
  const bridge = androidBridge();
  if (typeof bridge?.openTextFile !== "function") return false;
  bridge.openTextFile();
  return true;
}

export function saveTextFile(filename: string, content: string) {
  const bridge = androidBridge();
  if (typeof bridge?.saveTextFile !== "function") return false;
  bridge.saveTextFile(filename, content);
  return true;
}

export function getAndroidAppUpdateStatus(): AndroidAppUpdateStatus | null {
  const bridge = androidBridge();
  if (typeof bridge?.appUpdateStatus !== "function") return null;
  try {
    return JSON.parse(bridge.appUpdateStatus()) as AndroidAppUpdateStatus;
  } catch {
    return null;
  }
}

export function checkAndroidAppUpdate() {
  const bridge = androidBridge();
  return typeof bridge?.checkAppUpdate === "function" && bridge.checkAppUpdate();
}

export function installAndroidAppUpdate() {
  const bridge = androidBridge();
  return typeof bridge?.installAppUpdate === "function" && bridge.installAppUpdate();
}

export function setAndroidAppUpdateBetaEnabled(enabled: boolean) {
	const bridge = androidBridge();
	return typeof bridge?.setAppUpdateBetaEnabled === "function" && bridge.setAppUpdateBetaEnabled(enabled);
}

export function getServerAppUpdateStatus() { return request<AndroidAppUpdateStatus>("/v1/app-update"); }

async function request<T>(path: string, options: RequestOptions = {}): Promise<T> {
  const android = androidBridge();
  if (android && typeof android.request === "function") {
    const method = options.method ?? "GET";
    const raw = android.request(method, path, options.body ?? "");
    let result: AndroidRuntimeResponse;
    try {
      result = JSON.parse(raw) as AndroidRuntimeResponse;
    } catch {
      throw new Error("mobile_runtime_invalid_response");
    }
    if (result.status < 200 || result.status >= 300) {
      const payload = result.body as { error?: string; message?: string } | null;
      throw new Error(payload?.message ?? payload?.error ?? `HTTP ${result.status}`);
    }
    return result.body as T;
  }

  const response = await fetch(`/api${path}`, {
    ...options,
    headers: {
      Accept: "application/json",
      ...(options.body ? { "Content-Type": "application/json" } : {}),
      ...(options.method && options.method !== "GET" ? { "X-OrcheRoute-UI": "1" } : {}),
      ...options.headers,
    },
    cache: "no-store",
  });

  const payload = await response.json().catch(() => ({}));
  if (!response.ok) {
    const reason = typeof payload.error === "string" ? payload.error : `HTTP ${response.status}`;
    throw new Error(reason);
  }
  return payload as T;
}

export async function loadDashboard(): Promise<DashboardData> {
  const [status, pools, nodes, subscriptions, network, interfaces, qualification, dns, routes, components, operations, access, appUpdate] = await Promise.all([
    request<OrcheRouteStatus>("/v1/status"),
    request<{ pools: Pool[] }>("/v1/pools").catch(() => ({ pools: [] })),
    request<{ nodes: Node[] }>("/v1/nodes").catch(() => ({ nodes: [] })),
    request<{ subscriptions: Subscription[] }>("/v1/subscriptions").catch(() => ({ subscriptions: [] })),
    request<NetworkState>("/v1/network/profile").catch(() => null),
    request<{ interfaces: InterfaceInfo[] }>("/v1/network/interfaces").catch(() => ({ interfaces: [] })),
    request<QualificationState>("/v1/qualification").catch(() => null),
    request<{ active: DnsConfig; in_sync: boolean }>("/v1/dns").catch(() => null),
    request<RouteState>("/v1/routes").catch(() => null),
    request<ComponentStatus>("/v1/components").catch(() => null),
    request<OperationSnapshot>("/v1/operations").catch(() => null),
    platformCapabilities().showAccessSettings ? request<WebAccess>("/v1/web/access").catch(() => null) : Promise.resolve(null),
    platformCapabilities().appUpdater === "android" ? Promise.resolve(getAndroidAppUpdateStatus()) : request<AndroidAppUpdateStatus>("/v1/app-update").catch(() => null),
  ]);
  return {
    status,
    pools: pools.pools,
    nodes: nodes.nodes,
    subscriptions: subscriptions.subscriptions,
    network,
    interfaces: interfaces.interfaces,
    qualification,
    dns,
    routes,
    components,
    operations,
    access,
    appUpdate,
  };
}

export async function loadLiveDashboard(current: DashboardData): Promise<DashboardData> {
  const [status, pools, nodes, operations] = await Promise.all([
    request<OrcheRouteStatus>("/v1/status"),
    request<{ pools: Pool[] }>("/v1/pools").catch(() => ({ pools: current.pools })),
    request<{ nodes: Node[] }>("/v1/nodes").catch(() => ({ nodes: current.nodes })),
    request<OperationSnapshot>("/v1/operations").catch(() => current.operations),
  ]);
  return { ...current, status, pools: pools.pools, nodes: nodes.nodes, operations };
}

export function loadOperations(): Promise<OperationSnapshot> {
  return request<OperationSnapshot>("/v1/operations");
}

export const actions = {
  setEnabled(enabled: boolean) {
    return request<{ accepted: boolean; enabled: boolean }>(
      `/v1/service/${enabled ? "enable" : "disable"}`,
      { method: "POST", body: "{}" },
    );
  },
  setAuto() {
    return request<{ accepted: boolean }>("/v1/control/auto", { method: "POST", body: "{}" });
  },
  setEmergency() {
    return request<{ accepted: boolean }>("/v1/control/emergency", { method: "POST", body: "{}" });
  },
  setManual(nodeId: string) {
    return request<{ accepted: boolean }>("/v1/control/manual", {
      method: "POST",
      body: JSON.stringify({ node_id: nodeId, lock_seconds: 0 }),
    });
  },
  refreshSubscriptions() {
    return request<AcceptedOperation>("/v1/subscriptions/refresh", { method: "POST", body: "{}" }).then(requireAccepted);
  },
  checkServers() {
    return request<AcceptedOperation>("/v1/subscriptions/check", { method: "POST", body: "{}" }).then(requireAccepted);
  },
  cancelSubscriptionUpdate() {
    return request<{ accepted: boolean; active: boolean }>("/v1/operations/subscription-update/cancel", { method: "POST", body: "{}" });
  },
  scanWhitelistPool() {
    return request<AcceptedOperation>("/v1/whitelist/scan", { method: "POST", body: "{}" }).then(requireAccepted);
  },
  refreshSubscription(id: string) {
    return request<AcceptedOperation>(`/v1/subscriptions/${encodeURIComponent(id)}/refresh`, { method: "POST", body: "{}" }).then(requireAccepted);
  },
  checkSubscription(id: string) {
    return request<AcceptedOperation>(`/v1/subscriptions/${encodeURIComponent(id)}/check`, { method: "POST", body: "{}" }).then(requireAccepted);
  },
  revealSubscriptionSecret(id: string) {
    return request<{ id: string; secret: string }>(`/v1/subscriptions/${encodeURIComponent(id)}/secret`, { method: "POST", body: "{}" });
  },
  exportSubscription(id: string) {
    return request<{ id: string; name: string; parser: string; secret: string; links: string[] }>(`/v1/subscriptions/${encodeURIComponent(id)}/export`, { method: "POST", body: "{}" });
  },
  updateComponents(component: "check" | "geo" | "core" | "all") {
    return request<{ accepted: boolean }>("/v1/components/update", { method: "POST", body: JSON.stringify({ component }) });
  },
  checkAppUpdate(beta_enabled: boolean) { return request<{accepted:boolean}>("/v1/app-update/check", {method:"POST", body:JSON.stringify({beta_enabled})}); },
  installAppUpdate(beta_enabled: boolean) { return request<{accepted:boolean}>("/v1/app-update/install", {method:"POST", body:JSON.stringify({beta_enabled})}); },
  updateComponentSettings(payload: { geo_auto_update: boolean; geo_interval_hours: number; geo_source: string; geoip_url?: string; geosite_url?: string }) {
    return request<{ updated: boolean }>("/v1/components/settings", {
      method: "PUT",
      body: JSON.stringify(payload),
    });
  },
  updateQualification(defaults: QualificationPolicy["defaults"], pools?: Partial<QualificationPolicy["pools"]>) {
    return request<{ updated: boolean; policy: QualificationPolicy }>("/v1/qualification/policy", {
      method: "PUT",
      body: JSON.stringify({ defaults, ...(pools ? { pools } : {}) }),
    });
  },
  updateWebAccess(username: string, password: string) {
    return request<{ updated: boolean; username: string; reauthentication_required: boolean }>("/v1/web/access", {
      method: "PUT",
      body: JSON.stringify({ section: "credentials", username, password }),
    });
  },
  updateWebTls(payload: { mode: "auto" | "custom" | "disabled"; cert_path?: string; key_path?: string; certificate_name?: string }) {
    return request<{ updated: boolean; restart_required: boolean }>("/v1/web/access", {
      method: "PUT",
      body: JSON.stringify({ section: "tls", ...payload }),
    });
  },
  validateNetwork(profile: NetworkProfile) {
    return request<{ profile: NetworkProfile }>("/v1/network/validate", { method: "POST", body: JSON.stringify({ profile }) });
  },
  saveNetwork(revision: number, profile: NetworkProfile) {
    return request<{ updated: boolean; desired: NetworkProfile }>("/v1/network/profile", { method: "PUT", body: JSON.stringify({ revision, profile }) });
  },
  applyNetwork(revision: number, confirmSystemCapture = false) {
    return request<{ accepted: boolean; revision: number }>("/v1/network/apply", { method: "POST", body: JSON.stringify({ revision, confirm_system_capture: confirmSystemCapture }) });
  },
  validateDns(dns: DnsConfig) {
    return request<{ config: DnsConfig }>("/v1/dns/validate", { method: "POST", body: JSON.stringify({ dns }) });
  },
  saveDns(revision: number, dns: DnsConfig) {
    return request<{ updated: boolean; network_revision: number }>("/v1/dns", { method: "PUT", body: JSON.stringify({ revision, dns }) });
  },
  validateRoutes(defaultAction: RouteState["default"], lists: RouteState["lists"]) {
    return request<{ valid: boolean }>("/v1/routes/validate", { method: "POST", body: JSON.stringify({ default: defaultAction, lists }) });
  },
  saveRoutes(revision: number, defaultAction: RouteState["default"], lists: RouteState["lists"]) {
    return request<{ updated: boolean; applied: boolean; apply_pending?: boolean; routes: RouteState }>("/v1/routes", { method: "PUT", body: JSON.stringify({ revision, default: defaultAction, lists }) });
  },
  createSubscription(payload: { name: string; group: "primary" | "emergency"; parser: string; secret: string; enabled: boolean; interval_seconds: number }) {
    return request<{ subscription: Subscription }>("/v1/subscriptions", { method: "POST", body: JSON.stringify(payload) });
  },
  importSubscriptions(subscriptions: { name: string; group: "primary" | "emergency"; parser: string; secret: string; enabled: boolean; interval_seconds: number }[]) {
    return request<{ created: Subscription[]; skipped: { name: string; reason: string }[]; refresh_scheduled: boolean }>("/v1/subscriptions/import", { method: "POST", body: JSON.stringify({ subscriptions }) });
  },
  updateSubscription(id: string, payload: Partial<{ name: string; group: "primary" | "emergency"; parser: string; secret: string; enabled: boolean; interval_seconds: number }>) {
    return request<{ subscription: Subscription }>(`/v1/subscriptions/${encodeURIComponent(id)}`, { method: "PATCH", body: JSON.stringify(payload) });
  },
  updateDefaultEmergency(enabledIds: string[]) {
    return request<{ updated: boolean; refresh_scheduled: boolean }>("/v1/subscriptions/default-emergency", {
      method: "PUT",
      body: JSON.stringify({ enabled_ids: enabledIds }),
    });
  },
  deleteSubscription(id: string) {
    return request<{ deleted: boolean }>(`/v1/subscriptions/${encodeURIComponent(id)}`, { method: "DELETE", body: "{}" });
  },
  deleteNode(id: string) {
    return request<{
      deleted: boolean;
      node: { id: string; pool: Node["pool"]; was_selected: boolean; remaining: number };
    }>(`/v1/nodes/${encodeURIComponent(id)}`, { method: "DELETE", body: "{}" });
  },
};
