const storageKey = "porthook.dashboard.adminToken";
const gatewayStorageKey = "porthook.dashboard.gatewayURL";

const elements = {
  loginPanel: document.querySelector("#login-panel"),
  appPanel: document.querySelector("#app-panel"),
  loginForm: document.querySelector("#login-form"),
  adminToken: document.querySelector("#admin-token"),
  logoutButton: document.querySelector("#logout-button"),
  refreshButton: document.querySelector("#refresh-button"),
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
  requestLogStatus: document.querySelector("#request-log-status"),
  requestLogOutcome: document.querySelector("#request-log-outcome"),
  requestLogRequestID: document.querySelector("#request-log-request-id"),
  requestLogLimit: document.querySelector("#request-log-limit"),
  requestLogsBody: document.querySelector("#request-logs-body"),
  requestLogsEmptyState: document.querySelector("#request-logs-empty-state"),
  requestLogCount: document.querySelector("#request-log-count"),
};

let adminToken = sessionStorage.getItem(storageKey) || "";
let currentTokens = [];
let currentReservations = [];
let currentCustomDomains = [];
let currentAccessPolicies = [];
let currentTunnels = [];
let currentRequestLogs = [];
let editingAccessPolicyID = "";
let selectedTunnelID = "";

elements.gatewayURL.value = sessionStorage.getItem(gatewayStorageKey) || defaultGatewayURL();

function setAuthenticated(authenticated) {
  elements.loginPanel.hidden = authenticated;
  elements.appPanel.hidden = !authenticated;
  elements.logoutButton.hidden = !authenticated;
  elements.refreshButton.disabled = !authenticated;
  if (!authenticated) {
    currentTokens = [];
    currentReservations = [];
    currentCustomDomains = [];
    currentAccessPolicies = [];
    currentTunnels = [];
    currentRequestLogs = [];
    editingAccessPolicyID = "";
    selectedTunnelID = "";
    elements.tokensBody.replaceChildren();
    elements.reservationsBody.replaceChildren();
    elements.customDomainsBody.replaceChildren();
    elements.accessPoliciesBody.replaceChildren();
    elements.tunnelsBody.replaceChildren();
    elements.requestLogsBody.replaceChildren();
    elements.emptyState.hidden = true;
    elements.reservationsEmptyState.hidden = true;
    elements.customDomainsEmptyState.hidden = true;
    elements.accessPoliciesEmptyState.hidden = true;
    elements.tunnelsEmptyState.hidden = true;
    elements.requestLogsEmptyState.hidden = true;
    elements.tokenCount.textContent = "No tokens loaded";
    elements.reservationCount.textContent = "No reservations loaded";
    elements.customDomainCount.textContent = "No custom domains loaded";
    elements.accessPolicyCount.textContent = "No access policies loaded";
    elements.tunnelCount.textContent = "No tunnels loaded";
    elements.requestLogCount.textContent = "No request logs loaded";
    clearTunnelDetail();
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
    if (response.status === 401 || response.status === 403) {
      throw new Error("Admin token was rejected.");
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
  await Promise.all([loadTokens(), refreshStatus(), loadTunnels({ silent: true }), loadRequestLogs({ silent: true })]);
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
    detailItem("Protocol", tunnel.protocol || "http"),
    detailItem("Agent version", tunnel.agent_version || "-"),
    detailItem("Protocol version", tunnel.protocol_version || "-"),
    detailItem("Connected", formatTime(tunnel.connected_at)),
    detailItem("Streams", `${tunnel.active_streams || 0}/${tunnel.stream_capacity || 0}`),
    detailItem("Recent requests", String(requests.count || 0)),
    detailItem("Recent errors", String(requests.error_count || 0)),
    detailItem("Last status", requests.last_status ? String(requests.last_status) : "-"),
    detailItem("Last outcome", requests.last_outcome || "-"),
    detailItem("Last request ID", requests.last_request_id || "-", true),
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

async function loadRequestLogs({ silent = false } = {}) {
  const baseURL = normalizedGatewayURL();
  if (!baseURL) {
    elements.requestLogCount.textContent = "Gateway URL required";
    currentRequestLogs = [];
    renderRequestLogs([]);
    renderOperationalOverview();
    return;
  }

  const limit = normalizedRequestLogLimit();
  elements.requestLogCount.textContent = "Loading request logs";
  elements.requestLogsBody.replaceChildren();
  elements.requestLogsEmptyState.hidden = true;

  try {
    const response = await fetch(`${baseURL}/api/v1/request-logs?limit=${encodeURIComponent(limit)}`, { cache: "no-store" });
    const payload = await readPayload(response);
    if (!response.ok) {
      throw new Error(typeof payload === "string" && payload ? payload : `Gateway returned status ${response.status}.`);
    }
    currentRequestLogs = payload.request_logs || [];
    renderRequestLogs(currentRequestLogs);
    renderOperationalOverview();
  } catch (error) {
    currentRequestLogs = [];
    elements.requestLogCount.textContent = "Request logs unavailable";
    elements.requestLogsBody.replaceChildren();
    elements.requestLogsEmptyState.hidden = false;
    renderOperationalOverview();
    if (!silent) {
      showNotice(error.message, "error");
    }
  }
}

function renderRequestLogs(logs) {
  const filtered = filterRequestLogs(logs);
  elements.requestLogsBody.replaceChildren(...filtered.map(renderRequestLogRow));
  elements.requestLogsEmptyState.hidden = filtered.length !== 0;
  const loaded = logs.length === filtered.length ? `${logs.length}` : `${filtered.length} of ${logs.length}`;
  elements.requestLogCount.textContent = `${loaded} request log${filtered.length === 1 ? "" : "s"}`;
}

function renderRequestLogRow(entry) {
  const row = document.createElement("tr");
  row.append(
    cell(formatTime(entry.time)),
    cell(entry.subdomain || "-"),
    cell(entry.method || "-"),
    cell(entry.path || "-"),
    cell(entry.status || "-"),
    cell(entry.outcome || "-"),
    monoCell(entry.request_id || "-"),
    cell(`${entry.duration_ms || 0} ms`),
    cell(`${entry.request_bytes || 0}/${entry.response_bytes || 0}`),
    cell(entry.remote_ip || "-"),
  );
  return row;
}

function filterRequestLogs(logs) {
  const subdomain = elements.requestLogSubdomain.value.trim().toLowerCase();
  const status = elements.requestLogStatus.value.trim();
  const outcome = elements.requestLogOutcome.value.trim().toLowerCase();
  const requestID = elements.requestLogRequestID.value.trim().toLowerCase();
  return logs.filter((entry) => {
    if (subdomain && String(entry.subdomain || "").toLowerCase() !== subdomain) {
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
    return true;
  });
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

function detailItem(label, value, mono = false) {
  const item = document.createElement("div");
  item.className = "detail-item";
  const labelEl = document.createElement("span");
  labelEl.textContent = label;
  const valueEl = document.createElement("strong");
  valueEl.textContent = value;
  if (mono) {
    valueEl.classList.add("mono");
  }
  item.append(labelEl, valueEl);
  return item;
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
  try {
    await navigator.clipboard.writeText(value);
    showNotice("Copied plaintext token.", "success");
  } catch {
    showNotice("Copy failed. Select the token manually.", "error");
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

elements.createForm.addEventListener("submit", createToken);
elements.copyCreatedToken.addEventListener("click", copyCreatedToken);
elements.reservationForm.addEventListener("submit", createReservation);
elements.customDomainForm.addEventListener("submit", createCustomDomain);
elements.accessPolicyForm.addEventListener("submit", saveAccessPolicy);
elements.accessMode.addEventListener("change", updateAccessPolicyFields);
elements.accessPolicyCancel.addEventListener("click", resetAccessPolicyForm);
elements.gatewayForm.addEventListener("submit", async (event) => {
  event.preventDefault();
  const gatewayURL = normalizedGatewayURL();
  sessionStorage.setItem(gatewayStorageKey, gatewayURL);
  elements.gatewayURL.value = gatewayURL;
  await Promise.all([loadTunnels(), loadRequestLogs()]);
});
elements.requestLogForm.addEventListener("submit", async (event) => {
  event.preventDefault();
  await loadRequestLogs();
});
elements.tunnelDetailClose.addEventListener("click", clearTunnelDetail);
for (const input of [elements.requestLogSubdomain, elements.requestLogStatus, elements.requestLogOutcome, elements.requestLogRequestID]) {
  input.addEventListener("input", () => renderRequestLogs(currentRequestLogs));
}

updateAccessPolicyFields();
setAuthenticated(Boolean(adminToken));
if (adminToken) {
  refreshApp().catch((error) => showNotice(error.message, "error"));
}
