/* eslint-disable no-unused-vars */
/* eslint-disable no-undef */

'use strict';

class API {

  constructor() {
    // When set, all calls are transparently proxied through the local server
    // to a remote Cascade instance. The browser never communicates directly
    // with the remote — the token stays on the backend.
    this._remoteId = null;
    // Optional callback invoked when a proxy call fails with an unrecoverable
    // error (401 Unauthorized or 5xx server error). The app registers this to
    // auto-switch back to local mode when the remote becomes unavailable.
    this._onRemoteError = null;
  }

  /** Switch all subsequent calls to go through a remote server proxy. */
  setRemote(id) { this._remoteId = id; }

  /** Switch back to the local server. */
  clearRemote() { this._remoteId = null; }

  /** Returns the active remote id, or null if local. */
  getRemoteId() { return this._remoteId; }

  /** Like call(), but always targets the local server regardless of activeRemoteId. */
  async callLocal(opts) {
    const saved = this._remoteId;
    this._remoteId = null;
    try {
      return await this.call(opts);
    } finally {
      this._remoteId = saved;
    }
  }

  async call({ method, path, body, allowStatus = [] }) {
    // Compute API base URL from the first path segment of the current page.
    // Works correctly whether the page was loaded with or without a trailing slash,
    // and whether there is a reverse-proxy prefix (e.g. Caddy ADMIN_PATH) or not.
    // Examples:
    //   /a3c8ac6953f44ce1bf1e0c06/  → /a3c8ac6953f44ce1bf1e0c06/api
    //   /a3c8ac6953f44ce1bf1e0c06   → /a3c8ac6953f44ce1bf1e0c06/api  (no trailing slash — safe)
    //   /                           → /api  (direct access, no proxy prefix)
    const segs = window.location.pathname.split('/').filter(Boolean);
    const apiBase = segs.length > 0
      ? `${window.location.origin}/${segs[0]}/api`
      : `${window.location.origin}/api`;

    // If a remote is active, route through the proxy endpoint.
    // Remotes-management calls (/remotes/*) always go to local.
    const effectivePath = (this._remoteId && !path.startsWith('/remotes'))
      ? `/remotes/${this._remoteId}/proxy${path}`
      : path;

    const res = await fetch(`${apiBase}${effectivePath}`, {
      method: method.toUpperCase(), // Node.js 22 llhttp: HTTP method must be uppercase
      headers: {
        'Content-Type': 'application/json',
      },
      body: body
        ? JSON.stringify(body)
        : undefined,
    });

    // If a proxy call fails with an unrecoverable status (401 or 5xx),
    // notify the app so it can switch back to local mode gracefully.
    // This prevents the user from seeing a confusing "login window" when
    // the remote becomes temporarily unreachable.
    const isProxyCall = this._remoteId && effectivePath.startsWith(`/remotes/${this._remoteId}/proxy`);
    if (isProxyCall && (res.status === 401 || res.status >= 500)) {
      if (typeof this._onRemoteError === 'function') {
        this._onRemoteError(res.status, effectivePath);
      }
    }

    if (res.status === 204) {
      return undefined;
    }

    let json;
    try {
      json = await res.json();
    } catch (_) {
      // Сервер вернул пустой или не-JSON body
      throw new Error(`Server error ${res.status}: ${res.statusText}`);
    }

    if (!res.ok && !allowStatus.includes(res.status)) {
      throw new Error(json.message || json.error || res.statusText);
    }

    return json;
  }

  async getRelease() {
    return this.call({
      method: 'get',
      path: '/release',
    });
  }

  async getLang() {
    return this.call({
      method: 'get',
      path: '/lang',
    });
  }

  async getRememberMeEnabled() {
    return this.call({
      method: 'get',
      path: '/remember-me',
    });
  }

  async getuiTrafficStats() {
    return this.call({
      method: 'get',
      path: '/ui-traffic-stats',
    });
  }

  async getChartType() {
    return this.call({
      method: 'get',
      path: '/ui-chart-type',
    });
  }

  async getWGEnableOneTimeLinks() {
    return this.call({
      method: 'get',
      path: '/wg-enable-one-time-links',
    });
  }

  async getWGEnableExpireTime() {
    return this.call({
      method: 'get',
      path: '/wg-enable-expire-time',
    });
  }

  async getAvatarSettings() {
    return this.call({
      method: 'get',
      path: '/ui-avatar-settings',
    });
  }

  async getSession() {
    return this.call({
      method: 'get',
      path: '/session',
    });
  }

  async createSession({ username, password, remember }) {
    return this.call({
      method: 'post',
      path: '/session',
      body: { username, password, remember },
    });
  }

  async deleteSession() {
    return this.call({
      method: 'delete',
      path: '/session',
    });
  }

  async getClients() {
    return this.call({
      method: 'get',
      path: '/wireguard/client',
    }).then((clients) => clients.map((client) => ({
      ...client,
      createdAt: new Date(client.createdAt),
      updatedAt: new Date(client.updatedAt),
      expiredAt: client.expiredAt !== null
        ? new Date(client.expiredAt)
        : null,
      latestHandshakeAt: client.latestHandshakeAt !== null
        ? new Date(client.latestHandshakeAt)
        : null,
    })));
  }

  async createClient({ name, expiredDate }) {
    return this.call({
      method: 'post',
      path: '/wireguard/client',
      body: { name, expiredDate },
    });
  }

  async deleteClient({ clientId }) {
    return this.call({
      method: 'delete',
      path: `/wireguard/client/${clientId}`,
    });
  }

  async showOneTimeLink({ clientId }) {
    return this.call({
      method: 'post',
      path: `/wireguard/client/${clientId}/generateOneTimeLink`,
    });
  }

  async enableClient({ clientId }) {
    return this.call({
      method: 'post',
      path: `/wireguard/client/${clientId}/enable`,
    });
  }

  async disableClient({ clientId }) {
    return this.call({
      method: 'post',
      path: `/wireguard/client/${clientId}/disable`,
    });
  }

  async updateClientName({ clientId, name }) {
    return this.call({
      method: 'put',
      path: `/wireguard/client/${clientId}/name/`,
      body: { name },
    });
  }

  async updateClientAddress({ clientId, address }) {
    return this.call({
      method: 'put',
      path: `/wireguard/client/${clientId}/address/`,
      body: { address },
    });
  }

  async updateClientExpireDate({ clientId, expireDate }) {
    return this.call({
      method: 'put',
      path: `/wireguard/client/${clientId}/expireDate/`,
      body: { expireDate },
    });
  }

  async restoreConfiguration(file) {
    return this.call({
      method: 'put',
      path: '/wireguard/restore',
      body: { file },
    });
  }

  async getUiSortClients() {
    return this.call({
      method: 'get',
      path: '/ui-sort-clients',
    });
  }

  // ============================================================
  // Dashboard API
  // ============================================================

  async getDashboardWidgets(page = 'dashboard') {
    return this.call({ method: 'get', path: `/dashboard/widgets?page=${page}` });
  }

  async putDashboardWidgets(widgets, page = 'dashboard') {
    return this.call({ method: 'put', path: `/dashboard/widgets?page=${page}`, body: { widgets } });
  }

  async getSystemInfo() {
    return this.call({ method: 'get', path: '/dashboard/system-info' });
  }

  // ============================================================
  // Settings API
  // ============================================================

  async getSettings() {
    return this.call({
      method: 'get',
      path: '/settings',
    });
  }

  async updateSettings(settings) {
    return this.call({
      method: 'put',
      path: '/settings',
      body: settings,
    });
  }

  // ============================================================
  // AWG2 Templates API
  // ============================================================

  async getTemplates() {
    return this.call({
      method: 'get',
      path: '/templates',
    });
  }

  async createTemplate(template) {
    return this.call({
      method: 'post',
      path: '/templates',
      body: template,
    });
  }

  async updateTemplate({ templateId, ...updates }) {
    return this.call({
      method: 'put',
      path: `/templates/${templateId}`,
      body: updates,
    });
  }

  async deleteTemplate({ templateId }) {
    return this.call({
      method: 'delete',
      path: `/templates/${templateId}`,
    });
  }

  async setDefaultTemplate({ templateId }) {
    return this.call({
      method: 'post',
      path: `/templates/${templateId}/set-default`,
    });
  }

  /**
   * Get AWG2 settings from a template.
   * H1-H4 are copied as-is (ranges). The AWG protocol randomises within the range per handshake.
   * Used when the user selects "Load from template" in the Instance form.
   */
  async applyTemplate({ templateId }) {
    return this.call({
      method: 'post',
      path: `/templates/${templateId}/apply`,
    });
  }

  /**
   * generateTemplate — сгенерировать AWG 2.0 параметры (порт AmneziaWG-Architect).
   * @param {object} opts
   * @param {string} [opts.profile]    — профиль CPS ('random', 'quic_initial', 'tls_client_hello', ...)
   * @param {string} [opts.intensity]  — интенсивность ('low', 'medium', 'high')
   * @param {string} [opts.host]       — кастомный хост для SNI
   * @param {string} [opts.browser]    — Browser Fingerprint: chrome|firefox|safari|edge|yandex_desktop|yandex_mobile
   * @param {number} [opts.iterCount]  — счётчик попыток
   * @param {number} [opts.jc]         — базовое Jc
   * @param {string} [opts.saveName]   — если задан, сохраняет как шаблон
   * @returns {{ params, profiles[, template] }}
   */
  async generateTemplate({ profile, intensity, host, browser, iterCount, jc, saveName } = {}) {
    return this.call({
      method: 'post',
      path: '/templates/generate',
      body: { profile, intensity, host, browser, iterCount, jc, saveName },
    });
  }

  // ============================================================
  // Tunnel Interfaces API
  // ============================================================

  async getTunnelInterfaces() {
    return this.call({
      method: 'get',
      path: '/tunnel-interfaces',
    });
  }

  async createTunnelInterface(data) {
    return this.call({
      method: 'post',
      path: '/tunnel-interfaces',
      body: data,
    });
  }

  async importTunnelConfServer({ name, conf }) {
    return this.call({
      method: 'post',
      path: '/tunnel-interfaces/import-conf-server',
      body: { name, conf },
    });
  }

  async parseTunnelConf({ conf }) {
    return this.call({
      method: 'post',
      path: '/tunnel-interfaces/parse-conf',
      body: { conf },
    });
  }


  async importTunnelConf({ name, conf }) {
    return this.call({
      method: 'post',
      path: '/tunnel-interfaces/import-conf',
      body: { name, conf },
    });
  }

  async importTunnelBackup({ json, listenPort }) {
    return this.call({
      method: 'post',
      path: '/tunnel-interfaces/import-backup',
      body: { json, listenPort },
    });
  }

  async exportTunnelInterface({ interfaceId, includePeers }) {
    const peers = includePeers ? '1' : '0';
    const res = await fetch(`./api/tunnel-interfaces/${interfaceId}/export?peers=${peers}`, {
      headers: { Authorization: `Bearer ${this.token}` },
    });
    if (!res.ok) throw new Error(await res.text());
    return res.blob();
  }

  async importTunnelInterface({ json, listenPort }) {
    return this.call({
      method: 'post',
      path: '/tunnel-interfaces/import-interface',
      body: { json, listenPort },
    });
  }

  async updateTunnelInterface({ interfaceId, ...updates }) {
    return this.call({
      method: 'patch',
      path: `/tunnel-interfaces/${interfaceId}`,
      body: updates,
    });
  }

  async deleteTunnelInterface({ interfaceId }) {
    return this.call({
      method: 'delete',
      path: `/tunnel-interfaces/${interfaceId}`,
    });
  }

  async startTunnelInterface({ interfaceId }) {
    return this.call({
      method: 'post',
      path: `/tunnel-interfaces/${interfaceId}/start`,
    });
  }

  async stopTunnelInterface({ interfaceId }) {
    return this.call({
      method: 'post',
      path: `/tunnel-interfaces/${interfaceId}/stop`,
    });
  }

  async restartTunnelInterface({ interfaceId }) {
    return this.call({
      method: 'post',
      path: `/tunnel-interfaces/${interfaceId}/restart`,
    });
  }

  // ============================================================
  // Peers API (for Tunnel Interfaces)
  // ============================================================

  async getTunnelInterfacePeers({ interfaceId }) {
    return this.call({
      method: 'get',
      path: `/tunnel-interfaces/${interfaceId}/peers`,
    });
  }

  async createTunnelInterfacePeer({ interfaceId, ...peerData }) {
    return this.call({
      method: 'post',
      path: `/tunnel-interfaces/${interfaceId}/peers`,
      body: peerData,
    });
  }

  async updateTunnelInterfacePeer({ interfaceId, peerId, ...updates }) {
    return this.call({
      method: 'patch',
      path: `/tunnel-interfaces/${interfaceId}/peers/${peerId}`,
      body: updates,
    });
  }

  async deleteTunnelInterfacePeer({ interfaceId, peerId }) {
    return this.call({
      method: 'delete',
      path: `/tunnel-interfaces/${interfaceId}/peers/${peerId}`,
    });
  }

  async getPeerConfig({ interfaceId, peerId }) {
    const segs = window.location.pathname.split('/').filter(Boolean);
    const apiBase = segs.length > 0
      ? `${window.location.origin}/${segs[0]}/api`
      : `${window.location.origin}/api`;
    const path = `/tunnel-interfaces/${interfaceId}/peers/${peerId}/config`;
    const effectivePath = (this._remoteId && !path.startsWith('/remotes'))
      ? `/remotes/${this._remoteId}/proxy${path}`
      : path;
    const res = await fetch(`${apiBase}${effectivePath}`);
    if (!res.ok) throw new Error(res.statusText);
    return res.text();
  }

  async enablePeer({ interfaceId, peerId }) {
    return this.call({
      method: 'post',
      path: `/tunnel-interfaces/${interfaceId}/peers/${peerId}/enable`,
    });
  }

  async disablePeer({ interfaceId, peerId }) {
    return this.call({
      method: 'post',
      path: `/tunnel-interfaces/${interfaceId}/peers/${peerId}/disable`,
    });
  }

  async updatePeerName({ interfaceId, peerId, name }) {
    return this.call({
      method: 'put',
      path: `/tunnel-interfaces/${interfaceId}/peers/${peerId}/name`,
      body: { name },
    });
  }

  async updatePeerAddress({ interfaceId, peerId, address }) {
    return this.call({
      method: 'put',
      path: `/tunnel-interfaces/${interfaceId}/peers/${peerId}/address`,
      body: { address },
    });
  }

  async updatePeerExpireDate({ interfaceId, peerId, expireDate }) {
    return this.call({
      method: 'put',
      path: `/tunnel-interfaces/${interfaceId}/peers/${peerId}/expireDate`,
      body: { expireDate },
    });
  }

  async generatePeerOneTimeLink({ interfaceId, peerId }) {
    return this.call({
      method: 'post',
      path: `/tunnel-interfaces/${interfaceId}/peers/${peerId}/generateOneTimeLink`,
    });
  }

  /**
   * Экспортировать параметры Interconnect пира в JSON.
   * Возвращает объект для передачи удалённой стороне (та импортирует через importPeerJSON).
   * Доступен только для пиров с peerType === 'interconnect'.
   * Поля: name, publicKey, presharedKey, endpoint, persistentKeepalive, allowedIPs, clientAllowedIPs.
   */
  async exportPeerJSON({ interfaceId, peerId }) {
    return this.call({
      method: 'get',
      path: `/tunnel-interfaces/${interfaceId}/peers/${peerId}/export-json`,
    });
  }

  /**
   * Создать Interconnect пир из JSON экспортированного другой стороной.
   * peerData — объект полученный от exportPeerJSON() удалённой стороны.
   * peerType автоматически устанавливается в 'interconnect'.
   * Ключи не генерируются — они содержатся в импортируемом JSON.
   */
  async importPeerJSON({ interfaceId, ...peerData }) {
    return this.call({
      method: 'post',
      path: `/tunnel-interfaces/${interfaceId}/peers/import-json`,
      body: peerData,
    });
  }

  /**
   * Импортировать один или несколько клиентских .conf файлов, чтобы восстановить
   * приватные ключи пиров (например, после импорта интерфейса с другого сервера).
   * Возвращает { matched, unmatched: [filenames], peers: [...] }.
   * @param {{ interfaceId: string, files: File[] }}
   */
  async importClientConfigs({ interfaceId, files }) {
    const form = new FormData();
    for (const f of files) form.append('configs', f);
    const res = await fetch(`./api/tunnel-interfaces/${interfaceId}/peers/import-client-configs`, {
      method: 'POST',
      headers: { Authorization: `Bearer ${this.token}` },
      body: form,
    });
    if (!res.ok) {
      const err = await res.json().catch(() => ({ message: res.statusText }));
      throw new Error(err.message || res.statusText);
    }
    return res.json();
  }

  /**
   * Экспортировать AWG2 параметры обфускации интерфейса.
   * Возвращает объект с Jc, Jmin, Jmax, S1-S4, H1-H4, I1-I5.
   * Формат совместим с createTemplate() — можно сохранить как профиль.
   * Ошибка 400 если интерфейс не AWG2.
   */
  async exportObfuscationParams({ interfaceId }) {
    return this.call({
      method: 'get',
      path: `/tunnel-interfaces/${interfaceId}/export-obfuscation`,
    });
  }

  /**
   * Экспортировать параметры своего интерфейса для передачи удалённой стороне.
   * Удалённая сторона импортирует JSON через importPeerJSON() → создаёт пир для нас.
   * Возвращает: name, publicKey, endpoint, address, protocol, settings (AWG2 only).
   */
  async exportInterfaceParams({ interfaceId }) {
    return this.call({
      method: 'get',
      path: `/tunnel-interfaces/${interfaceId}/export-params`,
    });
  }

  async backupTunnelInterface({ interfaceId }) {
    return this.call({
      method: 'get',
      path: `/tunnel-interfaces/${interfaceId}/backup`,
    });
  }

  async restoreTunnelInterface({ interfaceId, file }) {
    return this.call({
      method: 'put',
      path: `/tunnel-interfaces/${interfaceId}/restore`,
      body: { file },
    });
  }

  // ============================================================
  // System Interfaces API
  // ============================================================

  async getSystemInterfaces() {
    return this.call({
      method: 'get',
      path: '/system/interfaces',
    });
  }

  // ============================================================
  // Gateways API
  // ============================================================

  async getGateways() {
    return this.call({
      method: 'get',
      path: '/gateways',
    });
  }

  async createGateway(data) {
    return this.call({
      method: 'post',
      path: '/gateways',
      body: data,
    });
  }

  async updateGateway({ gatewayId, ...updates }) {
    return this.call({
      method: 'patch',
      path: `/gateways/${gatewayId}`,
      body: updates,
    });
  }

  async deleteGateway({ gatewayId }) {
    return this.call({
      method: 'delete',
      path: `/gateways/${gatewayId}`,
    });
  }

  // ============================================================
  // Gateway Groups API
  // ============================================================

  async getGatewayGroups() {
    return this.call({
      method: 'get',
      path: '/gateway-groups',
    });
  }

  async createGatewayGroup(data) {
    return this.call({
      method: 'post',
      path: '/gateway-groups',
      body: data,
    });
  }

  async updateGatewayGroup({ groupId, ...updates }) {
    return this.call({
      method: 'patch',
      path: `/gateway-groups/${groupId}`,
      body: updates,
    });
  }

  async deleteGatewayGroup({ groupId }) {
    return this.call({
      method: 'delete',
      path: `/gateway-groups/${groupId}`,
    });
  }

  // ============================================================
  // Routing API
  // ============================================================

  async getRoutingTables() {
    return this.call({
      method: 'get',
      path: '/routing/tables',
    });
  }

  async getKernelRoutes(table = 'main') {
    return this.call({
      method: 'get',
      path: `/routing/table?table=${encodeURIComponent(table)}`,
    });
  }

  async testRoute(ip, src) {
    let qs = `ip=${encodeURIComponent(ip)}`;
    if (src) qs += `&src=${encodeURIComponent(src)}`;
    return this.call({ method: 'get', path: `/routing/test?${qs}` });
  }

  async getStaticRoutes() {
    return this.call({
      method: 'get',
      path: '/routing/routes',
    });
  }

  async createStaticRoute(data) {
    return this.call({
      method: 'post',
      path: '/routing/routes',
      body: data,
    });
  }

  async toggleStaticRoute({ routeId, enabled }) {
    return this.call({
      method: 'patch',
      path: `/routing/routes/${routeId}`,
      body: { enabled },
    });
  }

  async updateStaticRoute({ routeId, data }) {
    return this.call({
      method: 'patch',
      path: `/routing/routes/${routeId}`,
      body: data,
    });
  }

  async deleteStaticRoute({ routeId }) {
    return this.call({
      method: 'delete',
      path: `/routing/routes/${routeId}`,
    });
  }

  // ============================================================
  // NAT API — Source NAT (POSTROUTING)
  // ============================================================

  /**
   * Получить список сетевых интерфейсов хоста.
   * Используется для выбора outbound-интерфейса при создании NAT правила.
   * @returns {{ interfaces: Array<{name: string}> }}
   */
  async getNatInterfaces() {
    return this.call({
      method: 'get',
      path: '/nat/interfaces',
    });
  }

  /**
   * Получить список NAT правил.
   * @returns {{ rules: Array<object> }}
   */
  async getNatRules() {
    return this.call({
      method: 'get',
      path: '/nat/rules',
    });
  }

  /**
   * Создать новое NAT правило.
   * @param {object} data - { name, source, outInterface, type, toSource, comment }
   * @returns {{ rule: object }}
   */
  async createNatRule(data) {
    return this.call({
      method: 'post',
      path: '/nat/rules',
      body: data,
    });
  }

  /**
   * Обновить NAT правило (полное обновление полей).
   * @param {{ ruleId: string, name, source, outInterface, type, toSource, comment }}
   * @returns {{ rule: object }}
   */
  async updateNatRule({ ruleId, ...updates }) {
    return this.call({
      method: 'patch',
      path: `/nat/rules/${ruleId}`,
      body: updates,
    });
  }

  /**
   * Включить / выключить NAT правило (toggle).
   * @param {{ ruleId: string, enabled: boolean }}
   * @returns {{ rule: object }}
   */
  async toggleNatRule({ ruleId, enabled }) {
    return this.call({
      method: 'patch',
      path: `/nat/rules/${ruleId}`,
      body: { enabled },
    });
  }

  /**
   * Удалить NAT правило.
   * @param {{ ruleId: string }}
   */
  async deleteNatRule({ ruleId }) {
    return this.call({
      method: 'delete',
      path: `/nat/rules/${ruleId}`,
    });
  }

  // ============================================================
  // Aliases API — Firewall Aliases (host / network / ipset)
  // ============================================================

  /**
   * Получить список всех алиасов.
   * @returns {{ aliases: Array<object> }}
   */
  async getAliases() {
    return this.call({ method: 'get', path: '/aliases' });
  }

  async getClientGroups() {
    return this.call({ method: 'get', path: '/aliases/client-groups' });
  }

  async createClientGroup(data) {
    return this.call({ method: 'post', path: '/aliases', body: { ...data, type: 'client-group' } });
  }

  async deleteClientGroup({ id }) {
    return this.call({ method: 'delete', path: `/aliases/${id}` });
  }

  /**
   * Создать новый алиас.
   * @param {{ name, type, entries?, description? }} data
   * @returns {{ alias: object }}
   */
  async createAlias(data) {
    return this.call({ method: 'post', path: '/aliases', body: data });
  }

  /**
   * Обновить алиас.
   * @param {{ id: string, name?, description?, entries? }}
   * @returns {{ alias: object }}
   */
  async updateAlias({ id, ...updates }) {
    return this.call({ method: 'patch', path: `/aliases/${id}`, body: updates });
  }

  /**
   * Удалить алиас (для ipset — уничтожает kernel set).
   * @param {{ id: string }}
   */
  async deleteAlias({ id }) {
    return this.call({ method: 'delete', path: `/aliases/${id}` });
  }

  /**
   * Загрузить префиксы из txt-файла в ipset-алиас.
   * @param {{ id: string, text: string }}  — text — содержимое файла (один CIDR на строку)
   * @returns {{ alias: object }}
   */
  async uploadAliasFile({ id, text }) {
    return this.call({ method: 'post', path: `/aliases/${id}/upload`, body: { text } });
  }

  async getAliasEntries({ id }) {
    return this.call({ method: 'get', path: `/aliases/${id}/entries` });
  }

  /**
   * Запустить генерацию ipset через PrefixFetcher (async job).
   * @param {{ id: string, country?, asn?, asnList? }}
   * @returns {{ jobId: string }}
   */
  async generateAlias({ id, country, asn, asnList }) {
    return this.call({
      method: 'post',
      path: `/aliases/${id}/generate`,
      body: { country, asn, asnList },
    });
  }

  /**
   * Получить статус generation job.
   * @param {{ id: string, jobId: string }}
   * @returns {{ status: 'running'|'done'|'error', entryCount?, error? }}
   */
  async getAliasJobStatus({ id, jobId }) {
    return this.call({ method: 'get', path: `/aliases/${id}/generate/${jobId}` });
  }

  // ============================================================
  // Firewall Rules API  (Firewall → Rules, поглощает PBR)
  // ============================================================

  /** Список сетевых интерфейсов хоста (для дропдауна Interface). */
  async getFirewallInterfaces() {
    return this.call({ method: 'get', path: '/firewall/interfaces' });
  }

  /** Список всех firewall правил (sorted by order). */
  async getFirewallRules() {
    return this.call({ method: 'get', path: '/firewall/rules' });
  }

  /**
   * Создать firewall правило.
   * @param {{ name, interface?, protocol?, source, destination, action, gatewayId?, gatewayGroupId?, log?, comment? }} data
   */
  async createFirewallRule(data) {
    return this.call({ method: 'post', path: '/firewall/rules', body: data });
  }

  /**
   * Обновить firewall правило (полные данные).
   * @param {{ id: string, ...updates }}
   */
  async updateFirewallRule({ id, ...updates }) {
    return this.call({ method: 'patch', path: `/firewall/rules/${id}`, body: updates });
  }

  /**
   * Включить / выключить firewall правило.
   * @param {{ id: string, enabled: boolean }}
   */
  async toggleFirewallRule({ id, enabled }) {
    return this.call({ method: 'patch', path: `/firewall/rules/${id}`, body: { enabled } });
  }

  /**
   * Удалить firewall правило.
   * @param {{ id: string }}
   */
  async deleteFirewallRule({ id }) {
    return this.call({ method: 'delete', path: `/firewall/rules/${id}` });
  }

  /**
   * Переместить правило вверх или вниз.
   * @param {{ id: string, direction: 'up'|'down' }}
   */
  async moveFirewallRule({ id, direction }) {
    return this.call({ method: 'post', path: `/firewall/rules/${id}/move`, body: { direction } });
  }

  async reorderFirewallRules(ids) {
    return this.call({ method: 'post', path: '/firewall/reorder', body: { ids } });
  }

  async getFirewallPending() {
    return this.call({ method: 'get', path: '/firewall/pending' });
  }

  async applyFirewallRules() {
    return this.call({ method: 'post', path: '/firewall/apply' });
  }

  async discardFirewallChanges() {
    return this.call({ method: 'post', path: '/firewall/discard' });
  }

  // ============================================================
  // Users API — multi-user management
  // ============================================================

  /** List all users. */
  async getUsers() {
    return this.call({ method: 'get', path: '/users' });
  }

  /** Create a new user. */
  async createUser({ username, password }) {
    return this.call({ method: 'post', path: '/users', body: { username, password } });
  }

  /** Update a user's username or password. */
  async updateUser(id, updates) {
    return this.call({ method: 'patch', path: `/users/${id}`, body: updates });
  }

  /** Delete a user by ID. */
  async deleteUser(id) {
    return this.call({ method: 'delete', path: `/users/${id}` });
  }

  /** Grant or revoke admin role for a user. */
  async setUserAdmin(id, admin) {
    return this.call({ method: 'post', path: `/users/${id}/set-admin`, body: { admin } });
  }

  /** Get the currently authenticated user. */
  async getCurrentUser() {
    return this.call({ method: 'get', path: '/users/me' });
  }

  /** Update own password. */
  async updateCurrentUser(updates) {
    return this.call({ method: 'patch', path: '/users/me', body: updates });
  }

  // ============================================================
  // TOTP API
  // ============================================================

  /** Start TOTP setup — returns { secret, qr_uri, qr_png }. */
  async getTOTPSetup() {
    return this.call({ method: 'get', path: '/users/me/totp/setup' });
  }

  /** Confirm TOTP setup with a 6-digit code. */
  async enableTOTP({ code }) {
    return this.call({ method: 'post', path: '/users/me/totp/enable', body: { code } });
  }

  /** Disable TOTP — requires current TOTP code for confirmation. */
  async disableTOTP({ code }) {
    return this.call({ method: 'post', path: '/users/me/totp/disable', body: { code } });
  }

  /** Verify TOTP code during login (step 2 after password). */
  async verifyTOTP({ code }) {
    return this.call({ method: 'post', path: '/auth/totp/verify', body: { code } });
  }

  // ============================================================
  // API Tokens — programmatic access
  // ============================================================

  /** List all API tokens for the current user. */
  async getApiTokens() {
    return this.call({ method: 'get', path: '/tokens' });
  }

  /**
   * Create a new API token.
   * @param {{ name: string }} data
   * @returns {{ token: object, raw_token: string }}
   * raw_token is shown ONCE — save it, it cannot be retrieved later.
   */
  async createApiToken({ name }) {
    return this.call({ method: 'post', path: '/tokens', body: { name } });
  }

  /**
   * Revoke (delete) an API token.
   * @param {{ id: string }}
   */
  async deleteApiToken({ id }) {
    return this.call({ method: 'delete', path: `/tokens/${id}` });
  }

  _systemApiBase() {
    const segs = window.location.pathname.split('/').filter(Boolean);
    return segs.length > 0
      ? `${window.location.origin}/${segs[0]}/api/system`
      : `${window.location.origin}/api/system`;
  }

  /**
   * Download full system backup. If password provided, file is AES-256-GCM encrypted.
   * Returns a Blob so the caller can trigger a file download.
   * @param {{ password?: string }}
   */
  async downloadSystemBackup({ password = '', includeMetrics = false } = {}) {
    const res = await fetch(`${this._systemApiBase()}/backup`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ password, includeMetrics }),
    });
    if (!res.ok) {
      const err = await res.json().catch(() => ({ message: res.statusText }));
      throw new Error(err.message || res.statusText);
    }
    // Extract filename from Content-Disposition header.
    const cd = res.headers.get('Content-Disposition') || '';
    const match = cd.match(/filename[^;=\n]*=(['"]?)([^'"\n]+)\1/);
    const filename = match ? match[2] : (password ? 'cascade-backup.tar.gz.enc' : 'cascade-backup.tar.gz');
    const blob = await res.blob();
    return { blob, filename };
  }

  /**
   * Preview a backup file — returns physical interface names found in backup NAT rules
   * vs current server interfaces. Used to detect remapping needs before applying.
   * @param {{ file: File, password?: string }}
   */
  async previewSystemRestore({ file, password = '' }) {
    const form = new FormData();
    form.append('backup', file);
    if (password) form.append('password', password);
    const res = await fetch(`${this._systemApiBase()}/restore/preview`, { method: 'POST', body: form });
    if (!res.ok) {
      const err = await res.json().catch(() => ({ message: res.statusText }));
      throw new Error(err.message || res.statusText);
    }
    return res.json();
  }

  /**
   * Restore from a backup file (plain .tar.gz or encrypted .tar.gz.enc).
   * @param {{ file: File, password?: string, ifaceMap?: Object }}
   */
  async restoreSystemBackup({ file, password = '', ifaceMap = null }) {
    const form = new FormData();
    form.append('backup', file);
    if (password) form.append('password', password);
    if (ifaceMap && Object.keys(ifaceMap).length > 0) form.append('ifaceMap', JSON.stringify(ifaceMap));
    const res = await fetch(`${this._systemApiBase()}/restore`, { method: 'POST', body: form });
    if (!res.ok) {
      const err = await res.json().catch(() => ({ message: res.statusText }));
      throw new Error(err.message || res.statusText);
    }
    return res.json();
  }

  /**
   * List pre-restore auto-backups saved on the server.
   */
  async listSystemBackups() {
    return this.call({ method: 'get', path: '/system/backups' });
  }

  // ── Remote servers ──────────────────────────────────────────────────────────

  async getRemotes() {
    return this.call({ method: 'get', path: '/remotes/' });
  }

  /**
   * Add a remote server. Cascade will login with the credentials, obtain an API
   * token and store it. The password is never persisted.
   * @param {{ name: string, url: string, username: string, password: string }}
   */
  async addRemote({ name, url, username, password, totpCode, token, skipTlsVerify }) {
    const body = { name, url };
    if (skipTlsVerify) body.skipTlsVerify = true;
    if (token) {
      // Explicit-token mode — server validates and stores the token directly.
      body.token = token;
    } else {
      // Login mode — server logs in to obtain a token.
      body.username = username;
      body.password = password;
      if (totpCode) body.totpCode = totpCode;
    }
    // 422 = totp_required — not an error, returned as data to the caller.
    return this.call({ method: 'post', path: '/remotes/', body, allowStatus: [422] });
  }

  async deleteRemote({ id }) {
    return this.call({ method: 'delete', path: `/remotes/${id}` });
  }

  async testRemote({ id }) {
    return this.call({ method: 'post', path: `/remotes/${id}/test` });
  }

  /**
   * Call an API endpoint on a remote server via the proxy.
   * @param {{ remoteId: string, method: string, path: string, body?: any }}
   */
  async remoteCall({ remoteId, method, path, body }) {
    return this.call({ method, path: `/remotes/${remoteId}/proxy${path}`, body });
  }

  // ── Diagnostics ────────────────────────────────────────────────────────────

  // Ping host from the server that receives this call (local or via proxy).
  async ping({ host, count = 3, remoteId } = {}) {
    const body = { host, count };
    const path = '/diagnostics/ping';
    return remoteId
      ? this.remoteCall({ remoteId, method: 'post', path, body })
      : this.call({ method: 'post', path, body });
  }

  // ── Speed test ─────────────────────────────────────────────────────────────

  async speedtestRun(body) {
    return this.callLocal({ method: 'post', path: '/speedtest/run', body });
  }

  async speedtestGetResult(jobId) {
    return this.callLocal({ method: 'get', path: `/speedtest/result/${jobId}` });
  }

  async speedtestListResults() {
    return this.callLocal({ method: 'get', path: '/speedtest/results' });
  }

  async speedtestClearResults() {
    return this.callLocal({ method: 'delete', path: '/speedtest/results' });
  }

  // ── Metrics ────────────────────────────────────────────────────────────────

  async getMetrics() {
    return this.call({ method: 'get', path: '/metrics/' });
  }

  async getMetricsHistory({ key, period }) {
    return this.call({ method: 'get', path: `/metrics/history?key=${encodeURIComponent(key)}&period=${period}` });
  }

  async getMetricsGatewayDist({ key, period }) {
    return this.call({ method: 'get', path: `/metrics/gateway-dist?key=${encodeURIComponent(key)}&period=${period}` });
  }

}
