const storageKey = "porthook.dashboard.adminToken";
const gatewayStorageKey = "porthook.dashboard.gatewayURL";
const dashboardHashPrefix = "ops";

const auditEventStateFields = [
  ["ae_event", "auditEventEvent"],
  ["ae_level", "auditEventLevel"],
  ["ae_request_id", "auditEventRequestID"],
  ["ae_remote_ip", "auditEventRemoteIP"],
  ["ae_field", "auditEventField"],
  ["ae_limit", "auditEventLimit"],
];

const requestLogStateFields = [
  ["rl_subdomain", "requestLogSubdomain"],
  ["rl_method", "requestLogMethod"],
  ["rl_host", "requestLogHost"],
  ["rl_path", "requestLogPath"],
  ["rl_status", "requestLogStatus"],
  ["rl_outcome", "requestLogOutcome"],
  ["rl_request_id", "requestLogRequestID"],
  ["rl_tunnel_id", "requestLogTunnelID"],
  ["rl_since", "requestLogSince"],
  ["rl_until", "requestLogUntil"],
  ["rl_limit", "requestLogLimit"],
];

const elements = {
  loginPanel: document.querySelector("#login-panel"),
  appPanel: document.querySelector("#app-panel"),
  loginForm: document.querySelector("#login-form"),
  adminToken: document.querySelector("#admin-token"),
  logoutButton: document.querySelector("#logout-button"),
  refreshButton: document.querySelector("#refresh-button"),
  exportButton: document.querySelector("#export-button"),
  exportCount: document.querySelector("#export-count"),
  createForm: document.querySelector("#create-form"),
  tokenName: document.querySelector("#token-name"),
  scopeRegisterTunnel: document.querySelector("#scope-register-tunnel"),
  tokensBody: document.querySelector("#tokens-body"),
  emptyState: document.querySelector("#empty-state"),
  tokenCount: document.querySelector("#token-count"),
  notice: document.querySelector("#notice"),
  createdTokenPanel: document.querySelector("#created-token-panel"),
  createdTokenValue: document.querySelector("#created-token-value"),
  copyCreatedToken: document.querySelector("#copy-created-token"),
  adminTokenForm: document.querySelector("#admin-token-form"),
  adminTokenName: document.querySelector("#admin-token-name"),
  adminTokenScopeInputs: [...document.querySelectorAll("[data-admin-scope]")],
  adminTokensBody: document.querySelector("#admin-tokens-body"),
  adminTokensEmptyState: document.querySelector("#admin-tokens-empty-state"),
  adminTokenCount: document.querySelector("#admin-token-count"),
  createdAdminTokenPanel: document.querySelector("#created-admin-token-panel"),
  createdAdminTokenValue: document.querySelector("#created-admin-token-value"),
  copyCreatedAdminToken: document.querySelector("#copy-created-admin-token"),
  readyStatus: document.querySelector("#ready-status"),
  versionStatus: document.querySelector("#version-status"),
  reservationForm: document.querySelector("#reservation-form"),
  reservationName: document.querySelector("#reservation-name"),
  reservationToken: document.querySelector("#reservation-token"),
  reservationsBody: document.querySelector("#reservations-body"),
  reservationsEmptyState: document.querySelector("#reservations-empty-state"),
  reservationCount: document.querySelector("#reservation-count"),
  customDomainForm: document.querySelector("#custom-domain-form"),
  customDomainHostname: document.querySelector("#custom-domain-hostname"),
  customDomainReservation: document.querySelector("#custom-domain-reservation"),
  customDomainsBody: document.querySelector("#custom-domains-body"),
  customDomainsEmptyState: document.querySelector("#custom-domains-empty-state"),
  customDomainCount: document.querySelector("#custom-domain-count"),
  accessPolicyForm: document.querySelector("#access-policy-form"),
  accessReservation: document.querySelector("#access-reservation"),
  accessMode: document.querySelector("#access-mode"),
  basicUsernameField: document.querySelector("#basic-username-field"),
  basicUsername: document.querySelector("#basic-username"),
  policySecretField: document.querySelector("#policy-secret-field"),
  policySecret: document.querySelector("#policy-secret"),
  ipAllowlistField: document.querySelector("#ip-allowlist-field"),
  ipAllowlist: document.querySelector("#ip-allowlist"),
  accessPolicySubmit: document.querySelector("#access-policy-submit"),
  accessPolicyCancel: document.querySelector("#access-policy-cancel"),
  accessPoliciesBody: document.querySelector("#access-policies-body"),
  accessPoliciesEmptyState: document.querySelector("#access-policies-empty-state"),
  accessPolicyCount: document.querySelector("#access-policy-count"),
  auditEventForm: document.querySelector("#audit-event-form"),
  auditEventEvent: document.querySelector("#audit-event-event"),
  auditEventLevel: document.querySelector("#audit-event-level"),
  auditEventRequestID: document.querySelector("#audit-event-request-id"),
  auditEventRemoteIP: document.querySelector("#audit-event-remote-ip"),
  auditEventField: document.querySelector("#audit-event-field"),
  auditEventLimit: document.querySelector("#audit-event-limit"),
  auditEventsBody: document.querySelector("#audit-events-body"),
  auditEventsEmptyState: document.querySelector("#audit-events-empty-state"),
  auditEventCount: document.querySelector("#audit-event-count"),
  auditEventLoadMore: document.querySelector("#audit-event-load-more"),
  diagnosticsForm: document.querySelector("#diagnostics-form"),
  diagnosticsBody: document.querySelector("#diagnostics-body"),
  diagnosticsEmptyState: document.querySelector("#diagnostics-empty-state"),
  diagnosticsCount: document.querySelector("#diagnostics-count"),
  gatewayRuntimeGrid: document.querySelector("#gateway-runtime-grid"),
  gatewayRuntimeEmptyState: document.querySelector("#gateway-runtime-empty-state"),
  gatewayRuntimeCount: document.querySelector("#gateway-runtime-count"),
  metricsBody: document.querySelector("#metrics-body"),
  metricsEmptyState: document.querySelector("#metrics-empty-state"),
  metricsCount: document.querySelector("#metrics-count"),
  gatewayForm: document.querySelector("#gateway-form"),
  gatewayURL: document.querySelector("#gateway-url"),
  tunnelsBody: document.querySelector("#tunnels-body"),
  tunnelsEmptyState: document.querySelector("#tunnels-empty-state"),
  tunnelCount: document.querySelector("#tunnel-count"),
  tunnelDetailPanel: document.querySelector("#tunnel-detail-panel"),
  tunnelDetailTitle: document.querySelector("#tunnel-detail-title"),
  tunnelDetailMeta: document.querySelector("#tunnel-detail-meta"),
  tunnelDetailGrid: document.querySelector("#tunnel-detail-grid"),
  tunnelDetailClose: document.querySelector("#tunnel-detail-close"),
  overviewCount: document.querySelector("#overview-count"),
  overviewActiveTunnels: document.querySelector("#overview-active-tunnels"),
  overviewRecentRequests: document.querySelector("#overview-recent-requests"),
  overviewErrorRate: document.querySelector("#overview-error-rate"),
  overviewP95Latency: document.querySelector("#overview-p95-latency"),
  overviewOutcomeCount: document.querySelector("#overview-outcome-count"),
  overviewOutcomeChart: document.querySelector("#overview-outcome-chart"),
  overviewStatusCount: document.querySelector("#overview-status-count"),
  overviewStatusChart: document.querySelector("#overview-status-chart"),
  requestLogForm: document.querySelector("#request-log-form"),
  requestLogSubdomain: document.querySelector("#request-log-subdomain"),
  requestLogMethod: document.querySelector("#request-log-method"),
  requestLogHost: document.querySelector("#request-log-host"),
  requestLogPath: document.querySelector("#request-log-path"),
  requestLogStatus: document.querySelector("#request-log-status"),
  requestLogOutcome: document.querySelector("#request-log-outcome"),
  requestLogRequestID: document.querySelector("#request-log-request-id"),
  requestLogTunnelID: document.querySelector("#request-log-tunnel-id"),
  requestLogSince: document.querySelector("#request-log-since"),
  requestLogUntil: document.querySelector("#request-log-until"),
  requestLogLimit: document.querySelector("#request-log-limit"),
  requestLogsBody: document.querySelector("#request-logs-body"),
  requestLogsEmptyState: document.querySelector("#request-logs-empty-state"),
  requestLogCount: document.querySelector("#request-log-count"),
  requestLogLoadMore: document.querySelector("#request-log-load-more"),
};

let adminToken = sessionStorage.getItem(storageKey) || "";
let currentTokens = [];
let currentAdminTokens = [];
let currentReservations = [];
let currentCustomDomains = [];
let currentAccessPolicies = [];
let currentAuditEvents = [];
let currentDiagnostics = [];
let currentGatewayRuntime = null;
let currentMetrics = [];
let currentTunnels = [];
let currentRequestLogs = [];
let auditEventNextCursor = "";
let requestLogNextCursor = "";
let editingAccessPolicyID = "";
let selectedTunnelID = "";
let restoringFilterState = false;

elements.gatewayURL.value = sessionStorage.getItem(gatewayStorageKey) || defaultGatewayURL();
restoreFilterStateFromHash();

function setAuthenticated(authenticated) {
  elements.loginPanel.hidden = authenticated;
  elements.appPanel.hidden = !authenticated;
  elements.logoutButton.hidden = !authenticated;
  elements.refreshButton.disabled = !authenticated;
  elements.exportButton.disabled = !authenticated;
  if (!authenticated) {
    currentTokens = [];
    currentAdminTokens = [];
    currentReservations = [];
    currentCustomDomains = [];
    currentAccessPolicies = [];
    currentAuditEvents = [];
    currentDiagnostics = [];
    currentGatewayRuntime = null;
    currentMetrics = [];
    currentTunnels = [];
    currentRequestLogs = [];
    auditEventNextCursor = "";
    requestLogNextCursor = "";
    editingAccessPolicyID = "";
    selectedTunnelID = "";
    elements.tokensBody.replaceChildren();
    elements.adminTokensBody.replaceChildren();
    elements.reservationsBody.replaceChildren();
    elements.customDomainsBody.replaceChildren();
    elements.accessPoliciesBody.replaceChildren();
    elements.auditEventsBody.replaceChildren();
    elements.diagnosticsBody.replaceChildren();
    elements.gatewayRuntimeGrid.replaceChildren();
    elements.metricsBody.replaceChildren();
    elements.tunnelsBody.replaceChildren();
    elements.requestLogsBody.replaceChildren();
    elements.emptyState.hidden = true;
    elements.adminTokensEmptyState.hidden = true;
    elements.reservationsEmptyState.hidden = true;
    elements.customDomainsEmptyState.hidden = true;
    elements.accessPoliciesEmptyState.hidden = true;
    elements.auditEventsEmptyState.hidden = true;
    elements.diagnosticsEmptyState.hidden = true;
    elements.gatewayRuntimeEmptyState.hidden = true;
    elements.metricsEmptyState.hidden = true;
    elements.tunnelsEmptyState.hidden = true;
    elements.requestLogsEmptyState.hidden = true;
    updateAuditEventLoadMore();
    updateRequestLogLoadMore();
    elements.tokenCount.textContent = "No tokens loaded";
    elements.adminTokenCount.textContent = "No admin tokens loaded";
    elements.reservationCount.textContent = "No reservations loaded";
    elements.customDomainCount.textContent = "No custom domains loaded";
    elements.accessPolicyCount.textContent = "No access policies loaded";
    elements.auditEventCount.textContent = "No audit events loaded";
    elements.diagnosticsCount.textContent = "No diagnostics run";
    elements.gatewayRuntimeCount.textContent = "No gateway runtime loaded";
    elements.metricsCount.textContent = "No metrics loaded";
    elements.tunnelCount.textContent = "No tunnels loaded";
    elements.requestLogCount.textContent = "No request logs loaded";
    elements.exportCount.textContent = "No export downloaded";
    clearTunnelDetail();
    clearCreatedAdminToken();
    resetAdminTokenScopes();
    renderOperationalOverview();
    renderReservationTokenOptions();
    renderCustomDomainReservationOptions();
    renderAccessReservationOptions();
    resetAccessPolicyForm();
  }
}

function showNotice(message, tone = "info") {
  elements.notice.textContent = message;
  elements.notice.className = `notice ${tone}`;
  elements.notice.hidden = !message;
}

function clearCreatedToken() {
  elements.createdTokenValue.textContent = "";
  elements.createdTokenPanel.hidden = true;
}

function clearCreatedAdminToken() {
  elements.createdAdminTokenValue.textContent = "";
  elements.createdAdminTokenPanel.hidden = true;
}

async function apiRequest(path, options = {}) {
  const headers = new Headers(options.headers || {});
  headers.set("Authorization", `Bearer ${adminToken}`);
  if (options.body) {
    headers.set("Content-Type", "application/json");
  }

  const response = await fetch(path, { ...options, headers });
  if (response.status === 204) {
    return null;
  }

  const payload = await readPayload(response);
  if (!response.ok) {
    if (response.status === 401) {
      throw new Error("Admin token was rejected. Log in again with a valid token.");
    }
    if (response.status === 403) {
      throw new Error("Admin token does not have permission for this action.");
    }
    throw new Error(typeof payload === "string" && payload ? payload : `Request failed with status ${response.status}.`);
  }

  return payload;
}

async function readPayload(response) {
  const text = await response.text();
  if (!text) {
    return null;
  }
  try {
    return JSON.parse(text);
  } catch {
    return text.trim();
  }
}

async function refreshStatus() {
  try {
    const response = await fetch("/api/v1/status", { cache: "no-store" });
    const payload = await response.json();
    elements.versionStatus.textContent = `Version ${payload.version || "unknown"}`;
    if (!response.ok || !payload.ready) {
      throw new Error(payload.error || "not ready");
    }
    elements.readyStatus.textContent = "Ready";
    elements.readyStatus.className = "status-pill good";
  } catch {
    elements.readyStatus.textContent = "Not ready";
    elements.readyStatus.className = "status-pill bad";
  }
}

async function refreshApp() {
  showNotice("");
  clearCreatedToken();
  clearCreatedAdminToken();
  await Promise.all([loadTokens(), loadAdminTokens(), loadAuditEvents(), refreshStatus(), loadTunnels({ silent: true }), loadGatewayRuntime({ silent: true }), loadGatewayMetrics({ silent: true }), loadRequestLogs({ silent: true })]);
  await loadReservations();
  await loadCustomDomains();
  await loadAccessPolicies();
}

async function loadTokens() {
  elements.tokenCount.textContent = "Loading tokens";
  elements.tokensBody.replaceChildren();
  elements.emptyState.hidden = true;

  const payload = await apiRequest("/api/v1/tokens");
  currentTokens = payload.tokens || [];
  renderTokens(currentTokens);
  renderReservationTokenOptions();
}

function renderTokens(tokens) {
  elements.tokensBody.replaceChildren(...tokens.map(renderTokenRow));
  elements.emptyState.hidden = tokens.length !== 0;
  elements.tokenCount.textContent = `${tokens.length} token${tokens.length === 1 ? "" : "s"}`;
}

async function loadAdminTokens() {
  elements.adminTokenCount.textContent = "Loading admin tokens";
  elements.adminTokensBody.replaceChildren();
  elements.adminTokensEmptyState.hidden = true;

  const payload = await apiRequest("/api/v1/admin-tokens");
  currentAdminTokens = payload.tokens || [];
  renderAdminTokens(currentAdminTokens);
}

function renderAdminTokens(tokens) {
  elements.adminTokensBody.replaceChildren(...tokens.map(renderAdminTokenRow));
  elements.adminTokensEmptyState.hidden = tokens.length !== 0;
  elements.adminTokenCount.textContent = `${tokens.length} admin token${tokens.length === 1 ? "" : "s"}`;
}

function renderAdminTokenRow(token) {
  const row = document.createElement("tr");
  if (token.revoked_at) {
    row.classList.add("revoked");
  }

  row.append(
    cell(token.name),
    monoCell(token.id),
    cell((token.scopes || []).join(", ") || "none"),
    cell(formatTime(token.created_at)),
    cell(token.last_used_at ? formatTime(token.last_used_at) : "Never"),
    statusCell(token),
    adminTokenActionCell(token),
  );
  return row;
}

function renderTokenRow(token) {
  const row = document.createElement("tr");
  if (token.revoked_at) {
    row.classList.add("revoked");
  }

  row.append(
    cell(token.name),
    monoCell(token.id),
    cell((token.scopes || []).join(", ") || "none"),
    cell(formatTime(token.created_at)),
    cell(token.last_used_at ? formatTime(token.last_used_at) : "Never"),
    statusCell(token),
    tokenActionCell(token),
  );
  return row;
}

function renderReservationTokenOptions() {
  const activeTokens = currentTokens.filter((token) => !token.revoked_at);
  elements.reservationToken.replaceChildren();
  if (activeTokens.length === 0) {
    const option = document.createElement("option");
    option.value = "";
    option.textContent = "No active tokens";
    elements.reservationToken.append(option);
    elements.reservationToken.disabled = true;
    return;
  }

  elements.reservationToken.disabled = false;
  for (const token of activeTokens) {
    const option = document.createElement("option");
    option.value = token.id;
    option.textContent = `${token.name} (${token.id})`;
    elements.reservationToken.append(option);
  }
}

async function loadReservations() {
  elements.reservationCount.textContent = "Loading reservations";
  elements.reservationsBody.replaceChildren();
  elements.reservationsEmptyState.hidden = true;

  const payload = await apiRequest("/api/v1/reserved-subdomains");
  currentReservations = payload.reserved_subdomains || [];
  renderReservations(currentReservations);
  renderAccessReservationOptions();
  renderCustomDomainReservationOptions();
}

function renderReservations(reservations) {
  elements.reservationsBody.replaceChildren(...reservations.map(renderReservationRow));
  elements.reservationsEmptyState.hidden = reservations.length !== 0;
  elements.reservationCount.textContent = `${reservations.length} reservation${reservations.length === 1 ? "" : "s"}`;
}

function renderReservationRow(reservation) {
  const row = document.createElement("tr");
  row.append(
    cell(reservation.name),
    monoCell(reservation.id),
    monoCell(reservation.token_id),
    cell(formatTime(reservation.created_at)),
    reservationActionCell(reservation),
  );
  return row;
}

function renderCustomDomainReservationOptions() {
  elements.customDomainReservation.replaceChildren();
  if (currentReservations.length === 0) {
    const option = document.createElement("option");
    option.value = "";
    option.textContent = "No reserved subdomains";
    elements.customDomainReservation.append(option);
    elements.customDomainReservation.disabled = true;
    return;
  }

  elements.customDomainReservation.disabled = false;
  for (const reservation of currentReservations) {
    const option = document.createElement("option");
    option.value = reservation.id;
    option.textContent = `${reservation.name} (${reservation.id})`;
    elements.customDomainReservation.append(option);
  }
}

async function loadCustomDomains() {
  elements.customDomainCount.textContent = "Loading custom domains";
  elements.customDomainsBody.replaceChildren();
  elements.customDomainsEmptyState.hidden = true;

  const payload = await apiRequest("/api/v1/custom-domains");
  currentCustomDomains = payload.custom_domains || [];
  renderCustomDomains(currentCustomDomains);
}

function renderCustomDomains(domains) {
  elements.customDomainsBody.replaceChildren(...domains.map(renderCustomDomainRow));
  elements.customDomainsEmptyState.hidden = domains.length !== 0;
  elements.customDomainCount.textContent = `${domains.length} custom domain${domains.length === 1 ? "" : "s"}`;
}

function renderCustomDomainRow(domain) {
  const row = document.createElement("tr");
  const reservation = reservationByID(domain.reserved_subdomain_id);
  row.append(
    cell(domain.hostname),
    monoCell(domain.id),
    cell(reservation ? reservation.name : domain.reserved_subdomain_id),
    customDomainStatusCell(domain),
    customDomainVerificationCell(domain),
    cell(formatTime(domain.updated_at)),
    customDomainActionCell(domain),
  );
  return row;
}

function customDomainStatusCell(domain) {
  const item = document.createElement("td");
  const badge = document.createElement("span");
  badge.className = domain.status === "active" ? "badge active" : domain.status === "verification_failed" ? "badge revoked" : "badge";
  badge.textContent = domain.status || "unknown";
  item.append(badge);
  return item;
}

function customDomainVerificationCell(domain) {
  const item = document.createElement("td");
  item.classList.add("verification-cell");
  if (domain.verified_at) {
    item.textContent = formatTime(domain.verified_at);
    return item;
  }
  if (!domain.verification_name && !domain.verification_token) {
    item.textContent = "-";
    return item;
  }

  if (domain.verification_name) {
    const name = document.createElement("code");
    name.textContent = domain.verification_name;
    item.append(name);
  }
  if (domain.verification_token) {
    const value = document.createElement("code");
    value.textContent = `porthook-domain-verification=${domain.verification_token}`;
    item.append(value);
  }
  return item;
}

function customDomainActionCell(domain) {
  const item = document.createElement("td");
  item.classList.add("right", "button-cell");
  if (domain.status !== "active") {
    const verify = document.createElement("button");
    verify.className = "secondary";
    verify.type = "button";
    verify.textContent = "Verify";
    verify.addEventListener("click", () => verifyCustomDomain(domain));
    item.append(verify);
  }
  const remove = document.createElement("button");
  remove.className = "danger";
  remove.type = "button";
  remove.textContent = "Delete";
  remove.addEventListener("click", () => deleteCustomDomain(domain));
  item.append(remove);
  return item;
}

function renderAccessReservationOptions() {
  elements.accessReservation.replaceChildren();
  if (currentReservations.length === 0) {
    const option = document.createElement("option");
    option.value = "";
    option.textContent = "No reserved subdomains";
    elements.accessReservation.append(option);
    elements.accessReservation.disabled = true;
    elements.accessPolicySubmit.disabled = true;
    return;
  }

  elements.accessReservation.disabled = Boolean(editingAccessPolicyID);
  elements.accessPolicySubmit.disabled = false;
  for (const reservation of currentReservations) {
    const option = document.createElement("option");
    option.value = reservation.id;
    option.textContent = `${reservation.name} (${reservation.id})`;
    elements.accessReservation.append(option);
  }
}

async function loadAccessPolicies() {
  elements.accessPolicyCount.textContent = "Loading access policies";
  elements.accessPoliciesBody.replaceChildren();
  elements.accessPoliciesEmptyState.hidden = true;

  const payload = await apiRequest("/api/v1/access-policies");
  currentAccessPolicies = payload.access_policies || [];
  renderAccessPolicies(currentAccessPolicies);
}

function renderAccessPolicies(policies) {
  elements.accessPoliciesBody.replaceChildren(...policies.map(renderAccessPolicyRow));
  elements.accessPoliciesEmptyState.hidden = policies.length !== 0;
  elements.accessPolicyCount.textContent = `${policies.length} access polic${policies.length === 1 ? "y" : "ies"}`;
}

function renderAccessPolicyRow(policy) {
  const row = document.createElement("tr");
  const reservation = reservationByID(policy.reserved_subdomain_id);
  row.append(
    cell(reservation ? reservation.name : policy.reserved_subdomain_id),
    monoCell(policy.id),
    cell(policy.mode),
    cell(accessPolicySettings(policy)),
    cell(formatTime(policy.updated_at)),
    accessPolicyActionCell(policy),
  );
  return row;
}

function accessPolicySettings(policy) {
  switch (policy.mode) {
    case "basic_auth":
      return `basic username ${policy.basic_username || "-"}`;
    case "bearer_token":
      return policy.secret_configured ? "bearer token configured" : "bearer token missing";
    case "ip_allowlist":
      return (policy.ip_allowlist || []).join(", ") || "-";
    default:
      return "public";
  }
}

function accessPolicyActionCell(policy) {
  const item = document.createElement("td");
  item.classList.add("right", "button-cell");
  const edit = document.createElement("button");
  edit.className = "secondary";
  edit.type = "button";
  edit.textContent = "Edit";
  edit.addEventListener("click", () => editAccessPolicy(policy));
  const remove = document.createElement("button");
  remove.className = "danger";
  remove.type = "button";
  remove.textContent = "Delete";
  remove.addEventListener("click", () => deleteAccessPolicy(policy));
  item.append(edit, remove);
  return item;
}

async function loadAuditEvents({ append = false } = {}) {
  const cursor = append ? auditEventNextCursor : "";
  if (append && !cursor) {
    return;
  }
  const query = auditEventQuery(cursor);

  if (append) {
    setAuditEventLoadMoreLoading(true);
  } else {
    currentAuditEvents = [];
    auditEventNextCursor = "";
    elements.auditEventCount.textContent = "Loading audit events";
    elements.auditEventsBody.replaceChildren();
    elements.auditEventsEmptyState.hidden = true;
    updateAuditEventLoadMore();
  }

  try {
    const payload = await apiRequest(`/api/v1/events?${query}`);
    const events = payload.events || [];
    currentAuditEvents = append ? currentAuditEvents.concat(events) : events;
    auditEventNextCursor = payload.next_cursor || "";
    renderAuditEvents(currentAuditEvents);
    updateAuditEventLoadMore();
  } catch (error) {
    if (append) {
      auditEventNextCursor = "";
      renderAuditEvents(currentAuditEvents);
      updateAuditEventLoadMore();
    }
    throw error;
  } finally {
    if (append) {
      setAuditEventLoadMoreLoading(false);
    }
  }
}

function renderAuditEvents(events) {
  const filtered = filterAuditEvents(events);
  elements.auditEventsBody.replaceChildren(...filtered.map(renderAuditEventRow));
  elements.auditEventsEmptyState.hidden = filtered.length !== 0;
  const loaded = events.length === filtered.length ? `${events.length}` : `${filtered.length} of ${events.length}`;
  const suffix = auditEventNextCursor ? ", more available" : "";
  elements.auditEventCount.textContent = `${loaded} audit event${filtered.length === 1 ? "" : "s"}${suffix}`;
}

function updateAuditEventLoadMore() {
  elements.auditEventLoadMore.hidden = !auditEventNextCursor;
  elements.auditEventLoadMore.disabled = false;
  elements.auditEventLoadMore.textContent = "Load more";
}

function setAuditEventLoadMoreLoading(loading) {
  elements.auditEventLoadMore.disabled = loading;
  elements.auditEventLoadMore.textContent = loading ? "Loading" : "Load more";
}

function resetAuditEventPagination() {
  auditEventNextCursor = "";
  updateAuditEventLoadMore();
}

function renderAuditEventRow(event) {
  const row = document.createElement("tr");
  row.append(
    cell(formatTime(event.time)),
    cell(event.level || "-"),
    monoCell(event.event || "-"),
    cell(event.message || "-"),
    cell(event.method || "-"),
    cell(event.path || "-"),
    copyableMonoCell(event.request_id || "-"),
    cell(event.remote_ip || "-"),
    monoCell(auditEventFields(event)),
  );
  return row;
}

function filterAuditEvents(events) {
  const eventName = elements.auditEventEvent.value.trim().toLowerCase();
  const level = elements.auditEventLevel.value.trim().toUpperCase();
  const requestID = elements.auditEventRequestID.value.trim().toLowerCase();
  const remoteIP = elements.auditEventRemoteIP.value.trim().toLowerCase();
  const field = elements.auditEventField.value.trim().toLowerCase();
  return events.filter((event) => {
    if (eventName && !String(event.event || "").toLowerCase().includes(eventName)) {
      return false;
    }
    if (level && String(event.level || "").toUpperCase() !== level) {
      return false;
    }
    if (requestID && !String(event.request_id || "").toLowerCase().includes(requestID)) {
      return false;
    }
    if (remoteIP && !String(event.remote_ip || "").toLowerCase().includes(remoteIP)) {
      return false;
    }
    if (field && !auditEventFields(event).toLowerCase().includes(field)) {
      return false;
    }
    return true;
  });
}

function auditEventFields(event) {
  const fields = event.fields || {};
  const entries = Object.entries(fields).sort(([left], [right]) => left.localeCompare(right));
  return entries.map(([key, value]) => `${key}=${value}`).join(", ") || "-";
}

async function runDiagnostics() {
  elements.diagnosticsCount.textContent = "Running diagnostics";
  elements.diagnosticsBody.replaceChildren();
  elements.diagnosticsEmptyState.hidden = true;

  const checks = [
    {
      name: "Control-plane status",
      target: "/api/v1/status",
      run: checkControlPlaneStatus,
    },
    {
      name: "Control-plane readiness",
      target: "/readyz",
      run: checkControlPlaneReadiness,
    },
    {
      name: "Audit events API",
      target: "/api/v1/events",
      run: checkAuditEventsAPI,
    },
    {
      name: "Gateway tunnel API",
      target: "gateway /api/v1/tunnels",
      run: checkGatewayTunnelAPI,
    },
    {
      name: "Gateway runtime API",
      target: "gateway /api/v1/runtime",
      run: checkGatewayRuntimeAPI,
    },
    {
      name: "Gateway metrics API",
      target: "gateway /metrics",
      run: checkGatewayMetricsAPI,
    },
    {
      name: "Gateway request logs API",
      target: "gateway /api/v1/request-logs",
      run: checkGatewayRequestLogsAPI,
    },
  ];

  currentDiagnostics = await Promise.all(checks.map(runDiagnosticCheck));
  renderDiagnostics(currentDiagnostics);
}

async function runDiagnosticCheck(check) {
  const started = performance.now();
  try {
    const detail = await check.run();
    return {
      name: check.name,
      target: check.target,
      status: "pass",
      latency_ms: Math.round(performance.now() - started),
      detail,
    };
  } catch (error) {
    return {
      name: check.name,
      target: check.target,
      status: "fail",
      latency_ms: Math.round(performance.now() - started),
      detail: error.message,
    };
  }
}

function renderDiagnostics(checks) {
  elements.diagnosticsBody.replaceChildren(...checks.map(renderDiagnosticRow));
  elements.diagnosticsEmptyState.hidden = checks.length !== 0;
  const failed = checks.filter((check) => check.status === "fail").length;
  elements.diagnosticsCount.textContent = failed === 0 ? `${checks.length} checks passed` : `${failed} of ${checks.length} checks failed`;
}

function renderDiagnosticRow(check) {
  const row = document.createElement("tr");
  row.append(
    cell(check.name),
    cell(check.target),
    diagnosticStatusCell(check.status),
    cell(`${check.latency_ms || 0} ms`),
    cell(check.detail || "-"),
  );
  return row;
}

function diagnosticStatusCell(status) {
  const item = document.createElement("td");
  const badge = document.createElement("span");
  badge.className = status === "pass" ? "badge active" : "badge revoked";
  badge.textContent = status;
  item.append(badge);
  return item;
}

async function checkControlPlaneStatus() {
  const response = await fetch("/api/v1/status", { cache: "no-store" });
  const payload = await readPayload(response);
  if (!response.ok) {
    throw new Error(responseDetail(payload, response.status));
  }
  if (!payload || !payload.ready) {
    throw new Error("control plane is not ready");
  }
  return `ready, version ${payload.version || "unknown"}`;
}

async function checkControlPlaneReadiness() {
  const response = await fetch("/readyz", { cache: "no-store" });
  const payload = await readPayload(response);
  if (!response.ok) {
    throw new Error(responseDetail(payload, response.status));
  }
  return typeof payload === "string" && payload ? payload : "ready";
}

async function checkAuditEventsAPI() {
  const payload = await apiRequest("/api/v1/events?limit=1");
  const count = (payload.events || []).length;
  return `${count} recent event${count === 1 ? "" : "s"}`;
}

async function checkGatewayTunnelAPI() {
  const payload = await gatewayJSON("/api/v1/tunnels");
  const count = (payload.tunnels || []).length;
  return `${count} active tunnel${count === 1 ? "" : "s"}`;
}

async function checkGatewayRuntimeAPI() {
  const payload = await gatewayJSON("/api/v1/runtime");
  const runtime = payload.runtime || {};
  return `uptime ${formatDuration((runtime.uptime_seconds || 0) * 1000)}`;
}

async function checkGatewayMetricsAPI() {
  const metrics = await gatewayText("/metrics");
  const parsed = parsePrometheusMetrics(metrics);
  return `${parsed.length} metric${parsed.length === 1 ? "" : "s"}`;
}

async function checkGatewayRequestLogsAPI() {
  const payload = await gatewayJSON("/api/v1/request-logs?limit=1");
  const count = (payload.request_logs || []).length;
  return `${count} recent request log${count === 1 ? "" : "s"}`;
}

async function gatewayJSON(path) {
  const baseURL = normalizedGatewayURL();
  if (!baseURL) {
    throw new Error("gateway URL is required");
  }
  const response = await fetch(`${baseURL}${path}`, { cache: "no-store" });
  const payload = await readPayload(response);
  if (!response.ok) {
    throw new Error(responseDetail(payload, response.status));
  }
  return payload || {};
}

async function gatewayText(path) {
  const baseURL = normalizedGatewayURL();
  if (!baseURL) {
    throw new Error("gateway URL is required");
  }
  const response = await fetch(`${baseURL}${path}`, { cache: "no-store" });
  const payload = await readPayload(response);
  if (!response.ok) {
    throw new Error(responseDetail(payload, response.status));
  }
  return typeof payload === "string" ? payload : "";
}

function responseDetail(payload, status) {
  return typeof payload === "string" && payload ? payload : `status ${status}`;
}

function reservationByID(id) {
  return currentReservations.find((reservation) => reservation.id === id);
}

async function loadTunnels({ silent = false } = {}) {
  const baseURL = normalizedGatewayURL();
  if (!baseURL) {
    elements.tunnelCount.textContent = "Gateway URL required";
    currentTunnels = [];
    elements.tunnelsBody.replaceChildren();
    elements.tunnelsEmptyState.hidden = false;
    renderOperationalOverview();
    return;
  }

  elements.tunnelCount.textContent = "Loading tunnels";
  elements.tunnelsBody.replaceChildren();
  elements.tunnelsEmptyState.hidden = true;

  try {
    const response = await fetch(`${baseURL}/api/v1/tunnels`, { cache: "no-store" });
    const payload = await readPayload(response);
    if (!response.ok) {
      throw new Error(typeof payload === "string" && payload ? payload : `Gateway returned status ${response.status}.`);
    }
    currentTunnels = payload.tunnels || [];
    renderTunnels(currentTunnels);
    if (selectedTunnelID && !currentTunnels.some((tunnel) => tunnel.tunnel_id === selectedTunnelID)) {
      clearTunnelDetail();
    }
    renderOperationalOverview();
  } catch (error) {
    currentTunnels = [];
    elements.tunnelCount.textContent = "Gateway unavailable";
    elements.tunnelsBody.replaceChildren();
    elements.tunnelsEmptyState.hidden = false;
    renderOperationalOverview();
    if (!silent) {
      showNotice(error.message, "error");
    }
  }
}

async function loadGatewayRuntime({ silent = false } = {}) {
  const baseURL = normalizedGatewayURL();
  if (!baseURL) {
    currentGatewayRuntime = null;
    elements.gatewayRuntimeCount.textContent = "Gateway URL required";
    elements.gatewayRuntimeGrid.replaceChildren();
    elements.gatewayRuntimeEmptyState.hidden = false;
    return;
  }

  elements.gatewayRuntimeCount.textContent = "Loading gateway runtime";
  elements.gatewayRuntimeGrid.replaceChildren();
  elements.gatewayRuntimeEmptyState.hidden = true;

  try {
    const response = await fetch(`${baseURL}/api/v1/runtime`, { cache: "no-store" });
    const payload = await readPayload(response);
    if (!response.ok) {
      throw new Error(typeof payload === "string" && payload ? payload : `Gateway returned status ${response.status}.`);
    }
    currentGatewayRuntime = payload.runtime || null;
    renderGatewayRuntime(currentGatewayRuntime);
  } catch (error) {
    currentGatewayRuntime = null;
    elements.gatewayRuntimeCount.textContent = "Gateway runtime unavailable";
    elements.gatewayRuntimeGrid.replaceChildren();
    elements.gatewayRuntimeEmptyState.hidden = false;
    if (!silent) {
      showNotice(error.message, "error");
    }
  }
}

function renderGatewayRuntime(runtime) {
  if (!runtime) {
    elements.gatewayRuntimeGrid.replaceChildren();
    elements.gatewayRuntimeEmptyState.hidden = false;
    elements.gatewayRuntimeCount.textContent = "No gateway runtime loaded";
    return;
  }
  const counters = runtime.counters || {};
  const limits = runtime.limits || {};
  const timeouts = runtime.timeouts || {};
  elements.gatewayRuntimeEmptyState.hidden = true;
  elements.gatewayRuntimeCount.textContent = `Uptime ${formatDuration((runtime.uptime_seconds || 0) * 1000)}`;
  elements.gatewayRuntimeGrid.replaceChildren(
    detailItem("Started", formatTime(runtime.started_at)),
    detailItem("Public URL", runtime.public_url || "-"),
    detailItem("Root domain", runtime.root_domain || "-"),
    detailItem("Control plane", runtime.control_plane_configured ? "configured" : "standalone"),
    detailItem("Active tunnels", String(runtime.active_tunnels || 0)),
    detailItem("Streams", `${runtime.active_streams || 0}/${runtime.stream_capacity || 0}`),
    detailItem("Request logs", `${runtime.request_log_entries || 0}/${runtime.request_log_capacity || 0}`),
    detailItem("Public requests", String(counters.public_requests_total || 0)),
    detailItem("Request errors", String(counters.public_request_errors_total || 0)),
    detailItem("Rate limited", String(counters.public_request_rate_limited_total || 0)),
    detailItem("Timeouts", String(counters.public_request_timeouts_total || 0)),
    detailItem("No session", String(counters.public_request_no_active_session_total || 0)),
    detailItem("Max body", `${limits.max_body_bytes || 0} bytes`),
    detailItem("Rate limit", `${limits.rate_limit_rps || 0}/${limits.rate_limit_burst || 0}`),
    detailItem("Stream timeout", `${timeouts.stream_timeout_seconds || 0} s`),
    detailItem("WS keepalive", `${timeouts.websocket_ping_interval_seconds || 0}/${timeouts.websocket_pong_timeout_seconds || 0} s`),
  );
}

async function loadGatewayMetrics({ silent = false } = {}) {
  const baseURL = normalizedGatewayURL();
  if (!baseURL) {
    currentMetrics = [];
    elements.metricsCount.textContent = "Gateway URL required";
    elements.metricsBody.replaceChildren();
    elements.metricsEmptyState.hidden = false;
    return;
  }

  elements.metricsCount.textContent = "Loading metrics";
  elements.metricsBody.replaceChildren();
  elements.metricsEmptyState.hidden = true;

  try {
    const response = await fetch(`${baseURL}/metrics`, { cache: "no-store" });
    const payload = await readPayload(response);
    if (!response.ok) {
      throw new Error(typeof payload === "string" && payload ? payload : `Gateway returned status ${response.status}.`);
    }
    currentMetrics = parsePrometheusMetrics(typeof payload === "string" ? payload : "");
    renderMetrics(currentMetrics);
  } catch (error) {
    currentMetrics = [];
    elements.metricsCount.textContent = "Metrics unavailable";
    elements.metricsBody.replaceChildren();
    elements.metricsEmptyState.hidden = false;
    if (!silent) {
      showNotice(error.message, "error");
    }
  }
}

function renderMetrics(metrics) {
  elements.metricsBody.replaceChildren(...metrics.map(renderMetricRow));
  elements.metricsEmptyState.hidden = metrics.length !== 0;
  elements.metricsCount.textContent = `${metrics.length} metric${metrics.length === 1 ? "" : "s"}`;
}

function renderMetricRow(metric) {
  const row = document.createElement("tr");
  row.append(
    monoCell(metric.name),
    cell(metric.type || "-"),
    monoCell(metric.value),
    cell(metric.help || "-"),
  );
  return row;
}

function parsePrometheusMetrics(text) {
  const metadata = new Map();
  const metrics = [];
  for (const line of text.split("\n")) {
    const trimmed = line.trim();
    if (!trimmed) {
      continue;
    }
    if (trimmed.startsWith("# HELP ")) {
      const rest = trimmed.slice("# HELP ".length);
      const firstSpace = rest.indexOf(" ");
      if (firstSpace > 0) {
        const name = rest.slice(0, firstSpace);
        const meta = metadata.get(name) || {};
        meta.help = rest.slice(firstSpace + 1);
        metadata.set(name, meta);
      }
      continue;
    }
    if (trimmed.startsWith("# TYPE ")) {
      const rest = trimmed.slice("# TYPE ".length);
      const [name, metricType] = rest.split(/\s+/, 2);
      if (name) {
        const meta = metadata.get(name) || {};
        meta.type = metricType || "";
        metadata.set(name, meta);
      }
      continue;
    }
    if (trimmed.startsWith("#")) {
      continue;
    }
    const [sample, value] = trimmed.split(/\s+/, 2);
    if (!sample || value === undefined) {
      continue;
    }
    const name = sample.split("{", 1)[0];
    const meta = metadata.get(name) || {};
    metrics.push({
      name: sample,
      type: meta.type || "",
      value,
      help: meta.help || "",
    });
  }
  return metrics;
}

function renderTunnels(tunnels) {
  elements.tunnelsBody.replaceChildren(...tunnels.map(renderTunnelRow));
  elements.tunnelsEmptyState.hidden = tunnels.length !== 0;
  elements.tunnelCount.textContent = `${tunnels.length} active tunnel${tunnels.length === 1 ? "" : "s"}`;
}

function renderTunnelRow(tunnel) {
  const row = document.createElement("tr");
  if (tunnel.tunnel_id === selectedTunnelID) {
    row.classList.add("selected");
  }
  row.append(
    cell(tunnel.subdomain),
    monoCell(tunnel.tunnel_id),
    linkCell(tunnel.public_url),
    cell(tunnel.protocol || "http"),
    cell(formatTime(tunnel.connected_at)),
    tunnelActionCell(tunnel),
  );
  return row;
}

function tunnelActionCell(tunnel) {
  const item = document.createElement("td");
  item.classList.add("right");
  const button = document.createElement("button");
  button.className = "secondary";
  button.type = "button";
  button.textContent = "Details";
  button.addEventListener("click", () => loadTunnelDetail(tunnel));
  item.append(button);
  return item;
}

async function loadTunnelDetail(tunnel) {
  const baseURL = normalizedGatewayURL();
  if (!baseURL) {
    showNotice("Gateway URL is required.", "error");
    return;
  }
  selectedTunnelID = tunnel.tunnel_id;
  elements.tunnelDetailTitle.textContent = tunnel.subdomain || tunnel.tunnel_id;
  elements.tunnelDetailMeta.textContent = "Loading tunnel detail";
  elements.tunnelDetailGrid.replaceChildren();
  elements.tunnelDetailPanel.hidden = false;
  renderTunnels(currentTunnels);

  try {
    const response = await fetch(`${baseURL}/api/v1/tunnels/${encodeURIComponent(tunnel.tunnel_id)}`, { cache: "no-store" });
    const payload = await readPayload(response);
    if (!response.ok) {
      throw new Error(typeof payload === "string" && payload ? payload : `Gateway returned status ${response.status}.`);
    }
    renderTunnelDetail(payload.tunnel || {});
  } catch (error) {
    elements.tunnelDetailMeta.textContent = "Tunnel detail unavailable";
    elements.tunnelDetailGrid.replaceChildren(detailItem("Error", error.message));
    showNotice(error.message, "error");
  }
}

function renderTunnelDetail(tunnel) {
  const requests = tunnel.recent_requests || {};
  elements.tunnelDetailTitle.textContent = tunnel.subdomain || tunnel.tunnel_id || "Tunnel details";
  elements.tunnelDetailMeta.textContent = `${tunnel.tunnel_id || "-"} connected ${formatDuration((tunnel.connected_seconds || 0) * 1000)}`;
  elements.tunnelDetailGrid.replaceChildren(
    detailItem("Public URL", tunnel.public_url || "-"),
    detailItem("Tunnel ID", tunnel.tunnel_id || "-", true, true),
    detailItem("Protocol", tunnel.protocol || "http"),
    detailItem("Agent version", tunnel.agent_version || "-"),
    detailItem("Protocol version", tunnel.protocol_version || "-"),
    detailItem("Connected", formatTime(tunnel.connected_at)),
    detailItem("Streams", `${tunnel.active_streams || 0}/${tunnel.stream_capacity || 0}`),
    detailItem("Recent requests", String(requests.count || 0)),
    detailItem("Recent errors", String(requests.error_count || 0)),
    detailItem("Last status", requests.last_status ? String(requests.last_status) : "-"),
    detailItem("Last outcome", requests.last_outcome || "-"),
    detailItem("Last request ID", requests.last_request_id || "-", true, true),
    detailItem("Custom domains", (requests.custom_domains || []).join(", ") || "-"),
  );
}

function clearTunnelDetail() {
  selectedTunnelID = "";
  elements.tunnelDetailPanel.hidden = true;
  elements.tunnelDetailTitle.textContent = "Tunnel details";
  elements.tunnelDetailMeta.textContent = "Select an active tunnel.";
  elements.tunnelDetailGrid.replaceChildren();
  if (currentTunnels.length > 0) {
    renderTunnels(currentTunnels);
  }
}

async function loadRequestLogs({ silent = false, append = false } = {}) {
  const baseURL = normalizedGatewayURL();
  if (!baseURL) {
    currentRequestLogs = [];
    requestLogNextCursor = "";
    renderRequestLogs([]);
    elements.requestLogCount.textContent = "Gateway URL required";
    updateRequestLogLoadMore();
    renderOperationalOverview();
    return;
  }

  let query;
  try {
    const cursor = append ? requestLogNextCursor : "";
    if (append && !cursor) {
      return;
    }
    query = requestLogQuery(cursor);
  } catch (error) {
    elements.requestLogCount.textContent = "Invalid request log filter";
    if (!silent) {
      showNotice(error.message, "error");
    }
    return;
  }
  if (append) {
    setRequestLogLoadMoreLoading(true);
  } else {
    currentRequestLogs = [];
    requestLogNextCursor = "";
    elements.requestLogCount.textContent = "Loading request logs";
    elements.requestLogsBody.replaceChildren();
    elements.requestLogsEmptyState.hidden = true;
    updateRequestLogLoadMore();
  }

  try {
    const response = await fetch(`${baseURL}/api/v1/request-logs?${query}`, { cache: "no-store" });
    const payload = await readPayload(response);
    if (!response.ok) {
      throw new Error(typeof payload === "string" && payload ? payload : `Gateway returned status ${response.status}.`);
    }
    const logs = payload.request_logs || [];
    currentRequestLogs = append ? currentRequestLogs.concat(logs) : logs;
    requestLogNextCursor = payload.next_cursor || "";
    renderRequestLogs(currentRequestLogs);
    updateRequestLogLoadMore();
    renderOperationalOverview();
  } catch (error) {
    requestLogNextCursor = "";
    if (append) {
      renderRequestLogs(currentRequestLogs);
    } else {
      currentRequestLogs = [];
      elements.requestLogCount.textContent = "Request logs unavailable";
      elements.requestLogsBody.replaceChildren();
      elements.requestLogsEmptyState.hidden = false;
    }
    updateRequestLogLoadMore();
    renderOperationalOverview();
    if (!silent) {
      showNotice(error.message, "error");
    }
  } finally {
    if (append) {
      setRequestLogLoadMoreLoading(false);
    }
  }
}

function renderRequestLogs(logs) {
  const filtered = filterRequestLogs(logs);
  elements.requestLogsBody.replaceChildren(...filtered.map(renderRequestLogRow));
  elements.requestLogsEmptyState.hidden = filtered.length !== 0;
  const loaded = logs.length === filtered.length ? `${logs.length}` : `${filtered.length} of ${logs.length}`;
  const suffix = requestLogNextCursor ? ", more available" : "";
  elements.requestLogCount.textContent = `${loaded} request log${filtered.length === 1 ? "" : "s"}${suffix}`;
}

function updateRequestLogLoadMore() {
  elements.requestLogLoadMore.hidden = !requestLogNextCursor;
  elements.requestLogLoadMore.disabled = false;
  elements.requestLogLoadMore.textContent = "Load more";
}

function setRequestLogLoadMoreLoading(loading) {
  elements.requestLogLoadMore.disabled = loading;
  elements.requestLogLoadMore.textContent = loading ? "Loading" : "Load more";
}

function resetRequestLogPagination() {
  requestLogNextCursor = "";
  updateRequestLogLoadMore();
}

function renderRequestLogRow(entry) {
  const row = document.createElement("tr");
  row.append(
    cell(formatTime(entry.time)),
    cell(entry.subdomain || "-"),
    cell(entry.host || "-"),
    cell(entry.method || "-"),
    cell(entry.path || "-"),
    cell(entry.status || "-"),
    cell(entry.outcome || "-"),
    copyableMonoCell(entry.request_id || "-"),
    copyableMonoCell(entry.tunnel_id || "-"),
    cell(`${entry.duration_ms || 0} ms`),
    cell(`${entry.request_bytes || 0}/${entry.response_bytes || 0}`),
    cell(entry.remote_ip || "-"),
  );
  return row;
}

function filterRequestLogs(logs) {
  const subdomain = elements.requestLogSubdomain.value.trim().toLowerCase();
  const method = elements.requestLogMethod.value.trim().toUpperCase();
  const host = elements.requestLogHost.value.trim().toLowerCase();
  const path = elements.requestLogPath.value.trim().toLowerCase();
  const status = elements.requestLogStatus.value.trim();
  const outcome = elements.requestLogOutcome.value.trim().toLowerCase();
  const requestID = elements.requestLogRequestID.value.trim().toLowerCase();
  const tunnelID = elements.requestLogTunnelID.value.trim().toLowerCase();
  const since = requestLogFilterTime(elements.requestLogSince.value);
  const until = requestLogFilterTime(elements.requestLogUntil.value);
  return logs.filter((entry) => {
    if (subdomain && String(entry.subdomain || "").toLowerCase() !== subdomain) {
      return false;
    }
    if (method && String(entry.method || "").toUpperCase() !== method) {
      return false;
    }
    if (host && !String(entry.host || "").toLowerCase().includes(host)) {
      return false;
    }
    if (path && !String(entry.path || "").toLowerCase().includes(path)) {
      return false;
    }
    if (status && String(entry.status || "") !== status) {
      return false;
    }
    if (outcome && !String(entry.outcome || "").toLowerCase().includes(outcome)) {
      return false;
    }
    if (requestID && !String(entry.request_id || "").toLowerCase().includes(requestID)) {
      return false;
    }
    if (tunnelID && !String(entry.tunnel_id || "").toLowerCase().includes(tunnelID)) {
      return false;
    }
    const entryTime = Date.parse(entry.time || "");
    if (since && (!Number.isFinite(entryTime) || entryTime < since.getTime())) {
      return false;
    }
    if (until && (!Number.isFinite(entryTime) || entryTime > until.getTime())) {
      return false;
    }
    return true;
  });
}

function requestLogFilterTime(value) {
  const trimmed = value.trim();
  if (!trimmed) {
    return null;
  }
  const date = new Date(trimmed);
  if (Number.isNaN(date.getTime())) {
    return null;
  }
  return date;
}

function renderOperationalOverview() {
  const totalRequests = currentRequestLogs.length;
  const errorRequests = currentRequestLogs.filter((entry) => Number(entry.status || 0) >= 500).length;
  const errorRate = totalRequests === 0 ? 0 : Math.round((errorRequests / totalRequests) * 100);
  const p95Latency = percentile(
    currentRequestLogs
      .map((entry) => Number(entry.duration_ms || 0))
      .filter((duration) => Number.isFinite(duration) && duration >= 0),
    0.95,
  );

  elements.overviewActiveTunnels.textContent = String(currentTunnels.length);
  elements.overviewRecentRequests.textContent = String(totalRequests);
  elements.overviewErrorRate.textContent = `${errorRate}%`;
  elements.overviewP95Latency.textContent = formatDuration(p95Latency);
  elements.overviewCount.textContent = `${currentTunnels.length} active tunnel${currentTunnels.length === 1 ? "" : "s"}, ${totalRequests} recent request${totalRequests === 1 ? "" : "s"}`;

  const outcomes = countBy(currentRequestLogs, (entry) => entry.outcome || "unknown");
  const statusClasses = countStatusClasses(currentRequestLogs);
  elements.overviewOutcomeCount.textContent = `${outcomes.length} outcome${outcomes.length === 1 ? "" : "s"}`;
  elements.overviewStatusCount.textContent = `${statusClasses.length} status class${statusClasses.length === 1 ? "" : "es"}`;
  renderBarChart(elements.overviewOutcomeChart, outcomes);
  renderBarChart(elements.overviewStatusChart, statusClasses);
}

function countBy(items, keyFunc) {
  const counts = new Map();
  for (const item of items) {
    const key = keyFunc(item);
    counts.set(key, (counts.get(key) || 0) + 1);
  }
  return [...counts.entries()]
    .map(([label, count]) => ({ label, count }))
    .sort((a, b) => b.count - a.count || a.label.localeCompare(b.label))
    .slice(0, 6);
}

function countStatusClasses(logs) {
  return countBy(logs, (entry) => {
    const status = Number(entry.status || 0);
    if (status >= 200 && status < 300) {
      return "2xx";
    }
    if (status >= 300 && status < 400) {
      return "3xx";
    }
    if (status >= 400 && status < 500) {
      return "4xx";
    }
    if (status >= 500 && status < 600) {
      return "5xx";
    }
    return "other";
  });
}

function renderBarChart(container, rows) {
  container.replaceChildren();
  if (rows.length === 0) {
    const empty = document.createElement("p");
    empty.className = "muted chart-empty";
    empty.textContent = "No request logs loaded";
    container.append(empty);
    return;
  }

  const max = Math.max(...rows.map((row) => row.count), 1);
  for (const row of rows) {
    const item = document.createElement("div");
    item.className = "bar-row";
    const label = document.createElement("span");
    label.className = "bar-label";
    label.textContent = row.label;
    const track = document.createElement("span");
    track.className = "bar-track";
    const fill = document.createElement("span");
    fill.className = "bar-fill";
    fill.style.width = `${Math.max((row.count / max) * 100, 4)}%`;
    track.append(fill);
    const count = document.createElement("span");
    count.className = "bar-count mono";
    count.textContent = String(row.count);
    item.append(label, track, count);
    container.append(item);
  }
}

function percentile(values, percentileValue) {
  if (values.length === 0) {
    return 0;
  }
  const sorted = [...values].sort((a, b) => a - b);
  const index = Math.min(sorted.length - 1, Math.max(0, Math.ceil(sorted.length * percentileValue) - 1));
  return sorted[index];
}

function formatDuration(milliseconds) {
  if (milliseconds >= 1000) {
    return `${(milliseconds / 1000).toFixed(1)} s`;
  }
  return `${Math.round(milliseconds)} ms`;
}

function normalizedRequestLogLimit() {
  const value = Number.parseInt(elements.requestLogLimit.value, 10);
  if (!Number.isFinite(value) || value <= 0) {
    return 100;
  }
  return Math.min(value, 1000);
}

function normalizedAuditEventLimit() {
  const value = Number.parseInt(elements.auditEventLimit.value, 10);
  if (!Number.isFinite(value) || value <= 0) {
    return 100;
  }
  return Math.min(value, 1000);
}

function auditEventQuery(cursor = "") {
  const query = new URLSearchParams();
  query.set("limit", String(normalizedAuditEventLimit()));
  appendQueryFilter(query, "event", elements.auditEventEvent.value);
  appendQueryFilter(query, "level", elements.auditEventLevel.value);
  appendQueryFilter(query, "request_id", elements.auditEventRequestID.value);
  appendQueryFilter(query, "remote_ip", elements.auditEventRemoteIP.value);
  appendQueryFilter(query, "field", elements.auditEventField.value);
  appendQueryFilter(query, "cursor", cursor);
  return query.toString();
}

function requestLogQuery(cursor = "") {
  const query = new URLSearchParams();
  query.set("limit", String(normalizedRequestLogLimit()));
  appendQueryFilter(query, "subdomain", elements.requestLogSubdomain.value);
  appendQueryFilter(query, "method", elements.requestLogMethod.value);
  appendQueryFilter(query, "host", elements.requestLogHost.value);
  appendQueryFilter(query, "path", elements.requestLogPath.value);
  appendQueryFilter(query, "status", elements.requestLogStatus.value);
  appendQueryFilter(query, "outcome", elements.requestLogOutcome.value);
  appendQueryFilter(query, "request_id", elements.requestLogRequestID.value);
  appendQueryFilter(query, "tunnel_id", elements.requestLogTunnelID.value);
  appendRequestLogTimeFilter(query, "since", elements.requestLogSince.value);
  appendRequestLogTimeFilter(query, "until", elements.requestLogUntil.value);
  appendQueryFilter(query, "cursor", cursor);
  return query.toString();
}

function appendQueryFilter(query, name, value) {
  const trimmed = value.trim();
  if (trimmed) {
    query.set(name, trimmed);
  }
}

function appendRequestLogTimeFilter(query, name, value) {
  const trimmed = value.trim();
  if (!trimmed) {
    return;
  }
  const date = new Date(trimmed);
  if (Number.isNaN(date.getTime())) {
    throw new Error(`${name} filter is not a valid date.`);
  }
  query.set(name, date.toISOString());
}

function cell(text) {
  const item = document.createElement("td");
  item.textContent = text;
  return item;
}

function monoCell(text) {
  const item = cell(text);
  item.classList.add("mono");
  return item;
}

function copyableMonoCell(text) {
  const item = document.createElement("td");
  const value = String(text || "-");
  const wrapper = document.createElement("div");
  wrapper.className = "copyable-value";
  const label = document.createElement("span");
  label.className = "mono";
  label.textContent = value;
  wrapper.append(label);
  if (value !== "-") {
    wrapper.append(copyButton(value));
  }
  item.append(wrapper);
  return item;
}

function linkCell(href) {
  const item = document.createElement("td");
  if (!href) {
    item.textContent = "";
    return item;
  }
  const link = document.createElement("a");
  link.href = href;
  link.textContent = href;
  link.rel = "noreferrer";
  item.append(link);
  return item;
}

function detailItem(label, value, mono = false, copyable = false) {
  const item = document.createElement("div");
  item.className = "detail-item";
  const labelEl = document.createElement("span");
  labelEl.textContent = label;
  const valueEl = document.createElement("strong");
  valueEl.textContent = value;
  if (mono) {
    valueEl.classList.add("mono");
  }
  if (copyable && value && value !== "-") {
    const wrapper = document.createElement("div");
    wrapper.className = "detail-value";
    wrapper.append(valueEl, copyButton(value));
    item.append(labelEl, wrapper);
    return item;
  }
  item.append(labelEl, valueEl);
  return item;
}

function copyButton(value) {
  const button = document.createElement("button");
  button.className = "secondary copy-button";
  button.type = "button";
  button.textContent = "Copy";
  button.addEventListener("click", () => copyText(value));
  return button;
}

function statusCell(token) {
  const item = document.createElement("td");
  const badge = document.createElement("span");
  badge.className = token.revoked_at ? "badge revoked" : "badge active";
  badge.textContent = token.revoked_at ? `Revoked ${formatTime(token.revoked_at)}` : "Active";
  item.append(badge);
  return item;
}

function tokenActionCell(token) {
  const item = document.createElement("td");
  item.classList.add("right");
  const button = document.createElement("button");
  button.className = "danger";
  button.type = "button";
  button.textContent = "Revoke";
  button.disabled = Boolean(token.revoked_at);
  button.addEventListener("click", () => revokeToken(token));
  item.append(button);
  return item;
}

function adminTokenActionCell(token) {
  const item = document.createElement("td");
  item.classList.add("right");
  const button = document.createElement("button");
  button.className = "danger";
  button.type = "button";
  button.textContent = "Revoke";
  button.disabled = Boolean(token.revoked_at);
  button.addEventListener("click", () => revokeAdminToken(token));
  item.append(button);
  return item;
}

function reservationActionCell(reservation) {
  const item = document.createElement("td");
  item.classList.add("right");
  const button = document.createElement("button");
  button.className = "danger";
  button.type = "button";
  button.textContent = "Delete";
  button.addEventListener("click", () => deleteReservation(reservation));
  item.append(button);
  return item;
}

function formatTime(value) {
  if (!value) {
    return "";
  }
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) {
    return value;
  }
  return date.toLocaleString(undefined, {
    year: "numeric",
    month: "short",
    day: "2-digit",
    hour: "2-digit",
    minute: "2-digit",
  });
}

async function createToken(event) {
  event.preventDefault();
  showNotice("");
  clearCreatedToken();

  const name = elements.tokenName.value.trim();
  const scopes = elements.scopeRegisterTunnel.checked ? ["register_tunnel"] : [];
  if (!name) {
    showNotice("Token name is required.", "error");
    return;
  }

  try {
    const created = await apiRequest("/api/v1/tokens", {
      method: "POST",
      body: JSON.stringify({ name, scopes }),
    });
    elements.tokenName.value = "";
    elements.scopeRegisterTunnel.checked = true;
    await loadTokens();
    elements.createdTokenValue.textContent = created.token;
    elements.createdTokenPanel.hidden = false;
    showNotice(`Created token ${created.id}.`, "success");
  } catch (error) {
    showNotice(error.message, "error");
  }
}

async function createAdminToken(event) {
  event.preventDefault();
  showNotice("");
  clearCreatedAdminToken();

  const name = elements.adminTokenName.value.trim();
  const scopes = selectedAdminTokenScopes();
  if (!name) {
    showNotice("Admin token name is required.", "error");
    return;
  }
  if (scopes.length === 0) {
    showNotice("Select at least one admin token scope.", "error");
    return;
  }

  try {
    const created = await apiRequest("/api/v1/admin-tokens", {
      method: "POST",
      body: JSON.stringify({ name, scopes }),
    });
    elements.adminTokenName.value = "";
    resetAdminTokenScopes();
    await loadAdminTokens();
    elements.createdAdminTokenValue.textContent = created.token;
    elements.createdAdminTokenPanel.hidden = false;
    showNotice(`Created admin token ${created.id}.`, "success");
  } catch (error) {
    showNotice(error.message, "error");
  }
}

async function revokeToken(token) {
  if (!window.confirm(`Revoke token ${token.id}?`)) {
    return;
  }
  try {
    await apiRequest(`/api/v1/tokens/${encodeURIComponent(token.id)}`, { method: "DELETE" });
    await Promise.all([loadTokens(), loadReservations()]);
    showNotice(`Revoked token ${token.id}.`, "success");
  } catch (error) {
    showNotice(error.message, "error");
  }
}

async function revokeAdminToken(token) {
  if (!window.confirm(`Revoke admin token ${token.id}?`)) {
    return;
  }
  try {
    await apiRequest(`/api/v1/admin-tokens/${encodeURIComponent(token.id)}`, { method: "DELETE" });
    await loadAdminTokens();
    showNotice(`Revoked admin token ${token.id}.`, "success");
  } catch (error) {
    showNotice(error.message, "error");
  }
}

function selectedAdminTokenScopes() {
  return elements.adminTokenScopeInputs.filter((input) => input.checked).map((input) => input.value);
}

function resetAdminTokenScopes() {
  for (const input of elements.adminTokenScopeInputs) {
    input.checked = true;
  }
}

async function createReservation(event) {
  event.preventDefault();
  showNotice("");

  const name = elements.reservationName.value.trim();
  const tokenID = elements.reservationToken.value.trim();
  if (!name) {
    showNotice("Reserved subdomain name is required.", "error");
    return;
  }
  if (!tokenID) {
    showNotice("Owner token is required.", "error");
    return;
  }

  try {
    const created = await apiRequest("/api/v1/reserved-subdomains", {
      method: "POST",
      body: JSON.stringify({ name, token_id: tokenID }),
    });
    elements.reservationName.value = "";
    await loadReservations();
    showNotice(`Reserved subdomain ${created.name}.`, "success");
  } catch (error) {
    showNotice(error.message, "error");
  }
}

async function deleteReservation(reservation) {
  if (!window.confirm(`Delete reserved subdomain ${reservation.name}?`)) {
    return;
  }
  try {
    await apiRequest(`/api/v1/reserved-subdomains/${encodeURIComponent(reservation.id)}`, { method: "DELETE" });
    await loadReservations();
    await loadCustomDomains();
    await loadAccessPolicies();
    showNotice(`Deleted reserved subdomain ${reservation.name}.`, "success");
  } catch (error) {
    showNotice(error.message, "error");
  }
}

async function createCustomDomain(event) {
  event.preventDefault();
  showNotice("");

  const hostname = elements.customDomainHostname.value.trim();
  const reservedSubdomainID = elements.customDomainReservation.value.trim();
  if (!hostname) {
    showNotice("Custom domain hostname is required.", "error");
    return;
  }
  if (!reservedSubdomainID) {
    showNotice("Reserved subdomain is required.", "error");
    return;
  }

  try {
    const created = await apiRequest("/api/v1/custom-domains", {
      method: "POST",
      body: JSON.stringify({ hostname, reserved_subdomain_id: reservedSubdomainID }),
    });
    elements.customDomainHostname.value = "";
    await loadCustomDomains();
    showNotice(`Added custom domain ${created.hostname}.`, "success");
  } catch (error) {
    showNotice(error.message, "error");
  }
}

async function deleteCustomDomain(domain) {
  if (!window.confirm(`Delete custom domain ${domain.hostname}?`)) {
    return;
  }
  try {
    await apiRequest(`/api/v1/custom-domains/${encodeURIComponent(domain.id)}`, { method: "DELETE" });
    await loadCustomDomains();
    showNotice(`Deleted custom domain ${domain.hostname}.`, "success");
  } catch (error) {
    showNotice(error.message, "error");
  }
}

async function verifyCustomDomain(domain) {
  try {
    const verified = await apiRequest(`/api/v1/custom-domains/${encodeURIComponent(domain.id)}/verify`, { method: "POST" });
    await loadCustomDomains();
    if (verified.status === "active") {
      showNotice(`Verified custom domain ${verified.hostname}.`, "success");
      return;
    }
    showNotice(`Custom domain ${verified.hostname} is still ${verified.status}.`, "error");
  } catch (error) {
    showNotice(error.message, "error");
  }
}

function updateAccessPolicyFields() {
  const mode = elements.accessMode.value;
  elements.basicUsernameField.hidden = mode !== "basic_auth";
  elements.policySecretField.hidden = mode !== "basic_auth" && mode !== "bearer_token";
  elements.ipAllowlistField.hidden = mode !== "ip_allowlist";
}

function resetAccessPolicyForm() {
  editingAccessPolicyID = "";
  elements.accessPolicyForm.reset();
  elements.accessMode.value = "public";
  elements.accessPolicySubmit.textContent = "Create policy";
  elements.accessPolicyCancel.hidden = true;
  elements.accessReservation.disabled = currentReservations.length === 0;
  updateAccessPolicyFields();
}

function editAccessPolicy(policy) {
  editingAccessPolicyID = policy.id;
  elements.accessReservation.value = policy.reserved_subdomain_id;
  elements.accessReservation.disabled = true;
  elements.accessMode.value = policy.mode || "public";
  elements.basicUsername.value = policy.basic_username || "";
  elements.policySecret.value = "";
  elements.ipAllowlist.value = (policy.ip_allowlist || []).join(", ");
  elements.accessPolicySubmit.textContent = "Update policy";
  elements.accessPolicyCancel.hidden = false;
  updateAccessPolicyFields();
}

async function saveAccessPolicy(event) {
  event.preventDefault();
  showNotice("");

  const mode = elements.accessMode.value;
  const payload = { mode };
  if (!editingAccessPolicyID) {
    payload.reserved_subdomain_id = elements.accessReservation.value.trim();
    if (!payload.reserved_subdomain_id) {
      showNotice("Reserved subdomain is required.", "error");
      return;
    }
  }
  if (mode === "basic_auth") {
    payload.basic_username = elements.basicUsername.value.trim();
    if (!payload.basic_username) {
      showNotice("Basic username is required.", "error");
      return;
    }
    const secret = elements.policySecret.value.trim();
    if (secret) {
      payload.basic_password = secret;
    }
  }
  if (mode === "bearer_token") {
    const secret = elements.policySecret.value.trim();
    if (secret) {
      payload.bearer_token = secret;
    }
  }
  if (mode === "ip_allowlist") {
    payload.ip_allowlist = parseListInput(elements.ipAllowlist.value);
    if (payload.ip_allowlist.length === 0) {
      showNotice("IP allowlist is required.", "error");
      return;
    }
  }

  try {
    const updating = Boolean(editingAccessPolicyID);
    const path = editingAccessPolicyID
      ? `/api/v1/access-policies/${encodeURIComponent(editingAccessPolicyID)}`
      : "/api/v1/access-policies";
    const method = editingAccessPolicyID ? "PUT" : "POST";
    const saved = await apiRequest(path, {
      method,
      body: JSON.stringify(payload),
    });
    resetAccessPolicyForm();
    await loadAccessPolicies();
    showNotice(`${updating ? "Updated" : "Created"} access policy ${saved.id}.`, "success");
  } catch (error) {
    showNotice(error.message, "error");
  }
}

async function deleteAccessPolicy(policy) {
  if (!window.confirm(`Delete access policy ${policy.id}?`)) {
    return;
  }
  try {
    await apiRequest(`/api/v1/access-policies/${encodeURIComponent(policy.id)}`, { method: "DELETE" });
    if (editingAccessPolicyID === policy.id) {
      resetAccessPolicyForm();
    }
    await loadAccessPolicies();
    showNotice(`Deleted access policy ${policy.id}.`, "success");
  } catch (error) {
    showNotice(error.message, "error");
  }
}

function parseListInput(value) {
  return value
    .split(/[\s,]+/)
    .map((item) => item.trim())
    .filter(Boolean);
}

async function copyCreatedToken() {
  const value = elements.createdTokenValue.textContent;
  if (!value) {
    return;
  }
  await copyText(value, "Copied plaintext token.");
}

async function copyCreatedAdminToken() {
  const value = elements.createdAdminTokenValue.textContent;
  if (!value) {
    return;
  }
  await copyText(value, "Copied plaintext admin token.");
}

async function copyText(value, successMessage = "Copied value.") {
  try {
    await navigator.clipboard.writeText(value);
    showNotice(successMessage, "success");
  } catch {
    showNotice("Copy failed. Select the value manually.", "error");
  }
}

function normalizedGatewayURL() {
  return elements.gatewayURL.value.trim().replace(/\/+$/, "");
}

function defaultGatewayURL() {
  const { protocol, hostname } = window.location;
  if (!protocol || !hostname) {
    return "http://127.0.0.1:8080";
  }
  return `${protocol}//${hostname}:8080`;
}

async function downloadOperationalExport() {
  elements.exportButton.disabled = true;
  elements.exportCount.textContent = "Collecting export";
  try {
    const snapshot = await buildOperationalExport();
    const data = `${JSON.stringify(snapshot, null, 2)}\n`;
    const blob = new Blob([data], { type: "application/json" });
    const objectURL = URL.createObjectURL(blob);
    const link = document.createElement("a");
    link.href = objectURL;
    link.download = operationalExportFilename(snapshot.exported_at);
    document.body.append(link);
    link.click();
    link.remove();
    window.setTimeout(() => URL.revokeObjectURL(objectURL), 0);

    const errorCount = (snapshot.errors || []).length;
    elements.exportCount.textContent = errorCount === 0 ? "Export downloaded" : `Export downloaded with ${errorCount} error${errorCount === 1 ? "" : "s"}`;
    showNotice(errorCount === 0 ? "Operational export downloaded." : `Operational export downloaded with ${errorCount} error${errorCount === 1 ? "" : "s"}.`, errorCount === 0 ? "success" : "error");
  } catch (error) {
    elements.exportCount.textContent = "Export failed";
    showNotice(error.message, "error");
  } finally {
    elements.exportButton.disabled = !adminToken;
  }
}

async function buildOperationalExport() {
  const gatewayURL = normalizedGatewayURL();
  const requestLogQueryString = safeRequestLogQuery();
  const snapshot = {
    exported_at: new Date().toISOString(),
    exporter: "porthook-dashboard",
    sources: {
      dashboard_url: window.location.href,
      control_plane_url: window.location.origin,
      gateway_url: gatewayURL,
      event_limit: normalizedAuditEventLimit(),
      request_log_query: requestLogQueryString.query,
      request_log_filters: requestLogExportFilters(),
    },
    control_plane: {
      tokens: [],
      admin_tokens: [],
      reserved_subdomains: [],
      custom_domains: [],
      access_policies: [],
      audit_events: [],
    },
    gateway: {
      tunnels: [],
      tunnel_details: [],
      metrics: [],
      request_logs: [],
    },
    diagnostics: currentDiagnostics.map((check) => ({ ...check })),
    errors: [],
  };

  if (requestLogQueryString.error) {
    snapshot.errors.push({ component: "gateway", endpoint: "/api/v1/request-logs", error: requestLogQueryString.error });
  }

  const status = await captureOperationalExport(snapshot.errors, "control-plane", "/api/v1/status", () => localJSON("/api/v1/status"));
  if (status) {
    snapshot.control_plane.status = status;
  }
  const tokens = await captureOperationalExport(snapshot.errors, "control-plane", "/api/v1/tokens", () => apiRequest("/api/v1/tokens"));
  snapshot.control_plane.tokens = (tokens && tokens.tokens) || [];
  const adminTokens = await captureOperationalExport(snapshot.errors, "control-plane", "/api/v1/admin-tokens", () => apiRequest("/api/v1/admin-tokens"));
  snapshot.control_plane.admin_tokens = (adminTokens && adminTokens.tokens) || [];
  const reservations = await captureOperationalExport(snapshot.errors, "control-plane", "/api/v1/reserved-subdomains", () => apiRequest("/api/v1/reserved-subdomains"));
  snapshot.control_plane.reserved_subdomains = (reservations && reservations.reserved_subdomains) || [];
  const domains = await captureOperationalExport(snapshot.errors, "control-plane", "/api/v1/custom-domains", () => apiRequest("/api/v1/custom-domains"));
  snapshot.control_plane.custom_domains = (domains && domains.custom_domains) || [];
  const policies = await captureOperationalExport(snapshot.errors, "control-plane", "/api/v1/access-policies", () => apiRequest("/api/v1/access-policies"));
  snapshot.control_plane.access_policies = (policies && policies.access_policies) || [];
  const events = await captureOperationalExport(snapshot.errors, "control-plane", "/api/v1/events", () => apiRequest(`/api/v1/events?limit=${encodeURIComponent(normalizedAuditEventLimit())}`));
  snapshot.control_plane.audit_events = (events && events.events) || [];

  if (!gatewayURL) {
    snapshot.errors.push({ component: "gateway", endpoint: "*", error: "gateway URL is required" });
    return snapshot;
  }

  const tunnels = await captureOperationalExport(snapshot.errors, "gateway", "/api/v1/tunnels", () => gatewayJSON("/api/v1/tunnels"));
  snapshot.gateway.tunnels = (tunnels && tunnels.tunnels) || [];
  const tunnelDetails = await Promise.all(
    snapshot.gateway.tunnels
      .filter((tunnel) => tunnel.tunnel_id)
      .map((tunnel) => captureOperationalExport(snapshot.errors, "gateway", `/api/v1/tunnels/${tunnel.tunnel_id}`, () => gatewayJSON(`/api/v1/tunnels/${encodeURIComponent(tunnel.tunnel_id)}`))),
  );
  snapshot.gateway.tunnel_details = tunnelDetails.filter(Boolean).map((detail) => detail.tunnel || detail);

  const runtime = await captureOperationalExport(snapshot.errors, "gateway", "/api/v1/runtime", () => gatewayJSON("/api/v1/runtime"));
  if (runtime) {
    snapshot.gateway.runtime = runtime.runtime || {};
  }
  const metricsText = await captureOperationalExport(snapshot.errors, "gateway", "/metrics", () => gatewayText("/metrics"));
  if (metricsText !== null && metricsText !== undefined) {
    snapshot.gateway.metrics_text = metricsText;
    snapshot.gateway.metrics = parsePrometheusMetrics(metricsText);
  }
  if (!requestLogQueryString.error) {
    const requestLogs = await captureOperationalExport(snapshot.errors, "gateway", "/api/v1/request-logs", () => gatewayJSON(`/api/v1/request-logs?${requestLogQueryString.query}`));
    snapshot.gateway.request_logs = (requestLogs && requestLogs.request_logs) || [];
  }
  return snapshot;
}

async function localJSON(path) {
  const response = await fetch(path, { cache: "no-store" });
  const payload = await readPayload(response);
  if (!response.ok) {
    throw new Error(responseDetail(payload, response.status));
  }
  return payload || {};
}

async function captureOperationalExport(errors, component, endpoint, task) {
  try {
    return await task();
  } catch (error) {
    errors.push({ component, endpoint, error: error.message });
    return null;
  }
}

function safeRequestLogQuery() {
  try {
    return { query: requestLogQuery(), error: "" };
  } catch (error) {
    return { query: `limit=${encodeURIComponent(normalizedRequestLogLimit())}`, error: error.message };
  }
}

function requestLogExportFilters() {
  return {
    subdomain: elements.requestLogSubdomain.value.trim(),
    method: elements.requestLogMethod.value.trim(),
    host: elements.requestLogHost.value.trim(),
    path: elements.requestLogPath.value.trim(),
    status: elements.requestLogStatus.value.trim(),
    outcome: elements.requestLogOutcome.value.trim(),
    request_id: elements.requestLogRequestID.value.trim(),
    tunnel_id: elements.requestLogTunnelID.value.trim(),
    since: elements.requestLogSince.value.trim(),
    until: elements.requestLogUntil.value.trim(),
    limit: normalizedRequestLogLimit(),
  };
}

function operationalExportFilename(exportedAt) {
  const stamp = String(exportedAt || new Date().toISOString()).replace(/[:.]/g, "-");
  return `porthook-operational-export-${stamp}.json`;
}

elements.loginForm.addEventListener("submit", async (event) => {
  event.preventDefault();
  adminToken = elements.adminToken.value.trim();
  if (!adminToken) {
    showNotice("Admin token is required.", "error");
    return;
  }
  sessionStorage.setItem(storageKey, adminToken);
  setAuthenticated(true);
  try {
    await refreshApp();
  } catch (error) {
    showNotice(error.message, "error");
  }
});

elements.logoutButton.addEventListener("click", () => {
  adminToken = "";
  sessionStorage.removeItem(storageKey);
  elements.adminToken.value = "";
  showNotice("");
  clearCreatedToken();
  setAuthenticated(false);
});

elements.refreshButton.addEventListener("click", async () => {
  try {
    await refreshApp();
  } catch (error) {
    showNotice(error.message, "error");
  }
});

elements.exportButton.addEventListener("click", downloadOperationalExport);
elements.createForm.addEventListener("submit", createToken);
elements.copyCreatedToken.addEventListener("click", copyCreatedToken);
elements.adminTokenForm.addEventListener("submit", createAdminToken);
elements.copyCreatedAdminToken.addEventListener("click", copyCreatedAdminToken);
elements.reservationForm.addEventListener("submit", createReservation);
elements.customDomainForm.addEventListener("submit", createCustomDomain);
elements.accessPolicyForm.addEventListener("submit", saveAccessPolicy);
elements.accessMode.addEventListener("change", updateAccessPolicyFields);
elements.accessPolicyCancel.addEventListener("click", resetAccessPolicyForm);
elements.auditEventForm.addEventListener("submit", async (event) => {
  event.preventDefault();
  try {
    syncFilterStateToHash();
    await loadAuditEvents();
  } catch (error) {
    elements.auditEventCount.textContent = "Audit events unavailable";
    elements.auditEventsBody.replaceChildren();
    elements.auditEventsEmptyState.hidden = false;
    showNotice(error.message, "error");
  }
});
elements.auditEventLoadMore.addEventListener("click", async () => {
  try {
    await loadAuditEvents({ append: true });
  } catch (error) {
    elements.auditEventCount.textContent = "More audit events unavailable";
    showNotice(error.message, "error");
  }
});
elements.diagnosticsForm.addEventListener("submit", async (event) => {
  event.preventDefault();
  await runDiagnostics();
});
elements.gatewayForm.addEventListener("submit", async (event) => {
  event.preventDefault();
  const gatewayURL = normalizedGatewayURL();
  sessionStorage.setItem(gatewayStorageKey, gatewayURL);
  elements.gatewayURL.value = gatewayURL;
  await Promise.all([loadTunnels(), loadGatewayRuntime(), loadGatewayMetrics(), loadRequestLogs()]);
});
elements.requestLogForm.addEventListener("submit", async (event) => {
  event.preventDefault();
  syncFilterStateToHash();
  await loadRequestLogs();
});
elements.requestLogLoadMore.addEventListener("click", async () => {
  await loadRequestLogs({ append: true });
});
elements.tunnelDetailClose.addEventListener("click", clearTunnelDetail);
for (const input of [
  elements.auditEventEvent,
  elements.auditEventLevel,
  elements.auditEventRequestID,
  elements.auditEventRemoteIP,
  elements.auditEventField,
  elements.auditEventLimit,
]) {
  input.addEventListener("input", () => {
    resetAuditEventPagination();
    renderAuditEvents(currentAuditEvents);
    syncFilterStateToHash();
  });
}
for (const input of [
  elements.requestLogSubdomain,
  elements.requestLogMethod,
  elements.requestLogHost,
  elements.requestLogPath,
  elements.requestLogStatus,
  elements.requestLogOutcome,
  elements.requestLogRequestID,
  elements.requestLogTunnelID,
  elements.requestLogSince,
  elements.requestLogUntil,
  elements.requestLogLimit,
]) {
  input.addEventListener("input", () => {
    resetRequestLogPagination();
    renderRequestLogs(currentRequestLogs);
    syncFilterStateToHash();
  });
}

function restoreFilterStateFromHash() {
  const hash = window.location.hash.replace(/^#/, "");
  if (!hash.startsWith(`${dashboardHashPrefix}?`)) {
    return;
  }
  const params = new URLSearchParams(hash.slice(dashboardHashPrefix.length + 1));
  restoringFilterState = true;
  try {
    applyFilterState(params, auditEventStateFields);
    applyFilterState(params, requestLogStateFields);
  } finally {
    restoringFilterState = false;
  }
}

function applyFilterState(params, fields) {
  for (const [param, elementName] of fields) {
    if (!params.has(param)) {
      continue;
    }
    elements[elementName].value = params.get(param) || "";
  }
}

function syncFilterStateToHash() {
  if (restoringFilterState) {
    return;
  }
  const params = new URLSearchParams();
  appendFilterState(params, auditEventStateFields);
  appendFilterState(params, requestLogStateFields);
  const nextHash = params.toString() ? `#${dashboardHashPrefix}?${params}` : "";
  const nextURL = `${window.location.pathname}${window.location.search}${nextHash}`;
  window.history.replaceState(null, "", nextURL);
}

function appendFilterState(params, fields) {
  for (const [param, elementName] of fields) {
    const value = elements[elementName].value.trim();
    if (value) {
      params.set(param, value);
    }
  }
}

updateAccessPolicyFields();
setAuthenticated(Boolean(adminToken));
if (adminToken) {
  refreshApp().catch((error) => showNotice(error.message, "error"));
}
