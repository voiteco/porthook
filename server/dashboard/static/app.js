const storageKey = "porthook.dashboard.adminToken";

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
};

let adminToken = sessionStorage.getItem(storageKey) || "";

function setAuthenticated(authenticated) {
  elements.loginPanel.hidden = authenticated;
  elements.appPanel.hidden = !authenticated;
  elements.logoutButton.hidden = !authenticated;
  elements.refreshButton.disabled = !authenticated;
  if (!authenticated) {
    elements.tokensBody.replaceChildren();
    elements.emptyState.hidden = true;
    elements.tokenCount.textContent = "No tokens loaded";
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

  const text = await response.text();
  let payload = null;
  if (text) {
    try {
      payload = JSON.parse(text);
    } catch {
      payload = text.trim();
    }
  }

  if (!response.ok) {
    if (response.status === 401 || response.status === 403) {
      throw new Error("Admin token was rejected.");
    }
    throw new Error(typeof payload === "string" && payload ? payload : `Request failed with status ${response.status}.`);
  }

  return payload;
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

async function loadTokens() {
  showNotice("");
  clearCreatedToken();
  elements.tokenCount.textContent = "Loading tokens";
  elements.tokensBody.replaceChildren();
  elements.emptyState.hidden = true;

  const payload = await apiRequest("/api/v1/tokens");
  const tokens = payload.tokens || [];
  renderTokens(tokens);
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
    actionCell(token),
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

function statusCell(token) {
  const item = document.createElement("td");
  const badge = document.createElement("span");
  badge.className = token.revoked_at ? "badge revoked" : "badge active";
  badge.textContent = token.revoked_at ? `Revoked ${formatTime(token.revoked_at)}` : "Active";
  item.append(badge);
  return item;
}

function actionCell(token) {
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
    await loadTokens();
    showNotice(`Revoked token ${token.id}.`, "success");
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
    await Promise.all([loadTokens(), refreshStatus()]);
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
    await Promise.all([loadTokens(), refreshStatus()]);
  } catch (error) {
    showNotice(error.message, "error");
  }
});

elements.createForm.addEventListener("submit", createToken);
elements.copyCreatedToken.addEventListener("click", copyCreatedToken);

setAuthenticated(Boolean(adminToken));
if (adminToken) {
  Promise.all([loadTokens(), refreshStatus()]).catch((error) => showNotice(error.message, "error"));
}
