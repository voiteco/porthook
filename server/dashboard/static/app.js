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
  gatewayForm: document.querySelector("#gateway-form"),
  gatewayURL: document.querySelector("#gateway-url"),
  tunnelsBody: document.querySelector("#tunnels-body"),
  tunnelsEmptyState: document.querySelector("#tunnels-empty-state"),
  tunnelCount: document.querySelector("#tunnel-count"),
};

let adminToken = sessionStorage.getItem(storageKey) || "";
let currentTokens = [];

elements.gatewayURL.value = sessionStorage.getItem(gatewayStorageKey) || defaultGatewayURL();

function setAuthenticated(authenticated) {
  elements.loginPanel.hidden = authenticated;
  elements.appPanel.hidden = !authenticated;
  elements.logoutButton.hidden = !authenticated;
  elements.refreshButton.disabled = !authenticated;
  if (!authenticated) {
    currentTokens = [];
    elements.tokensBody.replaceChildren();
    elements.reservationsBody.replaceChildren();
    elements.tunnelsBody.replaceChildren();
    elements.emptyState.hidden = true;
    elements.reservationsEmptyState.hidden = true;
    elements.tunnelsEmptyState.hidden = true;
    elements.tokenCount.textContent = "No tokens loaded";
    elements.reservationCount.textContent = "No reservations loaded";
    elements.tunnelCount.textContent = "No tunnels loaded";
    renderReservationTokenOptions();
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
  await Promise.all([loadTokens(), loadReservations(), refreshStatus(), loadTunnels({ silent: true })]);
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
  const reservations = payload.reserved_subdomains || [];
  renderReservations(reservations);
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

async function loadTunnels({ silent = false } = {}) {
  const baseURL = normalizedGatewayURL();
  if (!baseURL) {
    elements.tunnelCount.textContent = "Gateway URL required";
    elements.tunnelsBody.replaceChildren();
    elements.tunnelsEmptyState.hidden = false;
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
    renderTunnels(payload.tunnels || []);
  } catch (error) {
    elements.tunnelCount.textContent = "Gateway unavailable";
    elements.tunnelsBody.replaceChildren();
    elements.tunnelsEmptyState.hidden = false;
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
  row.append(
    cell(tunnel.subdomain),
    monoCell(tunnel.tunnel_id),
    linkCell(tunnel.public_url),
    cell(tunnel.protocol || "http"),
    cell(formatTime(tunnel.connected_at)),
  );
  return row;
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
    showNotice(`Deleted reserved subdomain ${reservation.name}.`, "success");
  } catch (error) {
    showNotice(error.message, "error");
  }
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
elements.gatewayForm.addEventListener("submit", async (event) => {
  event.preventDefault();
  const gatewayURL = normalizedGatewayURL();
  sessionStorage.setItem(gatewayStorageKey, gatewayURL);
  elements.gatewayURL.value = gatewayURL;
  await loadTunnels();
});

setAuthenticated(Boolean(adminToken));
if (adminToken) {
  refreshApp().catch((error) => showNotice(error.message, "error"));
}
