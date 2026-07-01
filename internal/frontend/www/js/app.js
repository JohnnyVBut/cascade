/* eslint-disable no-console */
/* eslint-disable no-undef */
/* eslint-disable no-new */

'use strict';

function bytes(bytes, decimals, kib, maxunit) {
  kib = kib || false;
  if (bytes === 0) return '0 B';
  if (Number.isNaN(parseFloat(bytes)) && !Number.isFinite(bytes)) return 'NaN';
  const k = kib ? 1024 : 1000;
  const dm = decimals != null && !Number.isNaN(decimals) && decimals >= 0 ? decimals : 2;
  const sizes = kib
    ? ['B', 'KiB', 'MiB', 'GiB', 'TiB', 'PiB', 'EiB', 'ZiB', 'YiB', 'BiB']
    : ['B', 'KB', 'MB', 'GB', 'TB', 'PB', 'EB', 'ZB', 'YB', 'BB'];
  let i = Math.floor(Math.log(bytes) / Math.log(k));
  if (maxunit !== undefined) {
    const index = sizes.indexOf(maxunit);
    if (index !== -1) i = index;
  }
  // eslint-disable-next-line no-restricted-properties
  return `${parseFloat((bytes / Math.pow(k, i)).toFixed(dm))} ${sizes[i]}`;
}

/**
 * Sorts an array of objects by a specified property in ascending or descending order.
 *
 * @param {Array} array - The array of objects to be sorted.
 * @param {string} property - The property to sort the array by.
 * @param {boolean} [sort=true] - Whether to sort the array in ascending (default) or descending order.
 * @return {Array} - The sorted array of objects.
 */
function sortByProperty(array, property, sort = true) {
  if (sort) {
    return array.sort((a, b) => (typeof a[property] === 'string' ? a[property].localeCompare(b[property]) : a[property] - b[property]));
  }

  return array.sort((a, b) => (typeof a[property] === 'string' ? b[property].localeCompare(a[property]) : b[property] - a[property]));
}

const i18n = new VueI18n({
  locale: localStorage.getItem('lang') || 'en',
  fallbackLocale: 'en',
  messages,
});

const UI_CHART_TYPES = [
  { type: false, strokeWidth: 0 },
  { type: 'line', strokeWidth: 3 },
  { type: 'area', strokeWidth: 0 },
  { type: 'bar', strokeWidth: 0 },
];

const CHART_COLORS = {
  rx: { light: 'rgba(128,128,128,0.3)', dark: 'rgba(255,255,255,0.3)' },
  tx: { light: 'rgba(128,128,128,0.4)', dark: 'rgba(255,255,255,0.3)' },
  gradient: { light: ['rgba(0,0,0,1.0)', 'rgba(0,0,0,1.0)'], dark: ['rgba(128,128,128,0)', 'rgba(128,128,128,0)'] },
};

new Vue({
  el: '#app',
  components: {
    apexchart: VueApexCharts,
  },
  i18n,
  data: {
    authenticated: null,
    authenticating: false,
    versionInfo: null,       // populated by loadVersionInfo() — version + update status
    updateBannerDismissed: false, // hides update banner until next loadVersionInfo() call
    updateChecking: false,        // spinner state for "Check for updates" button
    username: 'admin',     // login form username field
    password: null,
    requiresPassword: null,
    remember: false,
    rememberMeEnabled: false,

    // TOTP login step 2
    totpCode: '',          // 6-digit code input during login
    totpRequired: false,   // true = password OK, show TOTP input
    totpPending: false,    // waiting for TOTP verification

    // Users management
    users: [],
    currentUser: null,

    // TOTP setup modal (for /users/me/totp/setup flow)
    showTOTPSetupModal: false,
    totpSetupSecret: '',
    totpSetupQrPng: '',
    totpSetupQrUri: '',
    totpSetupCode: '',
    totpSetupSaving: false,

    // Disable TOTP modal
    showTOTPDisableModal: false,
    totpDisableCode: '',

    // First-run setup (open mode — no users yet)
    showFirstRunSetup: false,
    firstRunForm: { username: 'admin', password: '', passwordConfirm: '' },
    firstRunSaving: false,

    // Add user modal
    showAddUserModal: false,
    addUserForm: { username: '', password: '', passwordConfirm: '' },

    // API Tokens
    apiTokens: [],
    showCreateTokenModal: false,
    createTokenForm: { name: '' },
    showNewTokenModal: false,   // shown after successful creation
    newTokenValue: '',          // raw token — displayed once, never stored

    clients: null,
    clientsPersist: {},
    clientDelete: null,
    clientCreate: null,
    clientCreateName: '',
    clientExpiredDate: '',
    clientEditName: null,
    clientEditNameId: null,
    clientEditAddress: null,
    clientEditAddressId: null,
    clientEditExpireDate: null,
    clientEditExpireDateId: null,
    qrcode: null,

    currentRelease: null,
    latestRelease: null,

    uiTrafficStats: false,

    uiChartType: 0,
    avatarSettings: {
      'dicebear': null,
      'gravatar': false,
    },
    enableOneTimeLinks: false,
    enableSortClient: false,
    sortClient: true, // Sort clients by name, true = asc, false = desc
    enableExpireTime: false,

    // Sidebar navigation
    activePage: 'dashboard', // 'dashboard' | 'interfaces' | 'gateways' | 'routing' | 'firewall' | 'settings' | 'administration'
    activeInterfaceId: null,  // ID выбранного интерфейса (вкладка)
    hoverPage: null,          // для hover-эффекта в sidebar
    sidebarMenu: [
      { id: 'dashboard',        label: 'Dashboard' },
      { id: 'interfaces',       label: 'Interfaces' },
      { id: 'gateways',         label: 'Gateways' },
      { id: 'routing',          label: 'Routing' },
      { id: 'nat',              label: 'NAT' },
      { id: '_header_firewall', label: 'Firewall', type: 'header' },
      { id: 'firewall-aliases', label: 'Aliases' },
      { id: 'firewall',         label: 'Rules' },
      { id: 'diagnostics',      label: 'Diagnostics' },
      { id: 'remotes',          label: 'Remotes' },
      { id: 'settings',         label: 'Settings' },
      { id: 'administration',   label: 'Administration' },
      { id: '_header_wizards',  label: 'Wizards', type: 'header' },
      { id: 'wizard-simple-vpn', label: 'Simple Client VPN' },
      { id: 'wizard-uplink-vpn', label: 'Cascade via WireGuard Uplink' },
      { id: 'wizard-cascade-s2s', label: 'Cascade ↔ Cascade S2S' },
    ],

    // ── Dashboard ──────────────────────────────────────────────────────────────
    dashWidgets: [],          // [{id, type, x, y, w, h}]
    dashGrid: null,           // GridStack instance
    dashShowAddMenu: false,
    dashSystemInfo: null,
    dashAvailableWidgetTypes: [
      { type: 'server-info',    label: 'Server Info',     icon: '🖥️' },
      { type: 'interfaces',     label: 'Interfaces',      icon: '🔌' },
      { type: 'gateways',       label: 'Gateways',        icon: '📡' },
      { type: 'peers-summary',  label: 'Peers Summary',   icon: '👥' },
      { type: 'peers',          label: 'Peers',           icon: '🔗' },
      { type: 'nat',            label: 'NAT',             icon: '🔀' },
      { type: 'traffic',        label: 'Traffic',         icon: '📊' },
      { type: 'monitoring',     label: 'Monitoring',      icon: '📈' },
    ],
    dashPeersState: {},   // per-widget: { [widgetId]: { iface: '', sort: 'name' } }

    // ── Monitoring widget ──────────────────────────────────────────────────────
    metricsSnapshot: null,          // latest GET /api/metrics response
    metricsAvailableKeys: [],       // all keys (populated on first snapshot)
    metricsHistory: {},             // { [widgetId+key]: [{x,y}] } rolling buffer
    metricsGatewayDist: {},         // { [widgetId+key]: [[ts_ms, healthy, degraded, down, adminDown]] }
    metricsGatewaySeriesCache: {},  // { [widgetId+key]: [series] } — stable refs so ApexCharts skips re-render
    metricsAreaSeriesCache: {},     // { [widgetId+key]: [{name, data:[]}] } — stable refs for area charts
    metricsPoller: null,            // setInterval handle
    metricsConfigWidget: null,      // widget being configured (modal open)
    metricsConfigPage: 'dashboard', // page the config modal was opened from
    metricsConfigDraft: [],         // draft graphs[] for config modal
    metricsColorDraft: {},          // { [key]: '#rrggbb' } for config modal
    metricsTitleDraft: '',          // draft widget title for config modal
    metricsWidgetPeriod: {},        // { [widgetId]: '5m'|'1h'|... }

    // ── Diagnostics page ──────────────────────────────────────────────────────
    diagWidgets: [],
    diagGrid: null,
    diagActiveTab: 'graphs', // 'graphs' | 'utilities'

    // ── Ping utility ──────────────────────────────────────────────────────────
    pingHost: '',
    pingSource: '',
    pingCount: 5,
    pingSize: '',
    pingDf: false,
    pingTos: '',
    pingRunning: false,
    pingEventSource: null,
    pingRouterIfaces: [],
    terminalLines: [], // shared output terminal for all Utilities

    // ── Traceroute utility ────────────────────────────────────────────────────
    traceHost: '',
    traceSource: '',
    traceType: 'udp',
    traceRunning: false,
    traceEventSource: null,

    // ── Tcpdump utility ───────────────────────────────────────────────────────
    tcpdumpIface: '',
    tcpdumpFilter: '',
    tcpdumpSave: false,
    tcpdumpRunning: false,
    tcpdumpEventSource: null,
    tcpdumpPcapId: null,        // capture ID sent as first SSE event [captureid:<id>]
    tcpdumpDownloadReady: false, // true once file is finalized and ready to download

    // Tunnel Interfaces
    tunnelInterfaces: [],
    selectedInterface: null,
    selectedInterfacePeers: [],
    allPeers: [],            // dashboard: flat list of peers from all interfaces
    showInterfaceCreate: false,
    createMode: 'quick',        // 'quick' | 'manual' — controls which form is shown in the create modal
    importConfForm: { name: '', conf: '', fileName: '' },
    importConfMode: 'server', // 'server' (client hub) | 'uplink' (S2S)
    importConfWarning: '',
    showImportBackup: false,
    importBackupTab: 'cascade', // 'cascade' | 'awgeasy'
    importBackupForm: { json: '', listenPort: '', fileName: '' },
    showExportInterface: false,
    exportInterfaceId: null,
    exportInterfaceIncludePeers: true,
    showInterfaceEdit: false,
    interfaceEdit: {
      id: null,
      name: '',
      address: '',
      listenPort: '',
      disableRoutes: false,
      natDisabled: false,
      dns: '',
      publicHost: '',
      mtu: 0,
      mss: 0,
      kernelMtu: 0,
      protocol: 'wireguard-1.0',
      selectedTemplateId: '',
      settings: {
        jc: 6, jmin: 10, jmax: 50,
        s1: 64, s2: 67, s3: 64, s4: 4,
        h1: '', h2: '', h3: '', h4: '',
        i1: '', i2: '', i3: '', i4: '', i5: '',
      },
    },
    showPeerCreate: false, // manual peer create modal
    showQuickPeerCreate: false, // quick peer create dialog
    loadingInterfaceId: null,
    interfaceCreate: {
      name: '',
      protocol: 'wireguard-1.0',
      address: '',
      listenPort: '',
      disableRoutes: false,
      dns: '',
      selectedTemplateId: '',   // UI-only: выбранный профиль обфускации (не отправляется в API)
      settings: {
        jc: 6, jmin: 10, jmax: 50,
        s1: 64, s2: 67, s3: 64, s4: 4,
        h1: '', h2: '', h3: '', h4: '',
        i1: '', i2: '', i3: '', i4: '', i5: '',
      },
    },
    peerCreate: {
      mode: 'generate',     // 'generate' | 'manual'
      peerType: 'client',   // 'client' | 'interconnect'
      name: '',
      publicKey: '',
      endpoint: '',
      allowedIPs: '',
      clientAllowedIPs: '',
      persistentKeepalive: 25,
      groupId: '',          // client-group alias ID
      expiredAt: '',        // YYYY-MM-DD or empty
      showQR: false,
    },

    // Quick peer create (one-click)
    peerCreateName: '',
    peerCreateShowQR: false,
    inlineGroupInput: '',        // inline new-group name input value
    inlineGroupShow: false,      // show inline input in Manual Create modal
    inlineGroupShowQuick: false, // show inline input in Quick Create modal
    peerCreateExpiredDate: '',
    peerCreateGroupId: '',    // selected client-group for quick create

    // Peer management (inline editing, admin-tunnel style)
    peersPersist: {},
    peerDelete: null,      // peer for delete confirmation modal
    interfaceDelete: null, // interface for delete confirmation modal
    peerEditNameId: null,
    peerEditName: null,
    peerEditAddressId: null,
    peerEditAddress: null,
    peerEditExpireDateId: null,
    peerEditExpireDate: null,
    // Peer Edit Modal
    showPeerEditModal: false,
    peerEditForm: {
      _peer: null,          // original peer reference (for type checks + API call)
      name: '',
      persistentKeepalive: 0,
      endpoint: '',         // interconnect only
      allowedIPs: '',       // interconnect only (editable)
      clientAllowedIPs: '', // client only
      rateDown: 0,          // kbps, 0 = unlimited
      rateUp: 0,            // kbps, 0 = unlimited
      groupId: '',          // client-group alias ID
      expiredAt: '',        // YYYY-MM-DD or '' = no expiry
    },
    // Settings
    globalSettings: {
      dns: '1.1.1.1, 8.8.8.8',
      defaultPersistentKeepalive: 25,
      defaultClientAllowedIPs: '0.0.0.0/0, ::/0',
      subnetPool:       '192.168.0.0/16',
      portPool:         '51831-65535',
      defaultFwPolicy:  'accept',
      gatewayWindowSeconds:     30,
      gatewayHealthyThreshold:  95,
      gatewayDegradedThreshold: 90,
      // Router identity
      routerName:     '',
      publicIPMode:   'auto',
      publicIPManual: '',
      // MTU for client configs (0 = auto)
      mtu: 0,
      // Expired peer policy
      expiredPeerPolicy:   'disable',  // "disable" | "restrict"
      expiredPeerRateDown: 0,          // kbps downstream; 0 = no limit
      expiredPeerRateUp:   0,          // kbps upstream;   0 = no limit
      expiredPeerGroupId:  '',         // client-group alias ID; '' = don't move
      // UI preferences
      lang: 'en',
      // Runtime-only (returned by GET, not sent in PUT)
      hostname:         '',
      resolvedPublicIP: '',
      publicIPWarning:  '',
    },
    settingsSaved: false,
    templates: [],
    showTemplateModal: false,
    templateEditTarget: null, // null = create, object = edit
    templateForm: {
      name: '',
      isDefault: false,
      host: '',
      jc: 6, jmin: 10, jmax: 50,
      s1: 64, s2: 67, s3: 64, s4: 4,
      h1: '', h2: '', h3: '', h4: '',
      i1: '', i2: '', i3: '', i4: '', i5: '',
    },

    // Generate AWG2 modal
    showGenerateModal: false,
    generateForm: {
      profile: 'random',
      intensity: 'medium',
      host: '',
      browser: '',
      saveName: '',
    },
    generatedParams: null,
    generatingParams: false,
    generateProfiles: [],

    // Gateways
    gateways: [],
    gatewayGroups: [],
    systemInterfaces: [],

    // ── Remote servers ──────────────────────────────────────────────────────
    remotes: [],
    activeRemoteId: null,   // null = local server; string = remote id
    localServerName: '',    // name of the local server, set once on login and never overwritten by remote settings
    showRemoteAdd: false,
    remoteAddForm: { name: '', url: '', mode: 'login', username: '', password: '', totpCode: '', token: '', skipTlsVerify: false },
    remoteAddError: '',
    remoteAddLoading: false,
    remoteAddNeedsTOTP: false,
    remoteTesting: {},   // { [id]: true } while test is in progress
    remoteTestResult: {}, // { [id]: 'ok' | 'error' }

    // ── Speed test ────────────────────────────────────────────────────────────
    showSpeedtest: false,
    speedtest: {
      fromId: '__local__',
      toId: '__local__',
      via: 'auto',      // 'auto' | 'internet' | 'tunnel'
      tunnelIp: '',     // manual override when via='tunnel'
      duration: 10,
      streams: 4,
    },
    speedtestDetectedTunnelIp: '',
    speedtestFromIfaces: [],   // active interfaces on "from" server (manual mode)
    speedtestToIfaces: [],     // active interfaces on "to" server (manual mode)
    speedtestFromIfaceId: '',  // selected interface id on "from" (manual mode)
    speedtestToIfaceId: '',    // selected interface id on "to" (manual mode, bind addr for iperf3 client)
    speedtestPingConfirm: false,     // show ping-failed confirmation
    speedtestPingConfirmMsg: '',     // message shown in confirmation
    speedtestPendingHost: '',        // host waiting for confirmation
    speedtestPendingVia: '',
    speedtestRunning: false,
    speedtestResult: null,
    speedtestError: '',
    speedtestHistory: [],

    wizardsExpanded: false,


    // ── Wizard: Simple Client VPN ─────────────────────────────────────────────
    wizardVPN: {
      step: 1,                // 1=protocol, 2=name, 3=dns+peer, 4=result
      protocol: 'wireguard',  // 'wireguard' | 'amneziawg'
      ifaceName: '',
      dns: '',
      peerName: 'My Device',
      running: false,
      error: '',
      ifaceId: '',
      peerId: '',
      qrUrl: '',
      ifaceAddr: '',
      ifacePort: 0,
    },

    wizardUplink: {
      step: 1,          // 1=import conf, 2=source, 3=destination, 4=options, 5=apply
      // step 1
      confText: '',
      confFileName: '',
      preview: null,    // parsed preview from /parse-conf
      ifaceName: '',
      // step 2
      selectedIfaceIds: [],
      createSrcAlias: true,
      srcAliasName: '',
      // inline create interface mini-form
      showInlineCreate: false,
      inlineIfaceName: '',
      inlineIfaceAddr: '',
      inlineIfacePort: '',
      inlineIfaceProto: 'wireguard',
      inlineIfaceCreating: false,
      // step 3
      dstType: 'all',   // 'all' | 'geo' | 'as'
      dstCountries: [],
      dstASN: '',
      dstNegate: false,
      dstAliasName: '',
      // step 4
      mssClamp: true,
      fallback: 'drop', // 'drop' | 'allow'
      gatewayName: '',
      fwRuleName: '',
      natRuleName: '',
      gatewayMonitorIP: '',
      // step 5 — apply state
      applying: false,
      steps: [],        // [{ label, status:'pending'|'running'|'ok'|'warn'|'error', detail }]
      // created object IDs for final screen
      createdIfaceId: '',
      createdSrcAliasId: '',
      createdDstAliasId: '',
      createdGatewayId: '',
      createdFwRuleId: '',
      createdNatRuleId: '',
      done: false,
      fatalError: '',
    },

    // ── Wizard: Cascade ↔ Cascade S2S ────────────────────────────────────────
    wizardS2S: {
      step: 1,           // 1=remote, 2=source, 3=destination, 4=options, 5=apply
      // step 1 — remote
      remotes: [],
      remoteId: '',
      showAddRemote: false,
      addRemoteName: '', addRemoteURL: '', addRemoteMode: 'password',
      addRemoteUser: '', addRemotePass: '', addRemoteToken: '',
      addRemoteSkipTLS: false, addRemoteLoading: false,
      // step 2 — source
      selectedIfaceIds: [],
      createSrcAlias: true,
      srcAliasName: '',
      // step 3 — destination
      dstType: 'all',
      dstCountries: [],
      dstASN: '',
      dstNegate: false,
      dstAliasName: '',
      // step 4 — options
      protocol: 'wireguard',
      mssClamp: true,
      fallback: 'drop',
      localIfaceName: '',
      remoteIfaceName: '',
      gatewayName: '',
      fwRuleName: '',
      // step 5 — apply
      applying: false,
      steps: [],
      createdLocalIfaceId: '',
      createdRemoteIfaceId: '',
      createdSrcAliasId: '',
      createdDstAliasId: '',
      createdGatewayId: '',
      createdFwRuleId: '',
      done: false,
      fatalError: '',
    },

    showGatewayCreate: false,
    showGatewayEdit:   false,
    showGroupCreate:   false,
    showGroupEdit:     false,
    gatewayCreate: {
      name: '',
      interface: '',
      gatewayIP: '',
      monitorAddress: '',
      monitor: true,
      monitorInterval: 5,
      windowSeconds: null,
      latencyThreshold: 500,
      monitorHttp: { enabled: false, url: '', expectedStatus: 200, interval: 10, timeout: 5 },
      monitorRule: 'icmp_only',
      description: '',
    },
    gatewayEdit: {
      id: null,
      name: '',
      interface: '',
      gatewayIP: '',
      monitorAddress: '',
      monitor: true,
      monitorInterval: 5,
      windowSeconds: null,
      latencyThreshold: 500,
      monitorHttp: { enabled: false, url: '', expectedStatus: 200, interval: 10, timeout: 5 },
      monitorRule: 'icmp_only',
      description: '',
    },
    groupCreate: { name: '', trigger: 'packetloss', description: '', gateways: [] },
    groupEdit:   { id: null, name: '', trigger: 'packetloss', description: '', gateways: [] },

    // Routing page
    activeRoutingTab: 'status',   // 'status' | 'static' | 'policy' | 'ospf'
    routingTable: 'main',         // таблица для Status tab
    routingTables: [],            // список таблиц из /etc/iproute2/rt_tables
    kernelRoutes: [],
    kernelRoutesError: '',
    kernelRoutesLoading: false,
    staticRoutes: [],
    routeTestIp: '',
    routeTestSrc: '',          // source IP (опционально) — запускает policy trace
    routeTestResult: null,
    routeTestMatchedRule: null, // { id, name, fwmark } | null
    routeTestSteps: [],         // шаги trace для отладки
    routeTestLoading: false,
    routeTestError: '',
    showRouteCreate: false,
    routeCreate: {
      description: '',
      destination: '',
      viaMode: 'manual',    // 'manual' | 'gateway' | 'group'
      gateway: '',          // manual next-hop IP
      dev: '',              // manual interface
      gatewayId: '',        // linked Gateway ID (viaMode=gateway)
      gatewayGroupId: '',   // linked GatewayGroup ID (viaMode=group)
      metric: '',
      table: 'main',
    },
    showRouteEdit: false,
    routeEdit: {
      id: '',
      description: '',
      destination: '',
      viaMode: 'manual',
      gateway: '',
      dev: '',
      gatewayId: '',
      gatewayGroupId: '',
      metric: '',
      table: 'main',
    },

    // NAT page
    activeNatTab: 'outbound',     // 'outbound' | 'portforward'
    natRules: [],                 // список NAT правил
    natInterfaces: [],            // список сетевых интерфейсов хоста
    natRulesLoading: false,
    // Port Forwarding (DNAT)
    dnatRules: [],
    dnatLoading: false,
    showDnatModal: false,
    dnatEditMode: false,
    dnatForm: { id: '', name: '', protocol: 'udp', inInterface: '', inPort: '', dest: '', destPort: '', masquerade: true, comment: '' },
    showNatRuleCreate: false,     // модал создания правила
    showNatRuleEdit: false,       // модал редактирования правила
    natRuleCreate: {
      name: '',
      sourceType: 'any',          // 'any' | 'subnet' | 'ip' | 'alias'
      sourceValue: '',            // значение при sourceType subnet/ip
      sourceAliasId: '',          // alias id при sourceType === 'alias'
      outInterface: '',
      type: 'MASQUERADE',         // 'MASQUERADE' | 'SNAT'
      toSource: '',               // целевой IP при type === 'SNAT'
      comment: '',
    },
    natRuleEdit: {
      id: null,
      name: '',
      sourceType: 'any',
      sourceValue: '',
      sourceAliasId: '',
      outInterface: '',
      type: 'MASQUERADE',
      toSource: '',
      comment: '',
    },

    // Firewall Aliases page
    aliases: [],
    aliasesLoading: false,
    clientGroups: [],            // aliases of type client-group
    showAliasCreate: false,
    showAliasEdit: false,
    aliasCreate: {
      name: '',
      description: '',
      type: 'network',          // 'host' | 'network' | 'ipset' | 'group' | 'port' | 'port-group'
      entries: '',              // textarea: one entry per line (host/network/port)
      ipsetEntries: '',         // textarea: manual CIDRs for ipset type
      memberIds: [],            // для group/port-group: выбранные UUID members
      genSource: 'country',     // 'country' | 'asn' | 'asn-list'
      genCountry: '',
      genAsn: '',
      genAsnList: '',
      file: null,               // optional CIDR file for ipset (uploaded immediately after create)
      rateDown: 0,              // kbps; 0 = unlimited (client-group only)
      rateUp: 0,
    },
    aliasEdit: {
      id: null,
      name: '',
      description: '',
      type: 'network',
      entries: '',
      ipsetEntries: '',         // manual CIDRs for ipset type (pre-populated for small sets)
      ipsetEntriesLoading: false,
      ipsetEntriesLoaded: false,
      memberIds: [],            // для group/port-group: выбранные UUID members
      genSource: 'country',
      genCountry: '',
      genAsn: '',
      genAsnList: '',
      rateDown: 0,              // kbps; 0 = unlimited (client-group only)
      rateUp: 0,
    },
    // Country picker combobox state (shared — only one modal open at a time)
    countrySearch: '',          // text in the filter input
    showCountryDrop: false,     // dropdown visible
    countryPickerForm: null,    // 'create' | 'edit' — which form is active
    aliasGeneratingId: null,    // id алиаса для которого идёт генерация
    aliasGenerateJobId: null,
    aliasGenerateJobStatus: null,
    aliasTooltip: null,         // { id, alias, x, y } — hover tooltip state
    systemRestoring: false,     // true while restore request is in flight
    showBackupModal: false,     // password prompt for backup download
    backupPassword: '',
    backupPasswordConfirm: '',
    backupIncludeMetrics: false,
    backupDownloading: false,
    showRestorePasswordModal: false, // password prompt for encrypted restore
    restorePassword: '',
    restoreFile: null,          // File object pending restore after password entry
    // Restore preview / remap state
    showRestorePreviewModal: false,
    restorePreview: null,       // { backupIfaces, serverIfaces, needsRemap }
    restoreIfaceMap: {},        // { oldIface: newIface } selected by user
    restorePreviewLoading: false,
    // Pre-restore auto-backups list
    preRestoreBackups: [],
    preRestoreBackupsLoading: false,

    // Import client configs (restore private keys for imported peers)
    importClientConfigsLoading: false,
    showImportClientConfigsResult: false,
    importClientConfigsResult: null, // { matched, unmatched: [...] }

    // Firewall Rules (поглощает PBR)
    firewallRules: [],
    firewallRulesLoading: false,
    firewallPending: false,   // unapplied changes exist
    firewallApplying: false,  // Apply button loading state
    firewallInterfaces: [],
    showFirewallCreate: false,
    showSeparatorModal: false,
    separatorEditId: null,    // null = new, string = edit existing
    separatorEdit: { name: '', color: '' },
    separatorColors: [
      { value: '',         label: 'Default', swatch: '#6b7280' },
      { value: 'red',      label: 'Red',     swatch: '#ef4444' },
      { value: 'orange',   label: 'Orange',  swatch: '#f97316' },
      { value: 'yellow',   label: 'Yellow',  swatch: '#eab308' },
      { value: 'green',    label: 'Green',   swatch: '#22c55e' },
      { value: 'cyan',     label: 'Cyan',    swatch: '#06b6d4' },
      { value: 'blue',     label: 'Blue',    swatch: '#3b82f6' },
      { value: 'purple',   label: 'Purple',  swatch: '#a855f7' },
    ],
    showFirewallEdit: false,
    firewallCreate: {
      name: '',
      interface: 'any',
      protocol: 'any',
      source:      { type: 'any', aliasId: '', value: '', invert: false, portMode: '', port: '', portAliasId: '' },
      destination: { type: 'any', aliasId: '', value: '', invert: false, portMode: '', port: '', portAliasId: '' },
      action: 'accept',
      gatewayId: '',
      gatewayGroupId: '',
      useGroup: false,
      fallbackToDefault: false,
      log: false,
      comment: '',
    },
    firewallEdit: {
      id: null,
      name: '',
      interface: 'any',
      protocol: 'any',
      source:      { type: 'any', aliasId: '', value: '', invert: false, portMode: '', port: '', portAliasId: '' },
      destination: { type: 'any', aliasId: '', value: '', invert: false, portMode: '', port: '', portAliasId: '' },
      action: 'accept',
      gatewayId: '',
      gatewayGroupId: '',
      useGroup: false,
      fallbackToDefault: false,
      log: false,
      comment: '',
    },

    // Toast notifications
    toasts: [],

    uiShowCharts: localStorage.getItem('uiShowCharts') === '1',
    uiTheme: localStorage.theme || 'auto',
    prefersDarkScheme: window.matchMedia('(prefers-color-scheme: dark)'),

    chartOptions: {
      chart: {
        background: 'transparent',
        stacked: false,
        toolbar: {
          show: false,
        },
        animations: {
          enabled: false,
        },
        parentHeightOffset: 0,
        sparkline: {
          enabled: true,
        },
      },
      colors: [],
      stroke: {
        curve: 'smooth',
      },
      fill: {
        type: 'gradient',
        gradient: {
          shade: 'dark',
          type: 'vertical',
          shadeIntensity: 0,
          gradientToColors: CHART_COLORS.gradient[this.theme],
          inverseColors: false,
          opacityTo: 0,
          stops: [0, 100],
        },
      },
      dataLabels: {
        enabled: false,
      },
      plotOptions: {
        bar: {
          horizontal: false,
        },
      },
      xaxis: {
        labels: {
          show: false,
        },
        axisTicks: {
          show: false,
        },
        axisBorder: {
          show: false,
        },
      },
      yaxis: {
        labels: {
          show: false,
        },
        min: 0,
      },
      tooltip: {
        enabled: false,
      },
      legend: {
        show: false,
      },
      grid: {
        show: false,
        padding: {
          left: -10,
          right: 0,
          bottom: -15,
          top: -15,
        },
        column: {
          opacity: 0,
        },
        xaxis: {
          lines: {
            show: false,
          },
        },
      },
    },

    // ISO 3166-1 alpha-2 country list for the country picker combobox.
    countries: [
      {code:'AD',name:'Andorra'},{code:'AE',name:'United Arab Emirates'},{code:'AF',name:'Afghanistan'},
      {code:'AG',name:'Antigua and Barbuda'},{code:'AL',name:'Albania'},{code:'AM',name:'Armenia'},
      {code:'AO',name:'Angola'},{code:'AR',name:'Argentina'},{code:'AT',name:'Austria'},
      {code:'AU',name:'Australia'},{code:'AZ',name:'Azerbaijan'},{code:'BA',name:'Bosnia and Herzegovina'},
      {code:'BB',name:'Barbados'},{code:'BD',name:'Bangladesh'},{code:'BE',name:'Belgium'},
      {code:'BF',name:'Burkina Faso'},{code:'BG',name:'Bulgaria'},{code:'BH',name:'Bahrain'},
      {code:'BI',name:'Burundi'},{code:'BJ',name:'Benin'},{code:'BN',name:'Brunei'},
      {code:'BO',name:'Bolivia'},{code:'BR',name:'Brazil'},{code:'BS',name:'Bahamas'},
      {code:'BT',name:'Bhutan'},{code:'BW',name:'Botswana'},{code:'BY',name:'Belarus'},
      {code:'BZ',name:'Belize'},{code:'CA',name:'Canada'},{code:'CD',name:'DR Congo'},
      {code:'CF',name:'Central African Republic'},{code:'CG',name:'Republic of the Congo'},
      {code:'CH',name:'Switzerland'},{code:'CI',name:"Côte d'Ivoire"},{code:'CL',name:'Chile'},
      {code:'CM',name:'Cameroon'},{code:'CN',name:'China'},{code:'CO',name:'Colombia'},
      {code:'CR',name:'Costa Rica'},{code:'CU',name:'Cuba'},{code:'CV',name:'Cape Verde'},
      {code:'CY',name:'Cyprus'},{code:'CZ',name:'Czech Republic'},{code:'DE',name:'Germany'},
      {code:'DJ',name:'Djibouti'},{code:'DK',name:'Denmark'},{code:'DM',name:'Dominica'},
      {code:'DO',name:'Dominican Republic'},{code:'DZ',name:'Algeria'},{code:'EC',name:'Ecuador'},
      {code:'EE',name:'Estonia'},{code:'EG',name:'Egypt'},{code:'ER',name:'Eritrea'},
      {code:'ES',name:'Spain'},{code:'ET',name:'Ethiopia'},{code:'FI',name:'Finland'},
      {code:'FJ',name:'Fiji'},{code:'FM',name:'Micronesia'},{code:'FR',name:'France'},
      {code:'GA',name:'Gabon'},{code:'GB',name:'United Kingdom'},{code:'GD',name:'Grenada'},
      {code:'GE',name:'Georgia'},{code:'GH',name:'Ghana'},{code:'GM',name:'Gambia'},
      {code:'GN',name:'Guinea'},{code:'GQ',name:'Equatorial Guinea'},{code:'GR',name:'Greece'},
      {code:'GT',name:'Guatemala'},{code:'GW',name:'Guinea-Bissau'},{code:'GY',name:'Guyana'},
      {code:'HN',name:'Honduras'},{code:'HR',name:'Croatia'},{code:'HT',name:'Haiti'},
      {code:'HU',name:'Hungary'},{code:'ID',name:'Indonesia'},{code:'IE',name:'Ireland'},
      {code:'IL',name:'Israel'},{code:'IN',name:'India'},{code:'IQ',name:'Iraq'},
      {code:'IR',name:'Iran'},{code:'IS',name:'Iceland'},{code:'IT',name:'Italy'},
      {code:'JM',name:'Jamaica'},{code:'JO',name:'Jordan'},{code:'JP',name:'Japan'},
      {code:'KE',name:'Kenya'},{code:'KG',name:'Kyrgyzstan'},{code:'KH',name:'Cambodia'},
      {code:'KI',name:'Kiribati'},{code:'KM',name:'Comoros'},{code:'KN',name:'Saint Kitts and Nevis'},
      {code:'KP',name:'North Korea'},{code:'KR',name:'South Korea'},{code:'KW',name:'Kuwait'},
      {code:'KZ',name:'Kazakhstan'},{code:'LA',name:'Laos'},{code:'LB',name:'Lebanon'},
      {code:'LC',name:'Saint Lucia'},{code:'LI',name:'Liechtenstein'},{code:'LK',name:'Sri Lanka'},
      {code:'LR',name:'Liberia'},{code:'LS',name:'Lesotho'},{code:'LT',name:'Lithuania'},
      {code:'LU',name:'Luxembourg'},{code:'LV',name:'Latvia'},{code:'LY',name:'Libya'},
      {code:'MA',name:'Morocco'},{code:'MC',name:'Monaco'},{code:'MD',name:'Moldova'},
      {code:'ME',name:'Montenegro'},{code:'MG',name:'Madagascar'},{code:'MH',name:'Marshall Islands'},
      {code:'MK',name:'North Macedonia'},{code:'ML',name:'Mali'},{code:'MM',name:'Myanmar'},
      {code:'MN',name:'Mongolia'},{code:'MR',name:'Mauritania'},{code:'MT',name:'Malta'},
      {code:'MU',name:'Mauritius'},{code:'MV',name:'Maldives'},{code:'MW',name:'Malawi'},
      {code:'MX',name:'Mexico'},{code:'MY',name:'Malaysia'},{code:'MZ',name:'Mozambique'},
      {code:'NA',name:'Namibia'},{code:'NE',name:'Niger'},{code:'NG',name:'Nigeria'},
      {code:'NI',name:'Nicaragua'},{code:'NL',name:'Netherlands'},{code:'NO',name:'Norway'},
      {code:'NP',name:'Nepal'},{code:'NR',name:'Nauru'},{code:'NZ',name:'New Zealand'},
      {code:'OM',name:'Oman'},{code:'PA',name:'Panama'},{code:'PE',name:'Peru'},
      {code:'PG',name:'Papua New Guinea'},{code:'PH',name:'Philippines'},{code:'PK',name:'Pakistan'},
      {code:'PL',name:'Poland'},{code:'PT',name:'Portugal'},{code:'PW',name:'Palau'},
      {code:'PY',name:'Paraguay'},{code:'QA',name:'Qatar'},{code:'RO',name:'Romania'},
      {code:'RS',name:'Serbia'},{code:'RU',name:'Russia'},{code:'RW',name:'Rwanda'},
      {code:'SA',name:'Saudi Arabia'},{code:'SB',name:'Solomon Islands'},{code:'SC',name:'Seychelles'},
      {code:'SD',name:'Sudan'},{code:'SE',name:'Sweden'},{code:'SG',name:'Singapore'},
      {code:'SI',name:'Slovenia'},{code:'SK',name:'Slovakia'},{code:'SL',name:'Sierra Leone'},
      {code:'SM',name:'San Marino'},{code:'SN',name:'Senegal'},{code:'SO',name:'Somalia'},
      {code:'SR',name:'Suriname'},{code:'SS',name:'South Sudan'},{code:'ST',name:'São Tomé and Príncipe'},
      {code:'SV',name:'El Salvador'},{code:'SY',name:'Syria'},{code:'SZ',name:'Eswatini'},
      {code:'TD',name:'Chad'},{code:'TG',name:'Togo'},{code:'TH',name:'Thailand'},
      {code:'TJ',name:'Tajikistan'},{code:'TL',name:'Timor-Leste'},{code:'TM',name:'Turkmenistan'},
      {code:'TN',name:'Tunisia'},{code:'TO',name:'Tonga'},{code:'TR',name:'Turkey'},
      {code:'TT',name:'Trinidad and Tobago'},{code:'TV',name:'Tuvalu'},{code:'TZ',name:'Tanzania'},
      {code:'UA',name:'Ukraine'},{code:'UG',name:'Uganda'},{code:'US',name:'United States'},
      {code:'UY',name:'Uruguay'},{code:'UZ',name:'Uzbekistan'},{code:'VA',name:'Vatican City'},
      {code:'VC',name:'Saint Vincent and the Grenadines'},{code:'VE',name:'Venezuela'},
      {code:'VN',name:'Vietnam'},{code:'VU',name:'Vanuatu'},{code:'WS',name:'Samoa'},
      {code:'YE',name:'Yemen'},{code:'ZA',name:'South Africa'},{code:'ZM',name:'Zambia'},
      {code:'ZW',name:'Zimbabwe'},
    ],

  },
  methods: {
    dateTime: (value) => {
      return new Intl.DateTimeFormat(undefined, {
        year: 'numeric',
        month: 'short',
        day: 'numeric',
        hour: 'numeric',
        minute: 'numeric',
      }).format(value);
    },

    // ========================================================================
    // Toast notifications
    // ========================================================================
    showToast(message, type = 'success', duration) {
      const id = Date.now() + Math.random();
      // Errors persist until manually dismissed; success/info auto-dismiss after 5s
      const ms = duration !== undefined ? duration : (type === 'error' ? 0 : 5000);
      this.toasts.push({ id, message, type });
      if (ms > 0) setTimeout(() => this.dismissToast(id), ms);
    },
    dismissToast(id) {
      const idx = this.toasts.findIndex(t => t.id === id);
      if (idx !== -1) this.toasts.splice(idx, 1);
    },


    async refresh({
      updateCharts = false,
    } = {}) {
      if (!this.authenticated) return;

      const clients = await this.api.getClients();
      this.clients = clients.map((client) => {
        if (client.name.includes('@') && client.name.includes('.') && this.avatarSettings.gravatar) {
          client.avatar = `https://gravatar.com/avatar/${sha256(client.name.toLowerCase().trim())}.jpg`;
        } else if (this.avatarSettings.dicebear) {
          client.avatar = `https://api.dicebear.com/9.x/${this.avatarSettings.dicebear}/svg?seed=${sha256(client.name.toLowerCase().trim())}`
        }

        if (!this.clientsPersist[client.id]) {
          this.clientsPersist[client.id] = {};
          this.clientsPersist[client.id].transferRxHistory = Array(50).fill(0);
          this.clientsPersist[client.id].transferRxPrevious = client.transferRx;
          this.clientsPersist[client.id].transferTxHistory = Array(50).fill(0);
          this.clientsPersist[client.id].transferTxPrevious = client.transferTx;
        }

        // Debug
        // client.transferRx = this.clientsPersist[client.id].transferRxPrevious + Math.random() * 1000;
        // client.transferTx = this.clientsPersist[client.id].transferTxPrevious + Math.random() * 1000;
        // client.latestHandshakeAt = new Date();
        // this.requiresPassword = true;

        this.clientsPersist[client.id].transferRxCurrent = client.transferRx - this.clientsPersist[client.id].transferRxPrevious;
        this.clientsPersist[client.id].transferRxPrevious = client.transferRx;
        this.clientsPersist[client.id].transferTxCurrent = client.transferTx - this.clientsPersist[client.id].transferTxPrevious;
        this.clientsPersist[client.id].transferTxPrevious = client.transferTx;

        if (updateCharts) {
          this.clientsPersist[client.id].transferRxHistory.push(this.clientsPersist[client.id].transferRxCurrent);
          this.clientsPersist[client.id].transferRxHistory.shift();

          this.clientsPersist[client.id].transferTxHistory.push(this.clientsPersist[client.id].transferTxCurrent);
          this.clientsPersist[client.id].transferTxHistory.shift();

          this.clientsPersist[client.id].transferTxSeries = [{
            name: 'Tx',
            data: this.clientsPersist[client.id].transferTxHistory,
          }];

          this.clientsPersist[client.id].transferRxSeries = [{
            name: 'Rx',
            data: this.clientsPersist[client.id].transferRxHistory,
          }];

          client.transferTxHistory = this.clientsPersist[client.id].transferTxHistory;
          client.transferRxHistory = this.clientsPersist[client.id].transferRxHistory;
          client.transferMax = Math.max(...client.transferTxHistory, ...client.transferRxHistory);

          client.transferTxSeries = this.clientsPersist[client.id].transferTxSeries;
          client.transferRxSeries = this.clientsPersist[client.id].transferRxSeries;
        }

        client.transferTxCurrent = this.clientsPersist[client.id].transferTxCurrent;
        client.transferRxCurrent = this.clientsPersist[client.id].transferRxCurrent;

        client.hoverTx = this.clientsPersist[client.id].hoverTx;
        client.hoverRx = this.clientsPersist[client.id].hoverRx;

        return client;
      });

      if (this.enableSortClient) {
        this.clients = sortByProperty(this.clients, 'name', this.sortClient);
      }
    },
    login(e) {
      e.preventDefault();

      if (!this.password) return;
      if (this.authenticating) return;

      this.authenticating = true;
      this.api.createSession({
        username: this.username || 'admin',
        password: this.password,
        remember: this.remember,
      })
        .then(async (res) => {
          // Server may require TOTP as a second step.
          if (res && res.totp_required) {
            this.totpRequired = true;
            this.totpCode = '';
            return; // stay on login screen — show TOTP input
          }
          // Fully authenticated (no TOTP or TOTP already done).
          await this._onLoginSuccess();
        })
        .catch((err) => {
          this.showToast(err.message || err.toString(), 'error');
        })
        .finally(() => {
          this.authenticating = false;
          this.password = null;
        });
    },

    // Step 2: submit TOTP code after password was accepted.
    async loginStep2() {
      if (!this.totpCode || this.authenticating) return;
      this.authenticating = true;
      try {
        await this.api.verifyTOTP({ code: this.totpCode });
        this.totpRequired = false;
        this.totpCode = '';
        await this._onLoginSuccess();
      } catch (err) {
        this.showToast(err.message || err.toString(), 'error');
      } finally {
        this.authenticating = false;
      }
    },

    // Called after a successful full authentication (steps 1 or 2).
    async _onLoginSuccess() {
      const session = await this.api.getSession();
      this.authenticated = session.authenticated;
      this.requiresPassword = session.requiresPassword;
      await this.refresh();
      // Re-load data that may have got 401 before login.
      this.loadTunnelInterfaces();
      this.loadSettings();
      this.loadClientGroups();
      this.loadUsers();
      this.loadCurrentUser();
      this.loadRemotes();
      // Re-load dashboard after login: loadDashboard() in mounted() ran before
      // the dashboard DOM existed (authenticated=false → v-if removed the div),
      // so dashInitGrid() silently returned without initialising GridStack.
      this.loadDashboard();
      if (this.activePage === 'gateways') {
        this.loadGateways();
        this.loadGatewayGroups();
        this.loadSystemInterfaces();
      }
      if (this.activePage === 'settings') {
        this.loadUsers();
      }
    },
    logout(e) {
      e.preventDefault();

      this.api.deleteSession()
        .then(() => {
          this.authenticated = false;
          this.clients = null;
        })
        .catch((err) => {
          this.showToast(err.message || err.toString(), 'error');
        });
    },
    createClient() {
      const name = this.clientCreateName;
      const expiredDate = this.clientExpiredDate;
      if (!name) return;

      this.api.createClient({ name, expiredDate })
        .catch((err) => this.showToast(err.message || err.toString(), 'error'))
        .finally(() => this.refresh().catch(console.error));
    },
    deleteClient(client) {
      this.api.deleteClient({ clientId: client.id })
        .catch((err) => this.showToast(err.message || err.toString(), 'error'))
        .finally(() => this.refresh().catch(console.error));
    },
    showOneTimeLink(client) {
      this.api.showOneTimeLink({ clientId: client.id })
        .catch((err) => this.showToast(err.message || err.toString(), 'error'))
        .finally(() => this.refresh().catch(console.error));
    },
    enableClient(client) {
      this.api.enableClient({ clientId: client.id })
        .catch((err) => this.showToast(err.message || err.toString(), 'error'))
        .finally(() => this.refresh().catch(console.error));
    },
    disableClient(client) {
      this.api.disableClient({ clientId: client.id })
        .catch((err) => this.showToast(err.message || err.toString(), 'error'))
        .finally(() => this.refresh().catch(console.error));
    },
    updateClientName(client, name) {
      this.api.updateClientName({ clientId: client.id, name })
        .catch((err) => this.showToast(err.message || err.toString(), 'error'))
        .finally(() => this.refresh().catch(console.error));
    },
    updateClientAddress(client, address) {
      this.api.updateClientAddress({ clientId: client.id, address })
        .catch((err) => this.showToast(err.message || err.toString(), 'error'))
        .finally(() => this.refresh().catch(console.error));
    },
    updateClientExpireDate(client, expireDate) {
      this.api.updateClientExpireDate({ clientId: client.id, expireDate })
        .catch((err) => this.showToast(err.message || err.toString(), 'error'))
        .finally(() => this.refresh().catch(console.error));
    },
    restoreConfig(e) {
      e.preventDefault();
      const file = e.currentTarget.files.item(0);
      if (file) {
        file.text()
          .then((content) => {
            this.api.restoreConfiguration(content)
              .then((_result) => this.showToast('The configuration was updated.'))
              .catch((err) => this.showToast(err.message || err.toString(), 'error'))
              .finally(() => this.refresh().catch(console.error));
          })
          .catch((err) => this.showToast(err.message || err.toString(), 'error'));
      } else {
        this.showToast('Failed to load your file!', 'error');
      }
    },
    toggleTheme() {
      const themes = ['light', 'dark', 'auto'];
      const currentIndex = themes.indexOf(this.uiTheme);
      const newIndex = (currentIndex + 1) % themes.length;
      this.uiTheme = themes[newIndex];
      localStorage.theme = this.uiTheme;
      this.setTheme(this.uiTheme);
    },
    setTheme(theme) {
      const { classList } = document.documentElement;
      const shouldAddDarkClass = theme === 'dark' || (theme === 'auto' && this.prefersDarkScheme.matches);
      classList.toggle('dark', shouldAddDarkClass);
    },
    handlePrefersChange(e) {
      if (localStorage.theme === 'auto') {
        this.setTheme(e.matches ? 'dark' : 'light');
      }
    },
    toggleCharts() {
      localStorage.setItem('uiShowCharts', this.uiShowCharts ? 1 : 0);
    },

    // Sidebar navigation
    switchPage(pageId) {
      // FIX: destroy GridStack when leaving dashboard — clears ResizeObserver,
      // inline styles and CSS vars it set on parent elements (which caused a
      // vertical gap on the interfaces page after visiting dashboard).
      if (this.activePage === 'diagnostics' && pageId !== 'diagnostics' && this.diagGrid) {
        // Clear inline styles GridStack sets on content elements and the grid container.
        // GridStack sets height on .diag-grid itself (e.g. height:60px for 1-row grid) and
        // on .grid-stack-item-content children. Without this, Vue's DOM reuse could carry
        // those heights over to unrelated elements on the next page.
        document.querySelectorAll('.diag-grid .grid-stack-item-content').forEach(el => { el.style.cssText = ''; });
        this.diagGrid.destroy(false);
        // Clear AFTER destroy too — destroy(false) may re-apply inline styles during cleanup.
        document.querySelectorAll('.diag-grid, .diag-grid .grid-stack-item-content, .diag-grid .grid-stack-item').forEach(el => { el.style.cssText = ''; });
        this.diagGrid = null;
        this.metricsStopPoller();
      }
      if (this.activePage === 'dashboard' && pageId !== 'dashboard' && this.dashGrid) {
        // destroy(false) — remove GridStack listeners/styles but keep DOM nodes
        // so that Vue's v-if can cleanly remove the subtree without conflict.
        this.dashGrid.destroy(false);
        // Clear GridStack-set inline styles from the grid container and items after destroy.
        document.querySelectorAll('#dashboard-grid, #dashboard-grid .grid-stack-item-content, #dashboard-grid .grid-stack-item').forEach(el => { el.style.cssText = ''; });
        this.dashGrid = null;
        this.metricsStopPoller();
      }
      if (this._dashResizeObs) {
        this._dashResizeObs.disconnect();
        this._dashResizeObs = null;
      }
      this.activePage = pageId;
      if (pageId.startsWith('wizard-')) this.wizardsExpanded = true;
      // Reset scroll and any GridStack inline styles AFTER Vue updates the DOM.
      this.$nextTick(() => {
        // Reset all inline styles GridStack may have set on the scroll container,
        // then restore only the original padding. height/overflow come from CSS.
        const mainEl = document.querySelector('.app-main');
        if (mainEl) {
          mainEl.style.cssText = 'padding: 24px 32px;';
          mainEl.scrollTop = 0;
        }
        // Also reset window scroll in case overflow:hidden on mainEl caused
        // the window to be the scroll container while on the previous page.
        window.scrollTo(0, 0);
      });
      if (pageId === 'dashboard') {
        // loadDashboard already calls dashInitGrid after await $nextTick —
        // do not call it separately here to avoid a double-init race.
        this.loadDashboard();
        this.loadSystemInfo();
        this.metricsStartPoller();
      }
      if (pageId === 'diagnostics') {
        this.loadDiagnostics();
        this.metricsStartPoller();
        this.pingLoadInterfaces();
      }
      if (pageId === 'interfaces') this.loadTunnelInterfaces();
      if (pageId === 'settings') { this.loadSettings(); this.loadUsers(); this.loadApiTokens(); this.loadPreRestoreBackups(); }
      if (pageId === 'remotes') this.loadRemotes();
      if (pageId === 'gateways') {
        this.loadGateways();
        this.loadGatewayGroups();
        this.loadSystemInterfaces();
      }
      if (pageId === 'routing') {
        // Запускаем параллельно — loadKernelRoutes не зависит от списка таблиц
        this.loadRoutingTables();
        this.loadKernelRoutes();
        this.loadStaticRoutes();
        if (!this.gateways.length) this.loadGateways();
        if (!this.gatewayGroups.length) this.loadGatewayGroups();
        this.loadNatInterfaces();
      }
      if (pageId === 'nat') {
        this.loadNatInterfaces();
        this.loadNatRules();
        if (!this.aliases.length) this.loadAliases();
      }
      if (pageId === 'firewall-aliases') {
        this.loadAliases();
        this.loadClientGroups();
      }
      if (pageId === 'wizard-simple-vpn') {
        this.wizardVPNInit();
      }
      if (pageId === 'wizard-uplink-vpn') {
        this.wizardUplinkInit();
      }
      if (pageId === 'wizard-cascade-s2s') {
        this.wizardS2SInit();
      }
      if (pageId === 'firewall') {
        this.loadFirewallRules();
        this.loadFirewallInterfaces();
        if (!this.aliases.length) this.loadAliases();
        if (!this.gateways.length) this.loadGateways();
        if (!this.gatewayGroups.length) this.loadGatewayGroups();
      }
    },

    // ========================================================================
    // Tunnel Interfaces Methods (New Architecture)
    // ========================================================================

    async loadTunnelInterfaces() {
      try {
        const data = await this.api.getTunnelInterfaces();
        this.tunnelInterfaces = data.interfaces || [];
      } catch (err) {
        console.error('Failed to load tunnel interfaces:', err);
      }
    },

    async createTunnelInterface() {
      try {
        if (!this.interfaceCreate.name) {
          this.showToast('Please enter interface name', 'error');
          return;
        }

        // Tunnel Address обязателен — нужен для авто-IP пиров и PostUp/PostDown
        if (!this.interfaceCreate.address || !this.interfaceCreate.address.includes('/')) {
          this.showToast('Please enter Tunnel Address in CIDR format (e.g. 10.100.0.1/24)', 'error');
          return;
        }

        if (this.interfaceCreate.protocol === 'amneziawg-2.0') {
          if (!this.interfaceCreate.settings.h1 || !this.interfaceCreate.settings.h2 ||
              !this.interfaceCreate.settings.h3 || !this.interfaceCreate.settings.h4) {
            this.showToast('Please set H1-H4 parameters for AWG 2.0 (select a profile or enter manually)', 'error');
            return;
          }
        }

        const payload = {
          name: this.interfaceCreate.name,
          protocol: this.interfaceCreate.protocol,
          address: this.interfaceCreate.address,
          listenPort: this.interfaceCreate.listenPort ? parseInt(this.interfaceCreate.listenPort, 10) : undefined,
          disableRoutes: this.interfaceCreate.disableRoutes || false,
          dns: this.interfaceCreate.dns || '',
        };

        if (this.interfaceCreate.protocol === 'amneziawg-2.0') {
          payload.settings = this.interfaceCreate.settings;
        }

        const newIface = await this.api.createTunnelInterface(payload);
        this.showInterfaceCreate = false;
        this._resetInterfaceCreate();

        await this.loadTunnelInterfaces();
        this.loadNatInterfaces();
        this.loadFirewallInterfaces();
        // Auto-switch to the new interface tab
        if (newIface && newIface.id) {
          this.activeInterfaceId = newIface.id;
        }
      } catch (err) {
        console.error('Failed to create interface:', err);
        this.showToast(`Failed: ${err.message}`, 'error');
      }
    },

    // ========================================================================
    // Quick Create Interface
    // ========================================================================

    async quickCreateTunnelInterface() {
      try {
        const body = {
          protocol: this.interfaceCreate.protocol,
        };
        // Name is optional — server defaults to interface ID when omitted.
        const trimmedName = (this.interfaceCreate.name || '').trim();
        if (trimmedName) {
          body.name = trimmedName;
        }

        const data = await this.api.call({ method: 'post', path: '/tunnel-interfaces/quick-create', body });
        this.showInterfaceCreate = false;
        this._resetInterfaceCreate();
        await this.loadTunnelInterfaces();
        this.loadNatInterfaces();
        this.loadFirewallInterfaces();

        const iface = data.interface || {};
        const addr  = iface.address    || '';
        const port  = iface.listenPort || '';
        const proto = iface.protocol === 'amneziawg-2.0' ? ' · AWG2' : '';

        if (data.started) {
          this.showToast(`✅ ${iface.id} created & started\n${addr} · UDP ${port}${proto}`, 'success');
          this.activeInterfaceId = iface.id;
        } else {
          this.showToast(
            `⚠️ ${iface.id} created but failed to start\n${data.startError || 'Unknown error'}`,
            'error'
          );
        }
      } catch (err) {
        console.error('Quick create failed:', err);
        this.showToast(`Failed: ${err.message}`, 'error');
      }
    },

    onConfFileSelected(event) {
      const file = event.target.files && event.target.files[0];
      if (!file) return;
      // Auto-fill Name from filename (strip .conf / .txt extension).
      if (!this.importConfForm.name) {
        this.importConfForm.name = file.name.replace(/\.(conf|txt)$/i, '');
      }
      this.importConfForm.fileName = file.name;
      const reader = new FileReader();
      reader.onload = (e) => {
        this.importConfForm.conf = e.target.result || '';
      };
      reader.readAsText(file);
      // Reset input so the same file can be re-selected if needed.
      event.target.value = '';
    },

    async doImportConf() {
      const name = (this.importConfForm.name || '').trim();
      const conf = (this.importConfForm.conf || '').trim();
      if (!name) { this.showToast('Please enter a name', 'error'); return; }
      if (!conf)  { this.showToast('Please paste the .conf content', 'error'); return; }

      this.importConfWarning = '';
      try {
        const isServer = this.importConfMode === 'server';
        const res = isServer
          ? await this.api.importTunnelConfServer({ name, conf })
          : await this.api.importTunnelConf({ name, conf });
        this.showImportBackup = false;
        this.importConfForm = { name: '', conf: '', fileName: '' };
        await this.loadTunnelInterfaces();
        this.loadNatInterfaces();
        this.loadFirewallInterfaces();

        const iface = res.interface || {};
        const proto = iface.protocol === 'amneziawg-2.0' ? ' · AWG2' : ' · WG1';
        if (res.conflictWarning) {
          this.showToast(`⚠️ ${res.conflictWarning}`, 'error');
        }
        if (res.started) {
          const extra = isServer ? ` · ${res.peersCreated} peers` : '';
          this.showToast(`✅ ${iface.id} imported & started · ${iface.address}${proto}${extra}`);
          this.activeInterfaceId = iface.id;
        } else {
          this.showToast(
            `⚠️ ${iface.id} imported but failed to start\n${res.startError || 'Unknown error'}`,
            'error'
          );
        }
        if (isServer && (res.peersFailed || []).length > 0) {
          this.showToast(`⚠️ Failed to import peers: ${res.peersFailed.join(', ')}`, 'error');
        }
      } catch (err) {
        console.error('Import conf failed:', err);
        this.showToast(`Failed: ${err.message}`, 'error');
      }
    },

    onBackupFileSelected(event) {
      const file = event.target.files[0];
      if (!file) return;
      this.importBackupForm.fileName = file.name;
      const reader = new FileReader();
      reader.onload = (e) => {
        const text = e.target.result || '';
        this.importBackupForm.json = text;
        // Auto-detect listen port from JSON
        try {
          const parsed = JSON.parse(text);
          const port =
            (parsed.interface && parsed.interface.listenPort) || // Cascade export
            (parsed.server && parsed.server.port);               // AWG-Easy
          if (port && !this.importBackupForm.listenPort) {
            this.importBackupForm.listenPort = String(port);
          }
        } catch (_) {}
      };
      reader.readAsText(file);
    },

    async doImportBackup() {
      const json = (this.importBackupForm.json || '').trim();
      const port = parseInt(this.importBackupForm.listenPort, 10);
      if (!json)        { this.showToast('Please select a backup file', 'error'); return; }
      if (!port || port < 1 || port > 65535) {
        this.showToast('Please enter a valid UDP port (1–65535)', 'error'); return;
      }

      try {
        const res = await this.api.importTunnelBackup({ json, listenPort: port });
        this.showImportBackup = false;
        this.importBackupForm = { json: '', listenPort: '', fileName: '' };
        await this.loadTunnelInterfaces();
        this.loadNatInterfaces();
        this.loadFirewallInterfaces();

        const iface = res.interface || {};
        const proto = iface.protocol === 'amneziawg-2.0' ? ' · AWG2' : ' · WG1';
        const failed = (res.peersFailed || []).length;

        if (res.started) {
          let msg = `✅ Backup imported: ${iface.id} · ${iface.address}${proto} · ${res.peersCreated} clients`;
          if (failed > 0) msg += ` (${failed} failed)`;
          this.showToast(msg);
          this.activeInterfaceId = iface.id;
        } else {
          this.showToast(
            `⚠️ Backup imported but interface failed to start\n${res.startError || ''}`,
            'error'
          );
        }
        if (failed > 0) {
          this.showToast(`⚠️ Failed to import clients: ${res.peersFailed.join(', ')}`, 'error');
        }
      } catch (err) {
        console.error('Import backup failed:', err);
        this.showToast(`Failed: ${err.message}`, 'error');
      }
    },

    openExportInterface(iface) {
      this.exportInterfaceId = iface.id;
      this.exportInterfaceIncludePeers = true;
      this.showExportInterface = true;
    },

    async doExportInterface() {
      if (!this.exportInterfaceId) return;
      try {
        const blob = await this.api.exportTunnelInterface({
          interfaceId: this.exportInterfaceId,
          includePeers: this.exportInterfaceIncludePeers,
        });
        const url = URL.createObjectURL(blob);
        const a = document.createElement('a');
        a.href = url;
        a.download = `${this.exportInterfaceId}-export.json`;
        a.click();
        URL.revokeObjectURL(url);
        this.showExportInterface = false;
      } catch (err) {
        this.showToast(`Export failed: ${err.message}`, 'error');
      }
    },

    async doImportInterface() {
      const json = (this.importBackupForm.json || '').trim();
      const port = parseInt(this.importBackupForm.listenPort, 10);
      if (!json)        { this.showToast('Please select a backup file', 'error'); return; }
      if (!port || port < 1 || port > 65535) {
        this.showToast('Please enter a valid UDP port (1–65535)', 'error'); return;
      }
      try {
        const res = await this.api.importTunnelInterface({ json, listenPort: port });
        this.showImportBackup = false;
        this.importBackupForm = { json: '', listenPort: '', fileName: '' };
        await this.loadTunnelInterfaces();
        this.loadNatInterfaces();
        this.loadFirewallInterfaces();
        const iface = res.interface || {};
        const proto = iface.protocol === 'amneziawg-2.0' ? ' · AWG2' : ' · WG1';
        const msg = res.started
          ? `✅ Interface restored: ${iface.id} · ${iface.address}${proto} · ${res.peersCreated} peers`
          : `⚠️ Interface restored but failed to start: ${res.startError || ''}`;
        this.showToast(msg, res.started ? 'success' : 'error');
        if (res.started) this.activeInterfaceId = iface.id;
      } catch (err) {
        this.showToast(`Failed: ${err.message}`, 'error');
      }
    },

    // generateAndFillInterfaceParams — fills AWG2 fields in the Manual form
    // by generating a random profile via the /templates/generate endpoint.
    async generateAndFillInterfaceParams() {
      try {
        const { params } = await this.api.generateTemplate({ profile: 'random', intensity: 'medium' });
        // Merge generated params into interfaceCreate.settings.
        Object.assign(this.interfaceCreate.settings, {
          jc:   params.jc   ?? this.interfaceCreate.settings.jc,
          jmin: params.jmin ?? this.interfaceCreate.settings.jmin,
          jmax: params.jmax ?? this.interfaceCreate.settings.jmax,
          s1:   params.s1   ?? this.interfaceCreate.settings.s1,
          s2:   params.s2   ?? this.interfaceCreate.settings.s2,
          s3:   params.s3   ?? this.interfaceCreate.settings.s3,
          s4:   params.s4   ?? this.interfaceCreate.settings.s4,
          h1:   params.h1   ?? this.interfaceCreate.settings.h1,
          h2:   params.h2   ?? this.interfaceCreate.settings.h2,
          h3:   params.h3   ?? this.interfaceCreate.settings.h3,
          h4:   params.h4   ?? this.interfaceCreate.settings.h4,
          i1:   params.i1   ?? this.interfaceCreate.settings.i1,
          i2:   params.i2   ?? this.interfaceCreate.settings.i2,
          i3:   params.i3   ?? this.interfaceCreate.settings.i3,
          i4:   params.i4   ?? this.interfaceCreate.settings.i4,
          i5:   params.i5   ?? this.interfaceCreate.settings.i5,
        });
        this.showToast('AWG2 params generated ✓', 'success');
      } catch (err) {
        console.error('generateAndFillInterfaceParams failed:', err);
        this.showToast(`Failed to generate params: ${err.message}`, 'error');
      }
    },

    // _resetInterfaceCreate resets the create-modal state including createMode.
    // Called after both quick-create and manual create complete.
    _resetInterfaceCreate() {
      this.createMode = 'quick';
      this.interfaceCreate = {
        name: '', protocol: 'wireguard-1.0', address: '', listenPort: '',
        disableRoutes: false, dns: '', selectedTemplateId: '',
        settings: {
          jc: 6, jmin: 10, jmax: 50, s1: 64, s2: 67, s3: 64, s4: 4,
          h1: '', h2: '', h3: '', h4: '',
          i1: '', i2: '', i3: '', i4: '', i5: '',
        },
      };
    },

    // ========================================================================
    // Version / Update check
    // ========================================================================

    async checkForUpdates() {
      this.updateChecking = true;
      try {
        const res = await fetch('./api/version/check', { method: 'POST' });
        if (res.ok) {
          const prev = this.versionInfo;
          this.versionInfo = await res.json();
          this.updateBannerDismissed = false;
          if (this.versionInfo.updateAvailable) {
            this.showToast(`Update available: ${this.versionInfo.latestVersion}`, 'info', 6000);
          } else {
            this.showToast("You're up to date", 'success', 4000);
          }
        }
      } catch (_) {
        this.showToast('Update check failed', 'error');
      } finally {
        this.updateChecking = false;
      }
    },

    async loadVersionInfo() {
      try {
        const res = await fetch('./api/version');
        if (res.ok) {
          const prev = this.versionInfo;
          this.versionInfo = await res.json();
          this.updateBannerDismissed = false;
          if (this.versionInfo.updateAvailable && !(prev && prev.updateAvailable)) {
            this.showToast(`Update available: ${this.versionInfo.latestVersion}`, 'info', 6000);
          }
        }
      } catch (_) {
        // silently ignore — non-critical
      }
    },

    // ========================================================================
    // Edit Interface
    // ========================================================================

    openInterfaceEdit(iface) {
      const s = iface.settings || {};
      this.interfaceEdit = {
        id: iface.id,
        name: iface.name || iface.id,
        address: iface.address || '',
        listenPort: iface.listenPort || '',
        disableRoutes: !!iface.disableRoutes,
        natDisabled: !!iface.natDisabled,
        dns: iface.dns || '',
        publicHost: iface.publicHost || '',
        mtu: iface.mtu || 0,
        mss: iface.mss || 0,
        kernelMtu: iface.kernelMtu || 0,
        protocol: iface.protocol || 'wireguard-1.0',
        selectedTemplateId: '',
        settings: {
          jc:   s.jc   ?? 6,   jmin: s.jmin ?? 10,  jmax: s.jmax ?? 50,
          s1:   s.s1   ?? 64,  s2:   s.s2   ?? 67,  s3:   s.s3  ?? 64,  s4: s.s4 ?? 4,
          h1:   s.h1   || '',  h2:   s.h2   || '',  h3:   s.h3  || '',  h4: s.h4 || '',
          i1:   s.i1   || '',  i2:   s.i2   || '',  i3:   s.i3  || '',  i4: s.i4 || '',  i5: s.i5 || '',
        },
      };
      this.showInterfaceEdit = true;
    },

    onMssSelectChange(e) {
      const mode = e.target.value;
      if (mode === 'disabled') this.interfaceEdit.mss = 0;
      else if (mode === 'auto') this.interfaceEdit.mss = -1;
      else if (mode === 'manual') this.interfaceEdit.mss = this.interfaceEdit.mss > 0 ? this.interfaceEdit.mss : 1380;
    },

    onEditInterfaceTemplateSelect(templateId) {
      if (!templateId) return;
      const tmpl = (this.templates || []).find(t => t.id === templateId);
      if (!tmpl) return;
      this.interfaceEdit.settings = {
        jc:   tmpl.jc,   jmin: tmpl.jmin,  jmax: tmpl.jmax,
        s1:   tmpl.s1,   s2:   tmpl.s2,    s3:   tmpl.s3,   s4: tmpl.s4,
        h1:   tmpl.h1,   h2:   tmpl.h2,    h3:   tmpl.h3,   h4: tmpl.h4,
        i1:   tmpl.i1 || '', i2: tmpl.i2 || '', i3: tmpl.i3 || '',
        i4:   tmpl.i4 || '', i5: tmpl.i5 || '',
      };
    },

    async saveInterfaceEdit() {
      const { id, name, address, listenPort, disableRoutes, natDisabled, dns, publicHost, mtu, mss, protocol, settings } = this.interfaceEdit;

      if (!name) { this.showToast('Please enter a name', 'error'); return; }
      if (!address || !address.includes('/')) {
        this.showToast('Please enter Tunnel Address in CIDR format (e.g. 10.100.0.1/24)', 'error');
        return;
      }
      if (protocol === 'amneziawg-2.0') {
        if (!settings.h1 || !settings.h2 || !settings.h3 || !settings.h4) {
          this.showToast('Please set H1-H4 parameters for AWG 2.0 (select a profile or enter manually)', 'error');
          return;
        }
      }

      const payload = {
        name,
        address,
        listenPort: listenPort !== '' && listenPort !== null ? Number(listenPort) : undefined,
        disableRoutes,
        natDisabled,
        dns: dns || '',
        publicHost: publicHost || '',
        mtu: mtu || 0,
        mss: mss || 0,
      };
      if (protocol === 'amneziawg-2.0') {
        payload.settings = { ...settings };
      }

      try {
        const res = await this.api.updateTunnelInterface({ interfaceId: id, ...payload });
        this._applyInterfaceUpdate(res);
        this.showInterfaceEdit = false;
        this.showToast(`Interface "${name}" updated successfully`);
      } catch (err) {
        console.error('saveInterfaceEdit failed:', err);
        this.showToast(`Failed: ${err.message}`, 'error');
      }
    },

    async deleteTunnelInterface(iface) {
      this.interfaceDelete = iface;
    },

    async confirmDeleteInterface() {
      const iface = this.interfaceDelete;
      if (!iface) return;
      this.interfaceDelete = null;
      try {
        await this.api.deleteTunnelInterface({ interfaceId: iface.id });
        if (this.activeInterfaceId === iface.id) {
          this.activeInterfaceId = null;
          this.selectedInterface = null;
          this.selectedInterfacePeers = [];
        }
        await this.loadTunnelInterfaces();
        this.loadNatInterfaces();
        this.loadFirewallInterfaces();
        this.showToast(`Interface "${iface.name}" deleted`);
      } catch (err) {
        console.error('Delete failed:', err);
        this.showToast(`Failed: ${err.message}`, 'error');
      }
    },

    // Обновить один интерфейс в массиве реактивно (Vue 2 splice).
    // Вызывается после start/stop/restart — данные берём из ответа API,
    // чтобы не делать лишний GET и не зависеть от состояния сети.
    _applyInterfaceUpdate(updatedIface) {
      const idx = this.tunnelInterfaces.findIndex(i => i.id === updatedIface.id);
      if (idx !== -1) {
        this.tunnelInterfaces.splice(idx, 1, updatedIface);
      } else {
        // Интерфейс появился впервые (например после create + immediate start)
        this.tunnelInterfaces.push(updatedIface);
      }
    },

    async startTunnelInterface(iface) {
      if (this.loadingInterfaceId) return; // предотвратить двойной клик
      this.loadingInterfaceId = iface.id;
      try {
        const data = await this.api.startTunnelInterface({ interfaceId: iface.id });
        if (data && data.interface) this._applyInterfaceUpdate(data.interface);
        this.loadNatInterfaces();
        this.loadFirewallInterfaces();
      } catch (err) {
        console.error('Start failed:', err);
        this.showToast(`Start failed: ${err.message}`, 'error');
      } finally {
        this.loadingInterfaceId = null;
      }
    },

    async stopTunnelInterface(iface) {
      if (this.loadingInterfaceId) return;
      this.loadingInterfaceId = iface.id;
      try {
        const data = await this.api.stopTunnelInterface({ interfaceId: iface.id });
        if (data && data.interface) this._applyInterfaceUpdate(data.interface);
        this.loadNatInterfaces();
        this.loadFirewallInterfaces();
      } catch (err) {
        console.error('Stop failed:', err);
        this.showToast(`Stop failed: ${err.message}`, 'error');
      } finally {
        this.loadingInterfaceId = null;
      }
    },

    async restartTunnelInterface(iface) {
      if (this.loadingInterfaceId) return;
      this.loadingInterfaceId = iface.id;
      try {
        const data = await this.api.restartTunnelInterface({ interfaceId: iface.id });
        if (data && data.interface) this._applyInterfaceUpdate(data.interface);
        this.loadNatInterfaces();
        this.loadFirewallInterfaces();
      } catch (err) {
        console.error('Restart failed:', err);
        this.showToast(`Restart failed: ${err.message}`, 'error');
      } finally {
        this.loadingInterfaceId = null;
      }
    },

    async loadInterfacePeers(interfaceId) {
      try {
        const data = await this.api.getTunnelInterfacePeers({ interfaceId });
        this.selectedInterfacePeers = data.peers || [];
      } catch (err) {
        console.error('Failed to load peers:', err);
        this.selectedInterfacePeers = [];
      }
    },

    async createPeer() {
      if (!this.activeInterfaceId) {
        this.showToast('No interface selected', 'error');
        return;
      }

      const { mode, peerType, name, publicKey, endpoint, allowedIPs, clientAllowedIPs, persistentKeepalive, groupId, expiredAt } = this.peerCreate;

      // Validation
      if (!name || name.trim() === '') {
        this.showToast('Please enter a name', 'error');
        return;
      }
      if (mode === 'manual' && !publicKey) {
        this.showToast('Please enter the public key', 'error');
        return;
      }
      // Interconnect requires explicit AllowedIPs (it routes a subnet, not just /32)
      if (peerType === 'interconnect' && !allowedIPs) {
        this.showToast('Please enter Allowed IPs for the interconnect peer (e.g., 192.168.2.0/24)', 'error');
        return;
      }

      // For client+generate with no allowedIPs → auto-allocate /32 from interface subnet
      const autoAllocate = mode === 'generate' && peerType === 'client' && !allowedIPs;

      const payload = {
        name,
        peerType,
        ...(mode === 'generate' ? { generateKeys: true } : { publicKey }),
        ...(autoAllocate ? { autoAllocateIP: true } : { allowedIPs }),
        clientAllowedIPs: clientAllowedIPs || undefined,
        endpoint: endpoint || undefined,
        persistentKeepalive: persistentKeepalive || 25,
        ...(peerType === 'client' && groupId ? { groupId } : {}),
        ...(expiredAt ? { expiredAt } : {}),
      };

      try {
        const res = await this.api.createTunnelInterfacePeer({
          interfaceId: this.activeInterfaceId,
          ...payload,
        });

        const interfaceId = this.activeInterfaceId;
        const peerId = res.peer && res.peer.id;

        const showQR = this.peerCreate.showQR;
        this.showPeerCreate = false;
        this.inlineGroupShow = false;
        this.inlineGroupInput = '';
        this.peerCreate = { mode: 'generate', peerType: 'client', name: '', publicKey: '', endpoint: '', allowedIPs: '', clientAllowedIPs: '', persistentKeepalive: 25, groupId: this.defaultGroupId(), expiredAt: '', showQR: false };

        await this.refreshPeers();
        await this.loadTunnelInterfaces();
        if (peerType === 'client') {
          this.loadClientGroups();
          this.loadAliases();
        }

        if (showQR && mode === 'generate' && peerType === 'client' && peerId) {
          this.qrcode = this.peerQrUrl(interfaceId, peerId);
        } else {
          this.showToast(peerType === 'client' ? 'Client created!' : 'Peer created!');
        }
      } catch (err) {
        console.error('Failed to create peer:', err);
        this.showToast(`Failed: ${err.message}`, 'error');
      }
    },

    async deletePeer(peer) {
      const label = peer.peerType === 'interconnect' ? 'peer' : 'client';
      if (!confirm(`Delete ${label} "${peer.name}"?`)) return;
      try {
        await this.api.deleteTunnelInterfacePeer({
          interfaceId: this.selectedInterface.id,
          peerId: peer.id,
        });
        await this.loadInterfacePeers(this.selectedInterface.id);
        await this.loadTunnelInterfaces();
        this.showToast(peer.peerType === 'interconnect' ? 'Peer deleted!' : 'Client deleted!');
      } catch (err) {
        console.error('Delete failed:', err);
        this.showToast(`Failed: ${err.message}`, 'error');
      }
    },

    async downloadPeerConfig(peer) {
      try {
        const config = await this.api.getPeerConfig({ interfaceId: this._peerIfaceId(peer), peerId: peer.id });
        const blob = new Blob([config], { type: 'text/plain' });
        const url = window.URL.createObjectURL(blob);
        const a = document.createElement('a');
        a.href = url;
        a.download = `${peer.name.replace(/\s+/g, '-')}.conf`;
        document.body.appendChild(a);
        a.click();
        document.body.removeChild(a);
        window.URL.revokeObjectURL(url);
      } catch (err) {
        console.error('Download failed:', err);
        this.showToast(`Failed: ${err.message}`, 'error');
      }
    },

    // ========================================================================
    // Gateways Methods
    // ========================================================================

    async loadGateways() {
      try {
        const res = await this.api.getGateways();
        this.gateways = res.gateways || [];
      } catch (err) {
        console.error('loadGateways error:', err);
      }
    },

    async refreshGateways() {
      if (!this.authenticated) return;
      try {
        const res = await this.api.getGateways();
        this.gateways = res.gateways || [];
      } catch (_) { /* silent poll */ }
    },

    async loadGatewayGroups() {
      try {
        const res = await this.api.getGatewayGroups();
        this.gatewayGroups = res.groups || [];
      } catch (err) {
        console.error('loadGatewayGroups error:', err);
      }
    },

    async loadSystemInterfaces() {
      try {
        const res = await this.api.getSystemInterfaces();
        this.systemInterfaces = res.interfaces || [];
      } catch (err) {
        console.error('loadSystemInterfaces error:', err);
      }
    },

    // ── Create Gateway ────────────────────────────────────────────────────────
    async createGateway() {
      const f = this.gatewayCreate;
      if (!f.name.trim())      return this.showToast('Gateway name is required', 'error');
      if (!f.interface)        return this.showToast('Interface is required', 'error');
      if (!f.gatewayIP.trim()) return this.showToast('Gateway IP is required', 'error');
      try {
        await this.api.createGateway({
          name:             f.name.trim(),
          interface:        f.interface,
          gatewayIP:        f.gatewayIP.trim(),
          monitorAddress:   f.monitorAddress.trim(),
          monitor:          f.monitor,
          monitorInterval:  Number(f.monitorInterval),
          windowSeconds:    f.windowSeconds !== null ? Number(f.windowSeconds) : null,
          latencyThreshold: Number(f.latencyThreshold),
          monitorHttp: {
            enabled:        f.monitorHttp.enabled,
            url:            f.monitorHttp.url.trim(),
            expectedStatus: Number(f.monitorHttp.expectedStatus),
            interval:       Number(f.monitorHttp.interval),
            timeout:        Number(f.monitorHttp.timeout),
          },
          monitorRule:      f.monitorRule,
          description:      f.description.trim(),
        });
        this.showGatewayCreate = false;
        this.gatewayCreate = {
          name: '', interface: '', gatewayIP: '', monitorAddress: '',
          monitor: true, monitorInterval: 5, windowSeconds: null,
          latencyThreshold: 500,
          monitorHttp: { enabled: false, url: '', expectedStatus: 200, interval: 10, timeout: 5 },
          monitorRule: 'icmp_only',
          description: '',
        };
        await this.loadGateways();
      } catch (err) {
        this.showToast(`Failed: ${err.message}`, 'error');
      }
    },

    // ── Edit Gateway ──────────────────────────────────────────────────────────
    openGatewayEdit(gw) {
      const httpDefaults = { enabled: false, url: '', expectedStatus: 200, interval: 10, timeout: 5 };
      this.gatewayEdit = {
        id:               gw.id,
        name:             gw.name,
        interface:        gw.interface,
        gatewayIP:        gw.gatewayIP || '',
        monitorAddress:   gw.monitorAddress || '',
        monitor:          gw.monitor,
        monitorInterval:  gw.monitorInterval,
        windowSeconds:    gw.windowSeconds ?? null,
        latencyThreshold: gw.latencyThreshold || 500,
        monitorHttp:      gw.monitorHttp ? { ...httpDefaults, ...gw.monitorHttp } : { ...httpDefaults },
        monitorRule:      gw.monitorRule || 'icmp_only',
        description:      gw.description || '',
        adminDown:        !!gw.adminDown,
      };
      this.showGatewayEdit = true;
    },

    async saveGatewayEdit() {
      const f = this.gatewayEdit;
      if (!f.name.trim())      return this.showToast('Gateway name is required', 'error');
      if (!f.interface)        return this.showToast('Interface is required', 'error');
      if (!f.gatewayIP.trim()) return this.showToast('Gateway IP is required', 'error');
      try {
        await this.api.updateGateway({
          gatewayId:        f.id,
          name:             f.name.trim(),
          interface:        f.interface,
          gatewayIP:        f.gatewayIP.trim(),
          monitorAddress:   f.monitorAddress.trim(),
          monitor:          f.monitor,
          monitorInterval:  Number(f.monitorInterval),
          windowSeconds:    f.windowSeconds !== null ? Number(f.windowSeconds) : null,
          latencyThreshold: Number(f.latencyThreshold),
          monitorHttp: {
            enabled:        f.monitorHttp.enabled,
            url:            f.monitorHttp.url.trim(),
            expectedStatus: Number(f.monitorHttp.expectedStatus),
            interval:       Number(f.monitorHttp.interval),
            timeout:        Number(f.monitorHttp.timeout),
          },
          monitorRule:      f.monitorRule,
          description:      f.description.trim(),
          adminDown:        f.adminDown,
        });
        this.showGatewayEdit = false;
        const res = await this.api.getGateways();
        this.gateways = res.gateways || [];
      } catch (err) {
        this.showToast(`Failed: ${err.message}`, 'error');
      }
    },

    // ── Toggle Admin Down ─────────────────────────────────────────────────────
    async toggleGatewayAdminDown(gw) {
      const newVal = !gw.adminDown;
      // Optimistic update: flip adminDown and status locally for instant feedback
      const idx = this.gateways.findIndex(g => g.id === gw.id);
      if (idx !== -1) {
        const updated = { ...this.gateways[idx], adminDown: newVal };
        if (newVal) {
          updated.realStatus = updated.status; // preserve real status for ring
          updated.status = 'admin_down';
        } else {
          updated.status = updated.realStatus || 'unknown';
          updated.realStatus = '';
        }
        this.gateways.splice(idx, 1, updated);
      }
      try {
        await this.api.updateGateway({
          gatewayId:        gw.id,
          name:             gw.name,
          interface:        gw.interface,
          gatewayIP:        gw.gatewayIP,
          monitorAddress:   gw.monitorAddress || '',
          monitor:          gw.monitor,
          monitorInterval:  gw.monitorInterval,
          windowSeconds:    gw.windowSeconds ?? null,
          latencyThreshold: gw.latencyThreshold || 500,
          monitorHttp:      gw.monitorHttp || {},
          monitorRule:      gw.monitorRule || 'icmp_only',
          description:      gw.description || '',
          adminDown:        newVal,
        });
        // Next polling cycle will bring fresh realStatus from server
      } catch (err) {
        // Revert on error
        const res = await this.api.getGateways();
        this.gateways = res.gateways || [];
        this.showToast(`Failed: ${err.message}`, 'error');
      }
    },

    // ── Delete Gateway ────────────────────────────────────────────────────────
    async deleteGateway(gw) {
      if (!confirm(`Delete gateway "${gw.name}"?`)) return;
      try {
        await this.api.deleteGateway({ gatewayId: gw.id });
        const res = await this.api.getGateways();
        this.gateways = res.gateways || [];
      } catch (err) {
        this.showToast(`Failed: ${err.message}`, 'error');
      }
    },

    // ── Create Gateway Group ──────────────────────────────────────────────────
    async createGatewayGroup() {
      const f = this.groupCreate;
      if (!f.name.trim()) return this.showToast('Group name is required', 'error');
      try {
        await this.api.createGatewayGroup({
          name:        f.name.trim(),
          trigger:     f.trigger,
          description: f.description.trim(),
          gateways:    f.gateways,
        });
        this.showGroupCreate = false;
        this.groupCreate = { name: '', trigger: 'packetloss', description: '', gateways: [] };
        await this.loadGatewayGroups();
      } catch (err) {
        this.showToast(`Failed: ${err.message}`, 'error');
      }
    },

    // ── Edit Gateway Group ────────────────────────────────────────────────────
    openGroupEdit(grp) {
      this.groupEdit = {
        id:          grp.id,
        name:        grp.name,
        trigger:     grp.trigger,
        description: grp.description || '',
        gateways:    JSON.parse(JSON.stringify(grp.gateways || [])),
      };
      this.showGroupEdit = true;
    },

    async saveGroupEdit() {
      const f = this.groupEdit;
      if (!f.name.trim()) return this.showToast('Group name is required', 'error');
      try {
        await this.api.updateGatewayGroup({
          groupId:     f.id,
          name:        f.name.trim(),
          trigger:     f.trigger,
          description: f.description.trim(),
          gateways:    f.gateways,
        });
        this.showGroupEdit = false;
        const res = await this.api.getGatewayGroups();
        this.gatewayGroups = res.groups || [];
      } catch (err) {
        this.showToast(`Failed: ${err.message}`, 'error');
      }
    },

    // ── Delete Gateway Group ──────────────────────────────────────────────────
    async deleteGatewayGroup(grp) {
      if (!confirm(`Delete gateway group "${grp.name}"?`)) return;
      try {
        await this.api.deleteGatewayGroup({ groupId: grp.id });
        const res = await this.api.getGatewayGroups();
        this.gatewayGroups = res.groups || [];
      } catch (err) {
        this.showToast(`Failed: ${err.message}`, 'error');
      }
    },

    // ── Remote servers ────────────────────────────────────────────────────────

    async loadRemotes() {
      if (!this.authenticated) return;
      try {
        const res = await this.api.getRemotes();
        this.remotes = res.remotes || [];
      } catch (err) {
        this.showToast(`Failed to load remotes: ${err.message}`, 'error');
      }
    },

    async switchToRemote(remote) {
      this.api.setRemote(remote.id);
      this.activeRemoteId = remote.id;
      // Reset current page data so stale local data isn't shown.
      this.tunnelInterfaces = [];
      this.allPeers = [];
      this.gateways = [];
      this.natRules = [];
      this.dnatRules = [];
      this.natInterfaces = [];
      this.staticRoutes = [];
      this.kernelRoutes = [];
      this.firewallRules = [];
      this.aliases = [];
      this.switchPage('dashboard');
      try {
        await Promise.all([this.loadTunnelInterfaces(), this.loadSettings()]);
      } catch (err) {
        this.showToast(`Failed to connect to ${remote.name}: ${err.message}`, 'error');
        this.switchToLocal();
      }
    },

    switchToLocal() {
      this.api.clearRemote();
      this.activeRemoteId = null;
      this.tunnelInterfaces = [];
      this.allPeers = [];
      this.gateways = [];
      this.natRules = [];
      this.dnatRules = [];
      this.natInterfaces = [];
      this.staticRoutes = [];
      this.kernelRoutes = [];
      this.firewallRules = [];
      this.aliases = [];
      this.switchPage('dashboard');
      // Must reload interfaces explicitly: refreshAllPeers() iterates
      // tunnelInterfaces, so it returns nothing until they are populated.
      this.loadTunnelInterfaces();
      this.loadSettings();
    },

    async addRemote() {
      this.remoteAddError = '';
      this.remoteAddLoading = true;
      try {
        const res = await this.api.addRemote(this.remoteAddForm);
        // Server returned totp_required: true — show TOTP step.
        if (res && res.totp_required) {
          this.remoteAddNeedsTOTP = true;
          this.remoteAddForm.totpCode = '';
          return;
        }
        this.remotes.push(res.remote);
        this.showRemoteAdd = false;
        this.remoteAddForm = { name: '', url: '', mode: 'login', username: '', password: '', totpCode: '', token: '' };
        this.remoteAddNeedsTOTP = false;
        this.showToast('Remote server added', 'success');
      } catch (err) {
        this.remoteAddError = err.message;
      } finally {
        this.remoteAddLoading = false;
      }
    },

    async deleteRemote(remote) {
      if (!confirm(`Remove remote server "${remote.name}"?`)) return;
      try {
        await this.api.deleteRemote({ id: remote.id });
        this.remotes = this.remotes.filter(r => r.id !== remote.id);
        this.showToast('Remote server removed', 'success');
      } catch (err) {
        this.showToast(`Failed: ${err.message}`, 'error');
      }
    },

    async testRemote(remote) {
      this.$set(this.remoteTesting, remote.id, true);
      this.$set(this.remoteTestResult, remote.id, null);
      try {
        await this.api.testRemote({ id: remote.id });
        this.$set(this.remoteTestResult, remote.id, 'ok');
      } catch (err) {
        this.$set(this.remoteTestResult, remote.id, 'error');
      } finally {
        this.$set(this.remoteTesting, remote.id, false);
      }
    },

    // ── Speed test ────────────────────────────────────────────────────────────

    openSpeedtest() {
      this.speedtestResult = null;
      this.speedtestError = '';
      this.speedtest.fromId = this.activeRemoteId || '__local__';
      this.speedtest.toId = this.remotes.length > 0 ? this.remotes[0].id : '__local__';
      this.speedtest.via = 'auto';
      this.speedtest.tunnelIp = '';
      this.speedtestDetectedTunnelIp = '';
      this.speedtestFromIfaces = [];
      this.speedtestToIfaces = [];
      this.speedtestFromIfaceId = '';
      this.speedtestToIfaceId = '';
      this.showSpeedtest = true;
      this.loadSpeedtestHistory();
      this.onSpeedtestServersChange();
    },

    async onSpeedtestServersChange() {
      this.speedtestDetectedTunnelIp = '';
      this.speedtestFromIfaces = [];
      this.speedtestToIfaces = [];
      this.speedtestFromIfaceId = '';
      this.speedtestToIfaceId = '';
      this.speedtestError = '';
      if (this.speedtest.fromId === this.speedtest.toId) return;
      try {
        const [ip, fromIfaces, toIfaces] = await Promise.all([
          this._findTunnelIP(this.speedtest.fromId, this.speedtest.toId),
          this._getIfacesFor(this.speedtest.fromId),
          this._getIfacesFor(this.speedtest.toId),
        ]);
        this.speedtestDetectedTunnelIp = ip || '';
        this.speedtestFromIfaces = fromIfaces;
        this.speedtestToIfaces = toIfaces;
        if (fromIfaces.length) this.speedtestFromIfaceId = fromIfaces[0].id;
        if (toIfaces.length) this.speedtestToIfaceId = toIfaces[0].id;
      } catch (e) {
        this.speedtestError = 'Failed to load interfaces: ' + (e.message || e);
      }
    },

    _speedtestServerName(id) {
      if (id === '__local__') return this.localServerName || this.pageTitle || 'Local';
      const r = this.remotes.find(r => r.id === id);
      return r ? r.name : id;
    },

    _speedtestPublicHost(fromId) {
      if (fromId === '__local__') {
        return this.globalSettings.resolvedPublicIP || this.globalSettings.publicIP || '';
      }
      const remote = this.remotes.find(r => r.id === fromId);
      if (!remote) return '';
      try { return new URL(remote.url).hostname; } catch (_) { return ''; }
    },

    // _ipInCIDR returns true if ip (string) falls within cidr (string), excluding /0.
    _ipInCIDR(ip, cidr) {
      const parts = ip.split('.').map(Number);
      if (parts.length !== 4 || parts.some(p => isNaN(p))) return false;
      const ip32 = (parts[0] << 24 | parts[1] << 16 | parts[2] << 8 | parts[3]) >>> 0;
      const n = this._cidrNetwork(cidr);
      if (!n || n.prefix === 0) return false; // exclude default route
      return ((ip32 & n.mask) >>> 0) === n.net;
    },

    // _cidrNetwork returns the network address and prefix length for a CIDR string.
    _cidrNetwork(cidr) {
      const [addr, bits] = cidr.split('/');
      if (!addr || bits === undefined) return null;
      const prefix = parseInt(bits, 10);
      if (isNaN(prefix)) return null;
      const parts = addr.split('.').map(Number);
      if (parts.length !== 4 || parts.some(p => isNaN(p))) return null;
      const ip32 = (parts[0] << 24 | parts[1] << 16 | parts[2] << 8 | parts[3]) >>> 0;
      const mask = prefix === 0 ? 0 : (0xffffffff << (32 - prefix)) >>> 0;
      return { net: (ip32 & mask) >>> 0, mask, ip32, prefix };
    },

    // _sameSubnet returns true if ipA/prefixA and ipB/prefixB share a subnet.
    _sameSubnet(cidrA, cidrB) {
      const a = this._cidrNetwork(cidrA);
      const b = this._cidrNetwork(cidrB);
      if (!a || !b) return false;
      const mask = Math.min(a.prefix, b.prefix) === 0 ? 0
        : (0xffffffff << (32 - Math.min(a.prefix, b.prefix))) >>> 0;
      return ((a.ip32 & mask) >>> 0) === ((b.ip32 & mask) >>> 0);
    },

    // _getIfacesFor loads active tunnel interfaces from a server (local or remote).
    async _getIfacesFor(serverId) {
      const data = serverId === '__local__'
        ? await this.api.getTunnelInterfaces()
        : await this.api.remoteCall({ remoteId: serverId, method: 'get', path: '/tunnel-interfaces' });
      return (data.interfaces || []).filter(i => i.enabled && i.address);
    },

    // _getPeersFor loads peers for one interface on a server.
    async _getPeersFor(serverId, ifaceId) {
      const path = `/tunnel-interfaces/${ifaceId}/peers`;
      const data = serverId === '__local__'
        ? await this.api.call({ method: 'get', path })
        : await this.api.remoteCall({ remoteId: serverId, method: 'get', path });
      return data.peers || [];
    },

    // _findTunnelIP finds a common subnet between interfaces on two servers.
    // If server A has 10.0.1.1/30 and server B has 10.0.1.2/30 — same subnet → S2S.
    // Returns the "from" interface IP, or null if no match found.
    async _findTunnelIP(fromId, toId) {
      try {
        const [fromIfaces, toIfaces] = await Promise.all([
          this._getIfacesFor(fromId),
          this._getIfacesFor(toId),
        ]);
        const fromS2S = fromIfaces.filter(i => i.disableRoutes);
        const toS2S = toIfaces.filter(i => i.disableRoutes);
        for (const fi of fromS2S) {
          for (const ti of toS2S) {
            if (this._sameSubnet(fi.address, ti.address)) {
              return fi.address.split('/')[0];
            }
          }
        }
      } catch (_) {}
      return null;
    },

    async runSpeedtest() {
      if (this.speedtestRunning) return;
      const { fromId, toId, duration, streams } = this.speedtest;
      if (fromId === toId) {
        this.speedtestError = 'Source and destination must be different servers.';
        return;
      }
      const host = this._speedtestPublicHost(fromId);
      if (!host) {
        this.speedtestError = 'Cannot determine IP address of source server. Set Public IP in Settings.';
        return;
      }

      this.speedtestRunning = true;
      this.speedtestResult = null;
      this.speedtestError = '';
      this.speedtestPingConfirm = false;

      try {
        let resolvedHost = host;
        let via = 'internet';

        if (this.speedtest.via === 'manual') {
          const iface = this.speedtestFromIfaces.find(i => i.id === this.speedtestFromIfaceId);
          if (!iface) {
            this.speedtestError = 'Select a source interface.';
            this.speedtestRunning = false;
            return;
          }
          resolvedHost = iface.address.split('/')[0];
          via = 'manual';
        } else if (this.speedtest.via === 'tunnel') {
          const ip = this.speedtest.tunnelIp.trim() || this.speedtestDetectedTunnelIp;
          if (!ip) {
            this.speedtestError = 'No S2S tunnel found. Switch to Manual or Internet mode.';
            this.speedtestRunning = false;
            return;
          }
          resolvedHost = ip;
          via = 'tunnel';
        } else if (this.speedtest.via === 'internet') {
          resolvedHost = host;
          via = 'internet';
        } else {
          // auto
          const tunnelIP = await this._findTunnelIP(fromId, toId);
          resolvedHost = tunnelIP || host;
          via = tunnelIP ? 'tunnel' : 'internet';
        }

        // Ping check: from server pings the resolved host.
        // For internet mode warn softly (ICMP may be blocked), others warn harder.
        if (via !== 'internet') {
          const fromRemoteId = fromId === '__local__' ? null : fromId;
          try {
            const pingRes = await this.api.ping({ host: resolvedHost, count: 3, remoteId: fromRemoteId });
            if (!pingRes.reachable) {
              this.speedtestPendingHost = resolvedHost;
              this.speedtestPendingVia = via;
              this.speedtestPingConfirmMsg = `${resolvedHost} is not reachable from the source server (ICMP). The speed test may fail.`;
              this.speedtestPingConfirm = true;
              this.speedtestRunning = false;
              return;
            }
          } catch (_) { /* ping endpoint unavailable — proceed */ }
        } else {
          // Internet mode: soft check
          const fromRemoteId = fromId === '__local__' ? null : fromId;
          try {
            const pingRes = await this.api.ping({ host: resolvedHost, count: 2, remoteId: fromRemoteId });
            if (!pingRes.reachable) {
              this.speedtestPendingHost = resolvedHost;
              this.speedtestPendingVia = via;
              this.speedtestPingConfirmMsg = `${resolvedHost} does not respond to ICMP — it may be blocked by firewall. The test may still work.`;
              this.speedtestPingConfirm = true;
              this.speedtestRunning = false;
              return;
            }
          } catch (_) {}
        }

        const toIface = this.speedtest.via === 'manual'
          ? this.speedtestToIfaces.find(i => i.id === this.speedtestToIfaceId)
          : null;
        const { jobId } = await this.api.speedtestRun({
          fromServer: this._speedtestServerName(fromId),
          toServer: this._speedtestServerName(toId),
          fromRemoteId: fromId === '__local__' ? '' : fromId,
          toRemoteId: toId === '__local__' ? '' : toId,
          host: resolvedHost,
          bindAddr: toIface ? toIface.address.split('/')[0] : '',
          via,
          duration: Number(duration),
          streams: Number(streams),
        });

        // Poll until done or error.
        for (;;) {
          await new Promise(r => setTimeout(r, 2000));
          const rec = await this.api.speedtestGetResult(jobId);
          if (rec.status === 'running') continue;
          if (rec.status === 'error') {
            this.speedtestError = rec.error || 'Speed test failed.';
          } else {
            this.speedtestResult = rec;
          }
          break;
        }
      } catch (err) {
        this.speedtestError = err.message || 'Speed test failed.';
      } finally {
        this.speedtestRunning = false;
        this.loadSpeedtestHistory();
      }
    },

    async confirmAndRunSpeedtest() {
      this.speedtestPingConfirm = false;
      // Re-enter runSpeedtest but skip ping check by temporarily overriding flag.
      const { fromId, toId, duration, streams } = this.speedtest;
      const host = this._speedtestPublicHost(fromId);
      this.speedtestRunning = true;
      this.speedtestResult = null;
      this.speedtestError = '';
      try {
        const resolvedHost = this.speedtestPendingHost;
        const via = this.speedtestPendingVia;
        const toIface2 = via === 'manual'
          ? this.speedtestToIfaces.find(i => i.id === this.speedtestToIfaceId)
          : null;
        const { jobId } = await this.api.speedtestRun({
          fromServer: this._speedtestServerName(fromId),
          toServer: this._speedtestServerName(toId),
          fromRemoteId: fromId === '__local__' ? '' : fromId,
          toRemoteId: toId === '__local__' ? '' : toId,
          host: resolvedHost,
          bindAddr: toIface2 ? toIface2.address.split('/')[0] : '',
          via,
          duration: Number(duration),
          streams: Number(streams),
        });
        for (;;) {
          await new Promise(r => setTimeout(r, 2000));
          const rec = await this.api.speedtestGetResult(jobId);
          if (rec.status === 'running') continue;
          if (rec.status === 'error') this.speedtestError = rec.error || 'Speed test failed.';
          else this.speedtestResult = rec;
          break;
        }
      } catch (err) {
        this.speedtestError = err.message || 'Speed test failed.';
      } finally {
        this.speedtestRunning = false;
        this.loadSpeedtestHistory();
      }
    },

    async loadSpeedtestHistory() {
      try {
        const { results } = await this.api.speedtestListResults();
        this.speedtestHistory = results || [];
      } catch (_) {}
    },

    async clearSpeedtestHistory() {
      try {
        await this.api.speedtestClearResults();
        this.speedtestHistory = [];
      } catch (err) {
        this.showToast(err.message, 'error');
      }
    },

    // ── Group gateways entry helpers ──────────────────────────────────────────
    addGroupGatewayEntry(form) {
      form.gateways.push({ gatewayId: '', tier: 1, weight: 100 });
    },

    removeGroupGatewayEntry(form, idx) {
      form.gateways.splice(idx, 1);
    },

    // ── Status helpers ────────────────────────────────────────────────────────
    gatewayStatusColor(status) {
      const map = { healthy: '#22c55e', degraded: '#eab308', down: '#ef4444', unknown: '#9ca3af', admin_down: '#6b7280' };
      return map[status] || map.unknown;
    },

    gatewayStatusLabel(status) {
      const map = { healthy: 'Healthy', degraded: 'Degraded', down: 'Down', unknown: 'Unknown', admin_down: 'Admin Down' };
      return map[status] || 'Unknown';
    },

    // Look up gateway name by id (used in group display)
    gatewayNameById(id) {
      const gw = this.gateways.find(g => g.id === id);
      return gw ? gw.name : id;
    },

    gatewayStatusById(id) {
      const gw = this.gateways.find(g => g.id === id);
      return gw ? gw.status : 'unknown';
    },

    // ========================================================================
    // Routing Methods
    // ========================================================================

    switchRoutingTab(tab) {
      this.activeRoutingTab = tab;
      if (tab === 'status') this.loadKernelRoutes();
    },

    async loadRoutingTables() {
      try {
        const res = await this.api.getRoutingTables();
        this.routingTables = res.tables || [];
        // Установить дефолтную таблицу = 'main' если она есть
        if (this.routingTables.length > 0 && !this.routingTables.find(t => t.name === this.routingTable)) {
          this.routingTable = this.routingTables[0].name;
        }
      } catch (err) {
        console.error('loadRoutingTables error:', err);
        this.routingTables = [{ id: 254, name: 'main' }, { id: null, name: 'all' }];
      }
    },

    async loadKernelRoutes() {
      this.kernelRoutesError = '';
      this.kernelRoutesLoading = true;
      try {
        const res = await this.api.getKernelRoutes(this.routingTable);
        this.kernelRoutes = res.routes || [];
      } catch (err) {
        console.error('loadKernelRoutes error:', err);
        this.kernelRoutesError = err.message || 'Failed to load kernel routes';
        this.kernelRoutes = [];
      } finally {
        this.kernelRoutesLoading = false;
      }
    },

    async loadStaticRoutes() {
      try {
        const res = await this.api.getStaticRoutes();
        this.staticRoutes = res.routes || [];
      } catch (err) {
        console.error('loadStaticRoutes error:', err);
      }
    },

    async testRoute() {
      if (!this.routeTestIp) return;
      this.routeTestLoading = true;
      this.routeTestResult = null;
      this.routeTestMatchedRule = null;
      this.routeTestSteps = [];
      this.routeTestError = '';
      try {
        const res = await this.api.testRoute(this.routeTestIp, this.routeTestSrc || undefined);
        this.routeTestResult      = res.result;
        this.routeTestMatchedRule = res.matchedRule || null;
        this.routeTestSteps       = res.steps || [];
      } catch (err) {
        this.routeTestError = err.message || 'Error';
      } finally {
        this.routeTestLoading = false;
      }
    },

    /**
     * Отображаемый лейбл для gateway IP в результатах Route Lookup.
     * Если IP совпадает с известным гейтвеем — добавляет его имя в скобках.
     * Если не найден — помечает "(default gateway)".
     */
    _routeGatewayLabel(ip) {
      if (!ip) return '—';
      const gw = (this.gateways || []).find(g => g.gatewayIP === ip);
      if (gw) return `${ip} (${gw.name})`;
      return `${ip} (default gateway)`;
    },

    async createRoute() {
      try {
        const data = {
          description: this.routeCreate.description,
          destination: this.routeCreate.destination,
          metric: this.routeCreate.metric !== '' ? Number(this.routeCreate.metric) : null,
          table: this.routeCreate.table || 'main',
        };
        if (this.routeCreate.viaMode === 'gateway') {
          data.gatewayId = this.routeCreate.gatewayId;
        } else if (this.routeCreate.viaMode === 'group') {
          data.gatewayGroupId = this.routeCreate.gatewayGroupId;
        } else {
          data.gateway = this.routeCreate.gateway;
          data.dev = this.routeCreate.dev;
        }
        await this.api.createStaticRoute(data);
        this.showRouteCreate = false;
        this.routeCreate = {
          description: '', destination: '',
          viaMode: 'manual', gateway: '', dev: '',
          gatewayId: '', gatewayGroupId: '',
          metric: '', table: 'main',
        };
        await this.loadStaticRoutes();
      } catch (err) {
        this.showToast(err.message || 'Failed to create route', 'error');
      }
    },

    // Helper: display label for next-hop column in static routes table
    _routeNextHopLabel(route) {
      if (route.gatewayGroupId) {
        const grp = (this.gatewayGroups || []).find(g => g.id === route.gatewayGroupId);
        return grp ? grp.name : route.gatewayGroupId;
      }
      if (route.gatewayId) {
        const gw = (this.gateways || []).find(g => g.id === route.gatewayId);
        return gw ? gw.name : route.gatewayId;
      }
      return route.gateway || '—';
    },

    // Helper: badge type for next-hop column
    _routeNextHopType(route) {
      if (route.gatewayGroupId) return 'group';
      if (route.gatewayId) return 'gateway';
      return 'manual';
    },

    // Helper: determine viaMode from a saved route object
    _routeViaMode(route) {
      if (route.gatewayGroupId) return 'group';
      if (route.gatewayId) return 'gateway';
      return 'manual';
    },

    openEditRoute(route) {
      this.routeEdit = {
        id:            route.id,
        description:   route.description || '',
        destination:   route.destination || '',
        viaMode:       this._routeViaMode(route),
        gateway:       route.gateway || '',
        dev:           route.dev || '',
        gatewayId:     route.gatewayId || '',
        gatewayGroupId: route.gatewayGroupId || '',
        metric:        route.metric != null ? String(route.metric) : '',
        table:         route.table || 'main',
      };
      this.showRouteEdit = true;
    },

    async saveEditRoute() {
      try {
        const data = {
          description: this.routeEdit.description,
          destination: this.routeEdit.destination,
          metric: this.routeEdit.metric !== '' ? Number(this.routeEdit.metric) : null,
          table: this.routeEdit.table || 'main',
        };
        if (this.routeEdit.viaMode === 'gateway') {
          data.gatewayId = this.routeEdit.gatewayId;
        } else if (this.routeEdit.viaMode === 'group') {
          data.gatewayGroupId = this.routeEdit.gatewayGroupId;
        } else {
          data.gateway = this.routeEdit.gateway;
          data.dev = this.routeEdit.dev;
        }
        await this.api.updateStaticRoute({ routeId: this.routeEdit.id, data });
        this.showRouteEdit = false;
        await this.loadStaticRoutes();
        this.showToast('Route updated');
      } catch (err) {
        this.showToast(err.message || 'Failed to update route', 'error');
      }
    },

    async toggleRoute(id, enabled) {
      try {
        await this.api.toggleStaticRoute({ routeId: id, enabled });
        await this.loadStaticRoutes();
      } catch (err) {
        this.showToast(err.message || 'Failed to toggle route', 'error');
      }
    },

    async deleteRoute(id) {
      if (!confirm('Delete this route?')) return;
      try {
        await this.api.deleteStaticRoute({ routeId: id });
        await this.loadStaticRoutes();
      } catch (err) {
        this.showToast(err.message || 'Failed to delete route', 'error');
      }
    },

    // ========================================================================
    // NAT Methods
    // ========================================================================

    switchNatTab(tab) {
      this.activeNatTab = tab;
      if (tab === 'portforward' && this.dnatRules.length === 0) {
        this.loadDnatRules();
      }
    },

    // ── Port Forwarding (DNAT) ────────────────────────────────────────────────

    async loadDnatRules() {
      this.dnatLoading = true;
      try {
        const res = await this.api.call({ method: 'GET', path: '/nat/dnat' });
        this.dnatRules = res.rules || [];
      } catch (e) {
        this.showToast('error', e.message || 'Failed to load port forwarding rules');
      } finally {
        this.dnatLoading = false;
      }
    },

    // portInPool returns true if port is within any range/entry in the portPool string.
    // portPool format: "51831-65535" | "433-442,8080,51831-65535"
    // Returns true (no warning) when portPool is empty/unset.
    portInPool(port) {
      const pool = (this.globalSettings && this.globalSettings.portPool) || '';
      if (!pool.trim()) return true;
      const p = parseInt(port);
      if (!p || p < 1 || p > 65535) return true; // let backend validate the value itself
      for (const part of pool.split(',')) {
        const s = part.trim();
        const dash = s.indexOf('-');
        if (dash > 0) {
          const lo = parseInt(s.slice(0, dash));
          const hi = parseInt(s.slice(dash + 1));
          if (p >= lo && p <= hi) return true;
        } else {
          if (parseInt(s) === p) return true;
        }
      }
      return false;
    },

    openAddDnat() {
      this.dnatEditMode = false;
      this.dnatForm = { id: '', name: '', protocol: 'udp', inInterface: '', inPort: '', dest: '', destPort: '', masquerade: true, comment: '' };
      this.showDnatModal = true;
    },

    openEditDnat(rule) {
      this.dnatEditMode = true;
      this.dnatForm = {
        id: rule.id,
        name: rule.name,
        protocol: rule.protocol,
        inInterface: rule.inInterface || '',
        inPort: rule.inPort,
        dest: rule.dest || rule.destIP,  // fallback to destIP for old rules migrated without dest
        destPort: rule.destPort || '',
        masquerade: rule.masquerade !== false, // default true for old rules without the field
        comment: rule.comment || '',
      };
      this.showDnatModal = true;
    },

    async saveDnat() {
      const body = {
        name: this.dnatForm.name,
        protocol: this.dnatForm.protocol,
        inInterface: this.dnatForm.inInterface || '',
        inPort: parseInt(this.dnatForm.inPort) || 0,
        dest: this.dnatForm.dest,
        destPort: parseInt(this.dnatForm.destPort) || 0,
        masquerade: !!this.dnatForm.masquerade,
        comment: this.dnatForm.comment,
      };
      try {
        if (this.dnatEditMode) {
          await this.api.call({ method: 'PATCH', path: `/nat/dnat/${this.dnatForm.id}`, body });
          this.showToast('success', 'Rule updated');
        } else {
          await this.api.call({ method: 'POST', path: '/nat/dnat', body });
          this.showToast('success', 'Rule created');
        }
        this.showDnatModal = false;
        await this.loadDnatRules();
      } catch (e) {
        this.showToast('error', e.message || 'Failed to save rule');
      }
    },

    async toggleDnat(rule) {
      try {
        await this.api.call({ method: 'PATCH', path: `/nat/dnat/${rule.id}`, body: { enabled: !rule.enabled } });
        await this.loadDnatRules();
      } catch (e) {
        this.showToast('error', e.message || 'Failed to toggle rule');
      }
    },

    async deleteDnat(rule) {
      if (!confirm(`Delete "${rule.name}"?`)) return;
      try {
        await this.api.call({ method: 'DELETE', path: `/nat/dnat/${rule.id}` });
        this.showToast('success', 'Rule deleted');
        await this.loadDnatRules();
      } catch (e) {
        this.showToast('error', e.message || 'Failed to delete rule');
      }
    },

    // Navigate to the interface page for an auto NAT rule
    goToInterface(interfaceId) {
      this.activePage = 'interfaces';
      this.activeInterfaceId = interfaceId;
    },

    async loadNatInterfaces() {
      try {
        const res = await this.api.getNatInterfaces();
        this.natInterfaces = res.interfaces || [];
        // Если выходной интерфейс не выбран — выбираем первый непустой
        if (!this.natRuleCreate.outInterface && this.natInterfaces.length > 0) {
          this.natRuleCreate.outInterface = this.natInterfaces[0].name;
        }
      } catch (err) {
        console.error('loadNatInterfaces error:', err);
        this.natInterfaces = [];
      }
    },

    async loadNatRules() {
      this.natRulesLoading = true;
      try {
        const res = await this.api.getNatRules();
        this.natRules = res.rules || [];
      } catch (err) {
        console.error('loadNatRules error:', err);
        this.natRules = [];
      } finally {
        this.natRulesLoading = false;
      }
    },

    /**
     * Открыть модал редактирования NAT правила.
     * Конвертирует rule.source в sourceType + sourceValue для UI.
     */
    openNatRuleEdit(rule) {
      let sourceType = 'any';
      let sourceValue = '';
      let sourceAliasId = '';
      if (rule.sourceAliasId) {
        sourceType = 'alias';
        sourceAliasId = rule.sourceAliasId;
      } else if (rule.source) {
        sourceType = rule.source.includes('/') ? 'subnet' : 'ip';
        sourceValue = rule.source;
      }
      this.natRuleEdit = {
        id:            rule.id,
        name:          rule.name,
        sourceType,
        sourceValue,
        sourceAliasId,
        outInterface:  rule.outInterface,
        type:          rule.type,
        toSource:      rule.toSource || '',
        comment:       rule.comment || '',
      };
      this.showNatRuleEdit = true;
    },

    /**
     * Вычислить итоговое значение source из полей формы.
     * sourceType='any'    → '' (без -s в iptables)
     * sourceType='alias'  → '' (source пуст, sourceAliasId заполнен)
     * sourceType='subnet' → sourceValue (CIDR)
     * sourceType='ip'     → sourceValue (single IP)
     */
    _natFormSource(form) {
      if (form.sourceType === 'any' || form.sourceType === 'alias') return '';
      return (form.sourceValue || '').trim();
    },

    /** Алиасы, применимые как L3 source в NAT (host/network/group/ipset, без port/port-group). */
    _natIpAliases() {
      return (this.aliases || []).filter(a =>
        ['host', 'network', 'group', 'ipset'].includes(a.type)
      );
    },

    /**
     * Отображаемая строка source для таблицы правил.
     */
    _natRuleSourceLabel(rule) {
      if (!rule.source) return 'any';
      return rule.source;
    },

    /**
     * Отображаемая строка type для таблицы правил.
     */
    _natRuleTypeLabel(rule) {
      if (rule.type === 'MASQUERADE') return 'MASQUERADE';
      return `SNAT → ${rule.toSource}`;
    },

    async createNatRule() {
      try {
        const data = {
          name:          this.natRuleCreate.name,
          source:        this._natFormSource(this.natRuleCreate),
          sourceAliasId: this.natRuleCreate.sourceType === 'alias' ? this.natRuleCreate.sourceAliasId : null,
          outInterface:  this.natRuleCreate.outInterface,
          type:          this.natRuleCreate.type,
          toSource:      this.natRuleCreate.type === 'SNAT' ? this.natRuleCreate.toSource : null,
          comment:       this.natRuleCreate.comment,
        };
        await this.api.createNatRule(data);
        // Сброс формы
        this.showNatRuleCreate = false;
        this.natRuleCreate = {
          name: '', sourceType: 'any', sourceValue: '', sourceAliasId: '',
          outInterface: this.natInterfaces.length > 0 ? this.natInterfaces[0].name : '',
          type: 'MASQUERADE', toSource: '', comment: '',
        };
        await this.loadNatRules();
        this.showToast('NAT rule created', 'success');
      } catch (err) {
        this.showToast(err.message || 'Failed to create NAT rule', 'error');
      }
    },

    async saveNatRule() {
      try {
        const data = {
          name:          this.natRuleEdit.name,
          source:        this._natFormSource(this.natRuleEdit),
          sourceAliasId: this.natRuleEdit.sourceType === 'alias' ? this.natRuleEdit.sourceAliasId : null,
          outInterface:  this.natRuleEdit.outInterface,
          type:          this.natRuleEdit.type,
          toSource:      this.natRuleEdit.type === 'SNAT' ? this.natRuleEdit.toSource : null,
          comment:       this.natRuleEdit.comment,
        };
        await this.api.updateNatRule({ ruleId: this.natRuleEdit.id, ...data });
        this.showNatRuleEdit = false;
        await this.loadNatRules();
        this.showToast('NAT rule updated', 'success');
      } catch (err) {
        this.showToast(err.message || 'Failed to update NAT rule', 'error');
      }
    },

    async toggleNatRule(rule) {
      try {
        await this.api.toggleNatRule({ ruleId: rule.id, enabled: !rule.enabled });
        await this.loadNatRules();
      } catch (err) {
        this.showToast(err.message || 'Failed to toggle NAT rule', 'error');
      }
    },

    async deleteNatRule(rule) {
      if (!confirm(`Delete NAT rule "${rule.name}"?`)) return;
      try {
        await this.api.deleteNatRule({ ruleId: rule.id });
        await this.loadNatRules();
        this.showToast('NAT rule deleted', 'success');
      } catch (err) {
        this.showToast(err.message || 'Failed to delete NAT rule', 'error');
      }
    },

    // ========================================================================
    // Firewall Aliases Methods
    // ========================================================================

    async loadAliases() {
      this.aliasesLoading = true;
      try {
        const res = await this.api.getAliases();
        this.aliases = Array.isArray(res) ? res : (res.aliases || []);
      } catch (err) {
        console.error('loadAliases error:', err);
        this.aliases = [];
      } finally {
        this.aliasesLoading = false;
      }
    },

    // ── Dashboard ──────────────────────────────────────────────────────────────

    async loadDashboard() {
      try {
        const res = await this.api.getDashboardWidgets();
        const saved = res.widgets || [];
        this.dashWidgets = saved.length > 0 ? saved : this.dashDefaultWidgets();
      } catch (e) {
        this.dashWidgets = this.dashDefaultWidgets();
      }
      // Pre-load NAT + aliases data (aliases needed to resolve sourceAliasId names)
      if (this.natRules.length === 0) this.loadNatRules();
      if (this.dnatRules.length === 0) this.loadDnatRules();
      if (this.aliases.length === 0) this.loadAliases();
      // Restore saved periods and load history for monitoring widgets
      for (const w of this.dashWidgets) {
        if (w.type !== 'monitoring') continue;
        // Restore persisted period (saved in w.period field)
        if (w.period) this.$set(this.metricsWidgetPeriod, w.id, w.period);
        const period = this.metricsWidgetPeriod[w.id] || '5m';
        if (period !== '5m') {
          for (const key of (w.graphs || [])) {
            if (key.startsWith('gateway:')) this.metricsLoadGatewayDist(w.id, key, period);
            else this.metricsLoadHistory(w.id, key, period);
          }
        }
      }
      await this.$nextTick();
      this.dashInitGrid();
    },

    dashInitGrid() {
      if (this.dashGrid) {
        this.dashGrid.destroy(false);
        this.dashGrid = null;
      }
      const el = document.getElementById('dashboard-grid');
      if (!el) return;

      // GridStack auto-adopts existing .grid-stack-item children rendered by Vue v-for.
      // FIX: GridStack fires 'change' synchronously during init() before all items are placed,
      // which would cause dashSaveLayout to overwrite correct positions with wrong ones.
      // We use a flag to suppress saves during init and enable them after a short delay.
      this._dashSaveEnabled = false;
      const grid = GridStack.init({
        column: 12,
        cellHeight: 60,
        margin: 8,
        animate: true,
        float: true,
        resizable: { handles: 'se' },
      }, el);

      grid.on('change', () => {
        if (this._dashSaveEnabled) this.dashSaveLayout(grid);
      });
      this.dashGrid = grid;

      // Enable saving after GridStack's init-time change events have all fired
      setTimeout(() => {
        this._dashSaveEnabled = true;
        this.dashInitResizeObservers();
      }, 300);

      if (this.dashWidgets.some(w => w.type === 'server-info')) {
        this.loadSystemInfo();
      }

      // Restore CSS zoom for non-monitoring widgets that have a saved fontScale.
      this.$nextTick(() => {
        this.dashWidgets.forEach(w => {
          if (w.type === 'monitoring' || !w.fontScale || w.fontScale === 1.0) return;
          const gsItem = document.querySelector(`[gs-id="${w.id}"]`);
          if (!gsItem) return;
          const card = gsItem.querySelector('.dash-card');
          if (card) card.style.zoom = w.fontScale;
        });
      });
    },

    // Attach ResizeObserver to each widget content container.
    // Applies CSS zoom to the inner .dash-card so fonts/icons/buttons scale
    // automatically when the widget is resized. GridStack layout is unaffected
    // because it measures the outer .grid-stack-item-content, not .dash-card.
    dashInitResizeObservers() {
      if (this._dashResizeObs) {
        this._dashResizeObs.disconnect();
        this._dashResizeObs = null;
      }
      const grid = document.getElementById('dashboard-grid');
      if (!grid) return;

      // Proportional scaling: 450px = 1.0 (reference), linear, clamped 0.55–1.5.
      // Multiplied by per-widget fontScale (user +/- adjustment, default 1.0).
      const zoomFor = (width, fontScale) =>
        Math.max(0.55, Math.min(1.5, (width / 450) * (fontScale || 1.0)));

      const obs = new ResizeObserver(entries => {
        for (const entry of entries) {
          const gsItem = entry.target.closest('[gs-id]');
          const widgetId = gsItem ? gsItem.getAttribute('gs-id') : null;
          const widget = widgetId ? this.dashWidgets.find(w => w.id === widgetId) : null;
          // CSS zoom on dash-card breaks ApexCharts tooltip coordinate mapping:
          // getBoundingClientRect() on SVG <g> elements does not account for ancestor
          // CSS zoom in all browsers, while event.clientX does → proportional drift.
          // Font scaling is handled via font-size on dash-card-body (fontScale setting).
        }
      });

      grid.querySelectorAll('.grid-stack-item-content').forEach(c => obs.observe(c));
      this._dashResizeObs = obs;
    },

    // Adjust per-widget font scale by delta (+0.1 or -0.1), clamp 0.5–2.0.
    // Triggers ResizeObserver re-fire by dispatching a resize on the content element.
    dashAdjustFontScale(widgetId, delta) {
      const idx = this.dashWidgets.findIndex(w => w.id === widgetId);
      if (idx === -1) return;
      const w = this.dashWidgets[idx];
      const current = w.fontScale || 1.0;
      const next = Math.round(Math.max(0.5, Math.min(2.0, current + delta)) * 10) / 10;
      this.dashWidgets.splice(idx, 1, { ...w, fontScale: next });
      // Apply CSS zoom immediately for non-monitoring widgets.
      // monitoring widgets use interactive ApexCharts tooltips — CSS zoom breaks
      // getBoundingClientRect() coordinate mapping causing tooltip drift.
      // Other widgets (server-info, peers-summary, gateways, etc.) only contain
      // sparkline charts with tooltips disabled, so CSS zoom is safe there.
      if (w.type !== 'monitoring') {
        this.$nextTick(() => {
          const gsItem = document.querySelector(`[gs-id="${widgetId}"]`);
          if (!gsItem) return;
          const card = gsItem.querySelector('.dash-card');
          if (card) card.style.zoom = next;
        });
      }
      this.api.putDashboardWidgets(this.dashWidgets).catch(console.error);
    },

    dashDefaultWidgets() {
      return [
        { id: 'w-server-info',   type: 'server-info',   x: 0, y: 0, w: 4, h: 4 },
        { id: 'w-peers-summary', type: 'peers-summary',  x: 4, y: 0, w: 4, h: 4 },
        { id: 'w-gateways',      type: 'gateways',       x: 8, y: 0, w: 4, h: 4 },
        { id: 'w-interfaces',    type: 'interfaces',     x: 0, y: 4, w: 6, h: 5 },
        { id: 'w-traffic',       type: 'traffic',        x: 6, y: 4, w: 6, h: 4 },
      ];
    },

    dashSaveLayout(grid) {
      const items = grid.save(false);
      const widgets = (items || []).map(item => {
        const existing = this.dashWidgets.find(w => w.id === item.id);
        // Preserve all extra fields (graphs, fontScale, etc.) alongside GridStack geometry
        return Object.assign({}, existing || {}, {
          id: item.id,
          type: existing ? existing.type : item.id.replace('w-', ''),
          x: item.x, y: item.y, w: item.w, h: item.h,
        });
      });
      this.dashWidgets = widgets;
      this.api.putDashboardWidgets(widgets).catch(console.error);
    },

    dashWidgetLabel(type) {
      const t = this.dashAvailableWidgetTypes.find(x => x.type === type);
      return t ? t.label : type;
    },

    dashAddWidget(type) {
      const id = 'w-' + type + '-' + Date.now();
      const newW = { id, type, x: 0, y: 0, w: 4, h: 4 };
      this.dashWidgets.push(newW);
      this.$nextTick(() => {
        if (!this.dashGrid) return;
        // Vue has rendered the new item; tell GridStack to adopt it
        const el = document.querySelector(`[gs-id="${id}"]`);
        if (el) {
          this.dashGrid.makeWidget(el);
          // Observe the new widget for resize scaling
          if (this._dashResizeObs) {
            const content = el.querySelector('.grid-stack-item-content');
            if (content) this._dashResizeObs.observe(content);
          }
        }
        if (type === 'server-info') this.loadSystemInfo();
      });
    },

    dashRemoveWidget(id) {
      if (this.dashGrid) {
        const el = this.dashGrid.el
          ? this.dashGrid.el.querySelector(`[gs-id="${id}"]`)
          : null;
        // removeWidget(el, false) — removes from GridStack but keeps DOM node
        // Vue will then remove it when we splice dashWidgets
        if (el) this.dashGrid.removeWidget(el, false);
      }
      const idx = this.dashWidgets.findIndex(w => w.id === id);
      if (idx !== -1) this.dashWidgets.splice(idx, 1);
      this.api.putDashboardWidgets(this.dashWidgets).catch(console.error);
    },

    dashResetLayout() {
      // Destroy grid first (keeps DOM nodes, Vue will replace them)
      if (this.dashGrid) {
        this.dashGrid.destroy(false);
        this.dashGrid = null;
      }
      const defaults = this.dashDefaultWidgets();
      this.dashWidgets = defaults;
      this.api.putDashboardWidgets(defaults).catch(console.error);
      // Wait for Vue to render the new items, then init GridStack
      this.$nextTick(() => this.dashInitGrid());
    },

    async loadSystemInfo() {
      try {
        this.dashSystemInfo = await this.api.getSystemInfo();
      } catch (e) { /* non-fatal */ }
    },

    // ── Monitoring widget methods ──────────────────────────────────────────────

    metricsStartPoller() {
      if (this.metricsPoller) return;
      this.metricsPoller = setInterval(() => this.metricsTick(), 5000);
      this.metricsTick();
      if (this._metricsHistoryPoller) clearInterval(this._metricsHistoryPoller);
      this._metricsRefreshHistory();
      this._metricsHistoryPoller = setInterval(() => this._metricsRefreshHistory(), 30000);
      // Pause poller when tab is hidden, resume (with immediate tick) when visible again
      if (!this._metricsVisibilityHandler) {
        this._metricsVisibilityHandler = () => {
          if (document.hidden) {
            this.metricsStopPoller();
          } else {
            this.metricsStartPoller();
          }
        };
        document.addEventListener('visibilitychange', this._metricsVisibilityHandler);
      }
    },

    metricsStopPoller() {
      if (this.metricsPoller) {
        clearInterval(this.metricsPoller);
        this.metricsPoller = null;
      }
      if (this._metricsHistoryPoller) {
        clearInterval(this._metricsHistoryPoller);
        this._metricsHistoryPoller = null;
      }
    },

    async _metricsRefreshHistory() {
      if (this.metricsConfigWidget) return;
      const allWidgets = [
        ...this.dashWidgets.filter(w => w.type === 'monitoring'),
        ...this.diagWidgets.filter(w => w.type === 'monitoring'),
      ];
      for (const w of allWidgets) {
        const period = this.metricsWidgetPeriod[w.id] || '5m';
        if (period === '5m') continue;
        for (const key of (w.graphs || [])) {
          if (key.startsWith('gateway:')) {
            await this.metricsLoadGatewayDist(w.id, key, period);
          } else {
            await this.metricsLoadHistory(w.id, key, period);
          }
        }
      }
    },

    async metricsTick() {
      if (this.metricsConfigWidget) return;
      try {
        const snap = await this.api.getMetrics();
        this.metricsSnapshot = snap;

        // Build available keys list; rebuild static keys on first snapshot,
        // but always sync gateway keys so newly added/removed gateways appear.
        if (snap) {
          if (!this.metricsAvailableKeys.length) {
            const keys = ['cpu', 'mem'];
            for (const iface of (snap.interfaces || [])) {
              keys.push(`net:${iface}:rx`);
              keys.push(`net:${iface}:tx`);
            }
            this.metricsAvailableKeys = keys;
          }
          // Gateway keys: add new, remove stale on every tick
          const gwIds = new Set(Object.keys(snap.gateways || {}));
          const gwKeys = [...gwIds].map(id => `gateway:${id}`);
          const withoutGw = this.metricsAvailableKeys.filter(k => !k.startsWith('gateway:'));
          this.metricsAvailableKeys = [...withoutGw, ...gwKeys];
        }

        // Append to rolling buffers for realtime (5m) widgets — both dashboard and diagnostics
        const now = Date.now();
        const MAX_POINTS = 60;
        const allMonitoringWidgets = [
          ...this.dashWidgets.filter(w => w.type === 'monitoring'),
          ...this.diagWidgets.filter(w => w.type === 'monitoring'),
        ];
        for (const w of allMonitoringWidgets) {
          const period = this.metricsWidgetPeriod[w.id] || '5m';
          if (period !== '5m') continue;
          for (const key of (w.graphs || [])) {
            const val = this.metricsValueFromSnap(snap, key);
            if (val === null) continue;
            const bufKey = `${w.id}:${key}`;
            let buf = this.metricsHistory[bufKey];
            if (!buf) { buf = []; this.$set(this.metricsHistory, bufKey, buf); }
            buf.push({ x: now, y: Math.round(val * 100) / 100 });
            if (buf.length > MAX_POINTS) buf.shift();
            if (key.startsWith('gateway:')) this._updateGatewaySeriesCache(w.id, key);
            else this._updateAreaSeriesCache(w.id, key);
          }
        }
      } catch (e) { /* non-fatal */ }
    },

    metricsValueFromSnap(snap, key) {
      if (!snap) return null;
      if (key === 'cpu') return snap.cpu ?? null;
      if (key === 'mem') return snap.mem ?? null;
      if (key.startsWith('net:')) {
        const [, iface, dir] = key.split(':');
        return snap.net?.[iface]?.[dir === 'rx' ? 'rxMbps' : 'txMbps'] ?? null;
      }
      if (key.startsWith('gateway:')) {
        const id = key.slice(8);
        return snap.gateways?.[id] ?? null;
      }
      return null;
    },

    metricsKeyLabel(key) {
      if (key === 'cpu') return 'CPU %';
      if (key === 'mem') return 'RAM %';
      if (key.startsWith('net:')) {
        const [, iface, dir] = key.split(':');
        const ti = (this.tunnelInterfaces || []).find(t => t.id === iface);
        const label = ti && ti.name ? `${ti.name} (${iface})` : iface;
        return `${label} ${dir.toUpperCase()} Mbps`;
      }
      if (key.startsWith('gateway:')) {
        const id = key.slice(8);
        const gw = (this.gateways || []).find(g => g.id === id);
        return gw ? gw.name : id;
      }
      return key;
    },

    // Returns fill color for a gateway status value (3=healthy, 2=degraded, 1=down, 0=admin_down)
    _gatewayStatusColor(val) {
      const v = Math.round(val);
      if (v >= 3) return '#22c55e'; // healthy — green
      if (v >= 2) return '#eab308'; // degraded — yellow
      if (v >= 1) return '#ef4444'; // down — red
      return '#9ca3af';               // admin_down / unknown — gray
    },

    metricsYAxisTitle(key) {
      if (key === 'cpu' || key === 'mem') return '%';
      return 'Mbps';
    },

    async metricsLoadHistory(widgetId, key, period) {
      try {
        const res = await this.api.getMetricsHistory({ key, period });
        const points = (res.points || []).map(p => ({ x: p[0], y: Math.round(p[1] * 100) / 100 }));
        this.$set(this.metricsHistory, `${widgetId}:${key}`, points);
        this._updateAreaSeriesCache(widgetId, key);
      } catch (e) { /* non-fatal */ }
    },

    async metricsLoadGatewayDist(widgetId, key, period) {
      try {
        const res = await this.api.getMetricsGatewayDist({ key, period });
        this.$set(this.metricsGatewayDist, `${widgetId}:${key}`, res.buckets || []);
        this._updateGatewaySeriesCache(widgetId, key);
      } catch (e) { /* non-fatal */ }
    },

    metricsGatewayTooltipFormatter(widgetId) {
      const period = this.metricsWidgetPeriod[widgetId] || '5m';
      if (period === '5m') {
        // Each tick is exactly one status — show name, hide zero series
        return function(v, opts) {
          if (!v) return undefined; // hide zero-value series from tooltip
          return opts.w.config.series[opts.seriesIndex].name;
        };
      }
      return function(v) { return v + '%'; };
    },

    // Returns cached series for gateway bar chart. Stable reference → ApexCharts skips re-render
    // when data hasn't changed. Cache is updated only in _updateGatewaySeriesCache.
    metricsGetGatewayStackedSeries(widgetId, key) {
      return this.metricsGatewaySeriesCache[`${widgetId}:${key}`] || [];
    },

    // Builds 4 stacked series and stores in cache. Call whenever underlying data changes.
    _updateGatewaySeriesCache(widgetId, key) {
      const period = this.metricsWidgetPeriod[widgetId] || '5m';
      const healthy   = { name: 'Healthy',    data: [] };
      const degraded  = { name: 'Degraded',   data: [] };
      const down      = { name: 'Down',       data: [] };
      const adminDown = { name: 'Admin Down', data: [] };

      if (period === '5m') {
        const buf = this.metricsHistory[`${widgetId}:${key}`] || [];
        for (const p of buf) {
          const v = Math.round(p.y);
          healthy.data.push(  { x: p.x, y: v >= 3 ? 1 : 0 });
          degraded.data.push( { x: p.x, y: v === 2 ? 1 : 0 });
          down.data.push(     { x: p.x, y: v === 1 ? 1 : 0 });
          adminDown.data.push({ x: p.x, y: v <= 0 ? 1 : 0 });
        }
      } else {
        const dist = this.metricsGatewayDist[`${widgetId}:${key}`] || [];
        for (const b of dist) {
          const [ts, h, d, dn, ad] = b;
          const total = h + d + dn + ad || 1;
          healthy.data.push(  { x: ts, y: Math.round(h  / total * 100) });
          degraded.data.push( { x: ts, y: Math.round(d  / total * 100) });
          down.data.push(     { x: ts, y: Math.round(dn / total * 100) });
          adminDown.data.push({ x: ts, y: Math.round(ad / total * 100) });
        }
      }
      this.$set(this.metricsGatewaySeriesCache, `${widgetId}:${key}`, [adminDown, down, degraded, healthy]);
    },


    async metricsOnPeriodChange(widgetId, period) {
      this.$set(this.metricsWidgetPeriod, widgetId, period);
      const isDiag = this.activePage === 'diagnostics';
      const list = isDiag ? this.diagWidgets : this.dashWidgets;
      const idx = list.findIndex(w => w.id === widgetId);
      if (idx === -1) return;
      // Persist period into widget layout so it survives page refresh.
      // Debounced: rapid switching won't fire multiple concurrent PUT requests.
      const updated = { ...list[idx], period };
      list.splice(idx, 1, updated);
      const page = isDiag ? 'diagnostics' : 'dashboard';
      if (this._metricsPeriodSaveTimer) clearTimeout(this._metricsPeriodSaveTimer);
      this._metricsPeriodSaveTimer = setTimeout(() => {
        this.api.putDashboardWidgets(isDiag ? this.diagWidgets : this.dashWidgets, page).catch(console.error);
      }, 500);
      const w = updated;
      // Clear existing buffer so chart doesn't mix realtime and history points
      for (const key of (w.graphs || [])) {
        this.$set(this.metricsHistory, `${widgetId}:${key}`, []);
        this.$set(this.metricsGatewayDist, `${widgetId}:${key}`, []);
        this.$set(this.metricsGatewaySeriesCache, `${widgetId}:${key}`, []);
        this.$set(this.metricsAreaSeriesCache, `${widgetId}:${key}`, []);
        if (this._chartOptionsCache) {
          const prefix = `${widgetId}:${key}:`;
          Object.keys(this._chartOptionsCache).forEach(k => { if (k.startsWith(prefix)) delete this._chartOptionsCache[k]; });
        }
      }
      if (period === '5m') return; // realtime — poller fills it from next tick
      for (const key of (w.graphs || [])) {
        if (key.startsWith('gateway:')) {
          await this.metricsLoadGatewayDist(widgetId, key, period);
        } else {
          await this.metricsLoadHistory(widgetId, key, period);
        }
      }
    },

    metricsGetSeries(widgetId, key) {
      const data = this.metricsHistory[`${widgetId}:${key}`] || [];
      if (!key.startsWith('gateway:')) return data;
      return data.map(p => ({ x: p.x, y: 1 }));
    },

    // Builds series wrapper for area charts; call whenever underlying data changes.
    // Always replaces with $set so ApexCharts detects the change and re-renders.
    // Chart options are stable via _chartOptionsCache, so this does not cause re-init.
    _updateAreaSeriesCache(widgetId, key) {
      const cacheKey = `${widgetId}:${key}`;
      const buf = this.metricsHistory[cacheKey] || [];
      this.$set(this.metricsAreaSeriesCache, cacheKey, [
        { name: this.metricsKeyLabel(key), data: buf.slice() },
      ]);
    },

    metricsGetCachedAreaSeries(widgetId, key) {
      return this.metricsAreaSeriesCache[`${widgetId}:${key}`] || [];
    },

    // Returns stable chart options object for area charts.
    // Keyed by all fields that affect the options so changes (theme, period, color) get fresh object.
    metricsAreaChartOptions(widgetId, key) {
      const w = [...this.dashWidgets, ...this.diagWidgets].find(w => w.id === widgetId);
      const period = this.metricsWidgetPeriod[widgetId] || '5m';
      const color = (w && w.graphColors && w.graphColors[key]) || '#22d3ee';
      const ck = `${widgetId}:${key}:${period}:${this.theme}:${color}`;
      if (!this._chartOptionsCache) this._chartOptionsCache = {};
      if (!this._chartOptionsCache[ck]) {
        this._chartOptionsCache[ck] = {
          chart: { id: widgetId+'_'+key, type:'area', animations:{ enabled:false }, toolbar:{ show:false }, sparkline:{ enabled:false }, background:'transparent',
            events: {
              mouseMove: function(event, chartContext) {
                // ApexCharts sets style.top on each mouseMove — override it to pin tooltip to top.
                var tt = chartContext.el && chartContext.el.querySelector('.apexcharts-tooltip');
                if (tt) tt.style.top = '4px';
              },
            },
          },
          colors: [color],
          stroke: { curve:'smooth', width:2 },
          markers: { size: 0 },
          dataLabels: { enabled: false },
          fill: { type:'gradient', gradient:{ shadeIntensity:1, opacityFrom:0.4, opacityTo:0.05 } },
          xaxis: { type:'datetime', labels:{ show: period !== '5m', datetimeUTC:false, style:{ fontSize:'9px', colors: this.theme==='dark'?'#a3a3a3':'#9ca3af' } }, axisBorder:{ show:false }, axisTicks:{ show:false }, tooltip:{ enabled:false } },
          yaxis: { labels:{ show:true, minWidth:42, maxWidth:42, style:{ fontSize:'9px', colors: this.theme==='dark'?'#a3a3a3':'#9ca3af' } }, min:0 },
          grid: { borderColor: this.theme==='dark'?'#404040':'#f0f0f0', padding:{ left:0, right:0, top:0 } },
          tooltip: { x:{ format:'HH:mm:ss' }, theme: this.theme==='dark'?'dark':'light' },
          theme: { mode: this.theme==='dark'?'dark':'light' },
        };
      }
      return this._chartOptionsCache[ck];
    },

    // Returns pre-computed per-bar color array for gateway bar charts.
    // Used with distributed:true so colors[i] maps to bar i directly — no dataPointIndex risk.
    metricsGatewayColors(widgetId, key) {
      const data = this.metricsHistory[`${widgetId}:${key}`] || [];
      return data.map(p => this._gatewayStatusColor(p.y));
    },

    // Tooltip label for gateway status by index into the raw history buffer.
    metricsGatewayTooltip(widgetId, key, dataPointIndex) {
      const data = this.metricsHistory[`${widgetId}:${key}`] || [];
      const p = data[dataPointIndex];
      const v = p ? Math.round(p.y) : -1;
      return ['Admin Down', 'Down', 'Degraded', 'Healthy'][v] || 'Unknown';
    },

    metricsCloseConfig() {
      this.metricsConfigWidget = null;
      this.metricsTick();
    },

    metricsOpenConfig(widgetId) {
      // Snapshot the page at open time so Save writes to the correct list
      // even if the user navigates away while the modal is open.
      const page = this.activePage === 'diagnostics' ? 'diagnostics' : 'dashboard';
      const list = page === 'diagnostics' ? this.diagWidgets : this.dashWidgets;
      const w = list.find(w => w.id === widgetId);
      if (!w) return;
      this.metricsConfigWidget = widgetId;
      this.metricsConfigPage = page;
      this.metricsConfigDraft = [...(w.graphs || [])];
      this.metricsColorDraft = Object.assign({}, w.graphColors || {});
      this.metricsTitleDraft = w.title || '';
    },

    metricsToggleGraph(key) {
      const idx = this.metricsConfigDraft.indexOf(key);
      if (idx === -1) this.metricsConfigDraft.push(key);
      else this.metricsConfigDraft.splice(idx, 1);
    },

    async metricsSaveConfig() {
      const widgetId = this.metricsConfigWidget;
      const page = this.metricsConfigPage || 'dashboard';
      const isDiag = page === 'diagnostics';
      const list = isDiag ? this.diagWidgets : this.dashWidgets;
      const idx = list.findIndex(w => w.id === widgetId);
      if (idx === -1) return;
      const title = this.metricsTitleDraft.trim() || '';
      const w = { ...list[idx], graphs: [...this.metricsConfigDraft], graphColors: { ...this.metricsColorDraft }, title };
      list.splice(idx, 1, w);
      this.metricsCloseConfig();
      this.api.putDashboardWidgets(isDiag ? this.diagWidgets : this.dashWidgets, page).catch(console.error);
      // Load history for newly added graphs if not in realtime mode
      const period = this.metricsWidgetPeriod[widgetId] || '5m';
      if (period !== '5m') {
        for (const key of w.graphs) {
          if (key.startsWith('gateway:')) await this.metricsLoadGatewayDist(widgetId, key, period);
          else await this.metricsLoadHistory(widgetId, key, period);
        }
      }
    },

    // ── Diagnostics page ─────────────────────────────────────────────────────

    async loadDiagnostics() {
      // Sequence counter prevents stale concurrent loads from calling diagInitGrid.
      const seq = (this._diagLoadSeq = (this._diagLoadSeq || 0) + 1);
      try {
        const res = await this.api.getDashboardWidgets('diagnostics');
        const saved = res.widgets || [];
        this.diagWidgets = saved;
      } catch (e) {
        this.diagWidgets = [];
      }
      if (seq !== this._diagLoadSeq) return; // superseded by a newer load
      // Restore persisted periods and pre-load history
      for (const w of this.diagWidgets) {
        if (w.period) this.$set(this.metricsWidgetPeriod, w.id, w.period);
        const period = this.metricsWidgetPeriod[w.id] || '5m';
        if (period !== '5m') {
          for (const key of (w.graphs || [])) {
            if (key.startsWith('gateway:')) this.metricsLoadGatewayDist(w.id, key, period);
            else this.metricsLoadHistory(w.id, key, period);
          }
        }
      }
      await this.$nextTick();
      if (seq !== this._diagLoadSeq) return;
      this.diagInitGrid();
    },

    diagInitGrid() {
      if (this.diagGrid) { this.diagGrid.destroy(false); this.diagGrid = null; }
      const el = document.querySelector('.diag-grid');
      if (!el) return;
      this.diagGrid = GridStack.init({
        cellHeight: 60,
        margin: 8,
        column: 12,
        animate: true,
        float: true,
        resizable: { handles: 'se' },
      }, el);
      // Sync positions back to diagWidgets on change
      this.diagGrid.on('change', () => {
        if (this._diagSaveEnabled) this.diagSaveLayout();
      });
      this._diagSaveEnabled = false;
      setTimeout(() => { this._diagSaveEnabled = true; }, 500);
    },

    diagSaveLayout() {
      if (!this.diagGrid) return;
      const snapshot = this.diagWidgets.slice(); // stable reference against concurrent mutations
      const items = this.diagGrid.save(false);
      const widgets = (items || []).map(item => {
        const existing = snapshot.find(w => w.id === item.id);
        if (!existing) return null; // widget removed mid-save — skip
        return Object.assign({}, existing, {
          x: item.x, y: item.y, w: item.w, h: item.h,
        });
      }).filter(Boolean);
      this.diagWidgets = widgets;
      this.api.putDashboardWidgets(widgets, 'diagnostics').catch(console.error);
    },

    diagAddWidget() {
      const id = 'w-monitoring-' + Date.now();
      const newW = { id, type: 'monitoring', x: 0, y: 0, w: 6, h: 5, graphs: [], title: '' };
      this.diagWidgets.push(newW);
      // Always persist immediately so the widget survives even if grid init failed.
      this.api.putDashboardWidgets(this.diagWidgets, 'diagnostics').catch(console.error);
      this.$nextTick(() => {
        if (!this.diagGrid) return;
        const el = document.querySelector(`[gs-id="${id}"]`);
        if (el) {
          this.diagGrid.makeWidget(el);
          this.diagSaveLayout();
        }
      });
    },

    diagRemoveWidget(widgetId) {
      const idx = this.diagWidgets.findIndex(w => w.id === widgetId);
      if (idx === -1) return;
      if (this.diagGrid) {
        const el = document.querySelector(`[gs-id="${widgetId}"]`);
        if (el) this.diagGrid.removeWidget(el, false);
      }
      this.diagWidgets.splice(idx, 1);
      this.api.putDashboardWidgets(this.diagWidgets, 'diagnostics').catch(console.error);
    },

    // ── Ping utility ─────────────────────────────────────────────────────────

    async pingLoadInterfaces() {
      if (this.pingRouterIfaces.length) return;
      try {
        const res = await this.api.getSystemInterfaces();
        this.pingRouterIfaces = res.interfaces || [];
      } catch (e) { /* silent */ }
    },

    pingStart() {
      if (!this.pingHost.trim()) return;
      if (this.pingEventSource) { this.pingEventSource.close(); this.pingEventSource = null; }
      this.terminalLines = [];
      this.pingRunning = true;

      const params = new URLSearchParams({ host: this.pingHost.trim(), count: this.pingCount });
      if (this.pingSource) {
        // Resolve interface name to its first IPv4 address (Cisco-style source IP routing).
        const iface = this.pingRouterIfaces.find(i => i.name === this.pingSource);
        const sourceIp = iface && iface.addrs && iface.addrs.length ? iface.addrs[0] : this.pingSource;
        params.set('source', sourceIp);
      }
      if (this.pingSize !== '') params.set('size', this.pingSize);
      if (this.pingDf) params.set('df', 'true');
      if (this.pingTos !== '') params.set('tos', this.pingTos);

      const segs = window.location.pathname.split('/').filter(Boolean);
      const apiBase = segs.length > 0
        ? `${window.location.origin}/${segs[0]}/api`
        : `${window.location.origin}/api`;
      const url = `${apiBase}/diagnostics/ping/stream?` + params.toString();
      const es = new EventSource(url);
      this.pingEventSource = es;

      es.onmessage = (e) => {
        if (e.data === '[done]') {
          this.pingRunning = false;
          es.close();
          this.pingEventSource = null;
          return;
        }
        this.terminalLines.push(e.data);
        this.$nextTick(() => {
          const el = this.$el && this.$el.querySelector('.ping-terminal');
          if (el) el.scrollTop = el.scrollHeight;
        });
      };
      es.onerror = () => {
        this.pingRunning = false;
        es.close();
        this.pingEventSource = null;
      };
    },

    pingStop() {
      if (this.pingEventSource) { this.pingEventSource.close(); this.pingEventSource = null; }
      this.pingRunning = false;
      if (this.terminalLines.length) this.terminalLines.push('--- interrupted ---');
    },

    pingClear() {
      this.terminalLines = [];
    },

    traceStart() {
      if (!this.traceHost.trim()) return;
      if (this.traceEventSource) { this.traceEventSource.close(); this.traceEventSource = null; }
      this.terminalLines = [];
      this.traceRunning = true;

      const params = new URLSearchParams({ host: this.traceHost.trim(), type: this.traceType });
      if (this.traceSource) {
        const iface = this.pingRouterIfaces.find(i => i.name === this.traceSource);
        const sourceIp = iface && iface.addrs && iface.addrs.length ? iface.addrs[0] : this.traceSource;
        params.set('source', sourceIp);
      }

      const segs = window.location.pathname.split('/').filter(Boolean);
      const apiBase = segs.length > 0
        ? `${window.location.origin}/${segs[0]}/api`
        : `${window.location.origin}/api`;
      const url = `${apiBase}/diagnostics/traceroute/stream?` + params.toString();
      const es = new EventSource(url);
      this.traceEventSource = es;

      es.onmessage = (e) => {
        if (e.data === '[done]') {
          this.traceRunning = false;
          es.close();
          this.traceEventSource = null;
          return;
        }
        this.terminalLines.push(e.data);
        this.$nextTick(() => {
          const el = this.$el && this.$el.querySelector('.ping-terminal');
          if (el) el.scrollTop = el.scrollHeight;
        });
      };
      es.onerror = () => {
        this.traceRunning = false;
        es.close();
        this.traceEventSource = null;
      };
    },

    traceStop() {
      if (this.traceEventSource) { this.traceEventSource.close(); this.traceEventSource = null; }
      this.traceRunning = false;
      if (this.terminalLines.length) this.terminalLines.push('--- interrupted ---');
    },

    tcpdumpStart() {
      if (!this.tcpdumpIface) return;
      if (this.tcpdumpEventSource) { this.tcpdumpEventSource.close(); this.tcpdumpEventSource = null; }
      this.terminalLines = [];
      this.tcpdumpPcapId = null;
      this.tcpdumpDownloadReady = false;
      this.tcpdumpRunning = true;

      const params = new URLSearchParams({ iface: this.tcpdumpIface });
      if (this.tcpdumpFilter.trim()) params.set('filter', this.tcpdumpFilter.trim());
      if (this.tcpdumpSave) params.set('save', 'true');

      const segs = window.location.pathname.split('/').filter(Boolean);
      const apiBase = segs.length > 0
        ? `${window.location.origin}/${segs[0]}/api`
        : `${window.location.origin}/api`;
      this._tcpdumpApiBase = apiBase;
      const url = `${apiBase}/diagnostics/tcpdump/stream?` + params.toString();
      const es = new EventSource(url);
      this.tcpdumpEventSource = es;

      es.onmessage = (e) => {
        if (e.data === '[done]') {
          this.tcpdumpRunning = false;
          es.close();
          this.tcpdumpEventSource = null;
          // If a capture ID was received, file is now ready to download.
          if (this.tcpdumpPcapId) this.tcpdumpDownloadReady = true;
          return;
        }
        // Backend sends capture ID as first event when save=true.
        if (e.data.startsWith('[captureid:') && e.data.endsWith(']')) {
          this.tcpdumpPcapId = e.data.slice(11, -1);
          return; // don't print to terminal
        }
        this.terminalLines.push(e.data);
        this.$nextTick(() => {
          const el = this.$el && this.$el.querySelector('.ping-terminal');
          if (el) el.scrollTop = el.scrollHeight;
        });
      };
      es.onerror = () => {
        this.tcpdumpRunning = false;
        es.close();
        this.tcpdumpEventSource = null;
      };
    },

    tcpdumpStop() {
      if (this.tcpdumpEventSource) { this.tcpdumpEventSource.close(); this.tcpdumpEventSource = null; }
      this.tcpdumpRunning = false;
      if (this.terminalLines.length) this.terminalLines.push('--- stopping, finalizing file…');

      if (this.tcpdumpSave && this.tcpdumpPcapId) {
        // Ask backend to SIGINT tcpdump so it flushes and closes the pcap file.
        const apiBase = this._tcpdumpApiBase || (window.location.origin + '/api');
        fetch(`${apiBase}/diagnostics/tcpdump/stop?file=` + encodeURIComponent(this.tcpdumpPcapId), { method: 'POST' })
          .then(() => { this.tcpdumpDownloadReady = true; })
          .catch(() => { this.tcpdumpDownloadReady = true; }); // show button anyway
      } else {
        if (this.terminalLines.length) this.terminalLines[this.terminalLines.length - 1] = '--- interrupted ---';
      }
    },

    tcpdumpDownload() {
      if (!this.tcpdumpPcapId) return;
      const apiBase = this._tcpdumpApiBase || (window.location.origin + '/api');
      const url = `${apiBase}/diagnostics/tcpdump/download?file=` + encodeURIComponent(this.tcpdumpPcapId);
      const a = document.createElement('a');
      a.href = url;
      a.download = 'capture-' + this.tcpdumpPcapId.slice(0, 8) + '.pcap';
      document.body.appendChild(a);
      a.click();
      document.body.removeChild(a);
      this.tcpdumpPcapId = null;
      this.tcpdumpDownloadReady = false;
    },

    // ── End ping utility ─────────────────────────────────────────────────────

    // Compact elapsed time: "5s", "20m", "3h", "2d"
    dashTimeShort(ts) {
      if (!ts) return '';
      const sec = Math.floor((Date.now() - new Date(ts).getTime()) / 1000);
      if (sec < 0) return '';
      if (sec < 60) return sec + 's';
      if (sec < 3600) return Math.floor(sec / 60) + 'm';
      if (sec < 86400) return Math.floor(sec / 3600) + 'h';
      return Math.floor(sec / 86400) + 'd';
    },

    dashFmtBytes(bytes) {
      if (!bytes || bytes === 0) return '0 B';
      const units = ['B', 'KB', 'MB', 'GB', 'TB'];
      const i = Math.floor(Math.log(bytes) / Math.log(1024));
      return (bytes / Math.pow(1024, i)).toFixed(i > 0 ? 1 : 0) + ' ' + units[Math.min(i, units.length - 1)];
    },

    // Returns (or lazily creates) reactive per-widget peers filter state
    dashPeersGetState(widgetId) {
      if (!this.dashPeersState[widgetId]) {
        this.$set(this.dashPeersState, widgetId, { iface: '', sort: 'name' });
      }
      return this.dashPeersState[widgetId];
    },

    // Returns filtered + sorted peers for a given widget
    dashFilteredPeers(widgetId) {
      const s = this.dashPeersGetState(widgetId);
      let peers = this.allPeers;
      if (s.iface === '__clients__') {
        peers = peers.filter(p => p.peerType !== 'interconnect');
      } else if (s.iface === '__s2s__') {
        peers = peers.filter(p => p.peerType === 'interconnect');
      } else if (s.iface) {
        peers = peers.filter(p => p.interfaceId === s.iface);
      }
      if (s.sort === 'traffic') {
        peers = [...peers].sort((a, b) =>
          ((b.totalTx || 0) + (b.totalRx || 0)) - ((a.totalTx || 0) + (a.totalRx || 0)));
      } else if (s.sort === 'seen') {
        peers = [...peers].sort((a, b) => {
          const ta = a.latestHandshakeAt ? new Date(a.latestHandshakeAt).getTime() : 0;
          const tb = b.latestHandshakeAt ? new Date(b.latestHandshakeAt).getTime() : 0;
          return tb - ta;
        });
      } else {
        peers = [...peers].sort((a, b) => (a.name || '').localeCompare(b.name || ''));
      }
      return peers;
    },

    // Toggle interface up/down from dashboard widget
    async dashToggleInterface(iface) {
      if (iface.enabled) {
        await this.stopTunnelInterface(iface);
      } else {
        await this.startTunnelInterface(iface);
      }
    },

    // Open Quick Create peer modal for a specific interface from dashboard
    dashOpenAddPeer(iface) {
      if (iface.disableRoutes) return; // S2S interfaces don't support quick-create clients
      this.activeInterfaceId = iface.id;
      this.peerCreateName = '';
      this.peerCreateExpiredDate = '';
      this.peerCreateGroupId = this.defaultGroupId();
      this.showQuickPeerCreate = true;
    },

    // Human-readable protocol label for dashboard badges
    dashProtoLabel(protocol) {
      if (protocol === 'wireguard') return 'WG1.0';
      if (protocol === 'amneziawg-2.0') return 'AWG2.0';
      if (protocol === 'amneziawg') return 'AWG';
      return protocol || 'WG';
    },

    // ── End Dashboard ──────────────────────────────────────────────────────────

    async loadClientGroups() {
      try {
        const res = await this.api.getClientGroups();
        this.clientGroups = res.groups || [];
      } catch (err) {
        console.error('loadClientGroups error:', err);
        this.clientGroups = [];
      }
    },

    defaultGroupId() {
      const g = this.clientGroups.find(g => g.name === 'default');
      return g ? g.id : '';
    },

    // Creates a new client-group inline from the peer create modals.
    // targetModel: 'manual' | 'quick'
    async createInlineClientGroup(targetModal) {
      const name = this.inlineGroupInput.trim();
      if (!name) return;

      // Basic validation: letters, digits, hyphens, underscores; must start with letter
      if (!/^[a-zA-Z][a-zA-Z0-9_-]{0,62}$/.test(name)) {
        this.showToast('Invalid name: start with a letter, only letters/digits/-/_', 'error');
        return;
      }

      // Check if already exists
      const existing = this.clientGroups.find(g => g.name.toLowerCase() === name.toLowerCase());
      if (existing) {
        this.showToast(`Group "${name}" already exists`, 'error');
        if (targetModal === 'manual') {
          this.peerCreate.groupId = existing.id;
          this.inlineGroupShow = false;
        } else {
          this.peerCreateGroupId = existing.id;
          this.inlineGroupShowQuick = false;
        }
        this.inlineGroupInput = '';
        return;
      }

      try {
        const res = await this.api.createClientGroup({ name });
        await this.loadClientGroups();
        const created = this.clientGroups.find(g => g.id === res.id || g.name === name);
        if (created) {
          if (targetModal === 'manual') {
            this.peerCreate.groupId = created.id;
            this.inlineGroupShow = false;
          } else {
            this.peerCreateGroupId = created.id;
            this.inlineGroupShowQuick = false;
          }
        }
        this.inlineGroupInput = '';
        this.showToast(`Group "${name}" created`);
      } catch (err) {
        this.showToast(`Failed to create group: ${err.message}`, 'error');
      }
    },

    _resetAliasCreate() {
      this.aliasCreate = {
        name: '', description: '', type: 'network', entries: '', ipsetEntries: '', memberIds: [],
        genSource: 'country', genCountry: '', genAsn: '', genAsnList: '',
        file: null, rateDown: 0, rateUp: 0,
      };
    },

    // Вернуть только host/network алиасы (кандидаты для group membership)
    _aliasGroupCandidates() {
      return this.aliases.filter(a => a.type === 'host' || a.type === 'network');
    },

    // Вернуть только port алиасы (кандидаты для port-group membership)
    _portAliasCandidates() {
      return this.aliases.filter(a => a.type === 'port');
    },

    // Вернуть port и port-group алиасы (для firewall rule port selector)
    _portAliasOptions() {
      return this.aliases.filter(a => a.type === 'port' || a.type === 'port-group');
    },

    async createAlias() {
      try {
        const data = { name: this.aliasCreate.name, description: this.aliasCreate.description, type: this.aliasCreate.type };
        if (data.type === 'host' || data.type === 'network') {
          data.entries = this.aliasCreate.entries.split('\n').map(l => l.trim()).filter(Boolean);
        }
        if (data.type === 'port') {
          data.entries = this.aliasCreate.entries.split('\n').map(l => l.trim()).filter(Boolean);
        }
        if (data.type === 'group' || data.type === 'port-group') {
          data.memberIds = this.aliasCreate.memberIds;
        }
        if (data.type === 'client-group') {
          data.rateDown = this.aliasCreate.rateDown || 0;
          data.rateUp = this.aliasCreate.rateUp || 0;
        }
        // Сохраняем ipsetEntries, file и genOpts ДО сброса формы
        const ipsetText = this.aliasCreate.ipsetEntries.trim();
        const uploadFile = this.aliasCreate.file;
        const genOpts = this.aliasCreate.type === 'ipset' ? {
          source:  this.aliasCreate.genSource,
          country: this.aliasCreate.genCountry,
          asn:     this.aliasCreate.genAsn,
          asnList: this.aliasCreate.genAsnList,
        } : null;

        const res = await this.api.createAlias(data);
        const created = res.alias || res; // сервер возвращает { alias: {...} }
        this.showAliasCreate = false;
        this._resetAliasCreate();
        await this.loadAliases();

        // ipset: загружаем содержимое (приоритет: ручной ввод > файл)
        if (created.type === 'ipset' && ipsetText) {
          try {
            const result = await this.api.uploadAliasFile({ id: created.id, text: ipsetText });
            await this.loadAliases();
            const count = result && result.entryCount ? result.entryCount : '?';
            this.showToast(`Alias created — ${count} entries loaded`, 'success');
          } catch (err) {
            this.showToast(`Alias created, but entries upload failed: ${err.message}`, 'error');
          }
        } else if (created.type === 'ipset' && uploadFile) {
          try {
            const text = await uploadFile.text();
            const result = await this.api.uploadAliasFile({ id: created.id, text });
            await this.loadAliases();
            const count = result && result.entryCount ? result.entryCount : '?';
            this.showToast(`Alias created — ${count} entries uploaded from ${uploadFile.name}`, 'success');
          } catch (err) {
            this.showToast(`Alias created, but file upload failed: ${err.message}`, 'error');
          }
        } else {
          if (created.type === 'client-group') await this.loadClientGroups();
          this.showToast('Alias created', 'success');
        }

        // Auto-generate если тип ipset и указан источник
        if (created.type === 'ipset' && genOpts &&
            (genOpts.country || genOpts.asn || genOpts.asnList)) {
          await this._startAliasGenerate(created.id, genOpts);
        }
      } catch (err) {
        this.showToast(err.message || 'Failed to create alias', 'error');
      }
    },

    // ── Country picker helpers ────────────────────────────────────────────────
    openCountryPicker(form) {
      this.countryPickerForm = form;
      const code = form === 'create' ? this.aliasCreate.genCountry : this.aliasEdit.genCountry;
      const found = code ? this.countries.find(c => c.code === code.toUpperCase()) : null;
      this.countrySearch = found ? found.name + ' (' + found.code + ')' : '';
      this.showCountryDrop = true;
    },
    selectCountry(c) {
      if (this.countryPickerForm === 'create') {
        this.aliasCreate.genCountry = c.code;
      } else {
        this.aliasEdit.genCountry = c.code;
      }
      this.countrySearch = c.name + ' (' + c.code + ')';
      this.showCountryDrop = false;
    },
    onCountryBlur() {
      // Delay so mousedown on item fires first.
      setTimeout(() => { this.showCountryDrop = false; }, 150);
    },

    async openAliasEdit(alias) {
      const hasEntries = alias.type === 'host' || alias.type === 'network' || alias.type === 'port';
      const hasMembers = alias.type === 'group' || alias.type === 'port-group';
      this.aliasEdit = {
        id: alias.id,
        name: alias.name,
        description: alias.description || '',
        type: alias.type,
        entryCount: alias.entryCount || 0,
        generatorOpts: alias.generatorOpts || null,
        entries: hasEntries ? (alias.entries || []).join('\n') : '',
        ipsetEntries: '',
        ipsetEntriesLoading: false,
        ipsetEntriesLoaded: false,
        memberIds: hasMembers ? [...(alias.memberIds || [])] : [],
        genSource: alias.generatorOpts?.asnList ? 'asn-list' : alias.generatorOpts?.asn ? 'asn' : 'country',
        genCountry: alias.generatorOpts?.country || '',
        genAsn: alias.generatorOpts?.asn || '',
        genAsnList: alias.generatorOpts?.asnList || '',
        rateDown: alias.rateDown || 0,
        rateUp: alias.rateUp || 0,
      };
      // Pre-fill country picker display if country was saved.
      const code = this.aliasEdit.genCountry;
      const found = code ? this.countries.find(c => c.code === code.toUpperCase()) : null;
      this.countrySearch = found ? found.name + ' (' + found.code + ')' : '';
      this.showAliasEdit = true;

      // For small manually-entered ipsets: pre-populate textarea from kernel.
      // Criteria: no generatorOpts (not generated) AND entryCount <= 200.
      if (alias.type === 'ipset' && !alias.generatorOpts && alias.entryCount <= 200) {
        this.aliasEdit.ipsetEntriesLoading = true;
        try {
          const res = await this.api.getAliasEntries({ id: alias.id });
          this.aliasEdit.ipsetEntries = (res.entries || []).join('\n');
          this.aliasEdit.ipsetEntriesLoaded = true;
        } catch (e) {
          this.aliasEdit.ipsetEntries = '';
        } finally {
          this.aliasEdit.ipsetEntriesLoading = false;
        }
      }
    },

    async saveAliasEdit() {
      try {
        const data = { id: this.aliasEdit.id, name: this.aliasEdit.name, description: this.aliasEdit.description };
        if (this.aliasEdit.type === 'host' || this.aliasEdit.type === 'network') {
          data.entries = this.aliasEdit.entries.split('\n').map(l => l.trim()).filter(Boolean);
        }
        if (this.aliasEdit.type === 'port') {
          data.entries = this.aliasEdit.entries.split('\n').map(l => l.trim()).filter(Boolean);
        }
        if (this.aliasEdit.type === 'group' || this.aliasEdit.type === 'port-group') {
          data.memberIds = this.aliasEdit.memberIds;
        }
        if (this.aliasEdit.type === 'client-group') {
          data.rateDown = this.aliasEdit.rateDown || 0;
          data.rateUp = this.aliasEdit.rateUp || 0;
        }
        await this.api.updateAlias(data);

        // For ipset: if user edited entries in textarea — upload new content
        const ipsetText = this.aliasEdit.ipsetEntries.trim();
        if (this.aliasEdit.type === 'ipset' && ipsetText) {
          try {
            const result = await this.api.uploadAliasFile({ id: this.aliasEdit.id, text: ipsetText });
            const count = result && result.entryCount ? result.entryCount : '?';
            this.showToast(`Alias updated — ${count} entries saved`, 'success');
          } catch (err) {
            this.showToast(`Alias saved, but entries upload failed: ${err.message}`, 'error');
          }
        } else {
          this.showToast('Alias updated', 'success');
        }

        this.showAliasEdit = false;
        await this.loadAliases();
        if (this.aliasEdit.type === 'client-group') await this.loadClientGroups();
      } catch (err) {
        this.showToast(err.message || 'Failed to update alias', 'error');
      }
    },

    async deleteAlias(alias) {
      const msg = alias.type === 'client-group'
        ? `Delete group "${alias.name}"? All peers will be moved to the default group.`
        : `Delete alias "${alias.name}"?`;
      if (!confirm(msg)) return;
      try {
        const res = await this.api.deleteAlias({ id: alias.id });
        await this.loadAliases();
        await this.loadClientGroups();
        if (alias.type === 'client-group') {
          const moved = res && res.movedCount ? res.movedCount : 0;
          const toast = moved > 0
            ? `Group "${alias.name}" deleted. ${moved} peer${moved > 1 ? 's' : ''} moved to default.`
            : `Group "${alias.name}" deleted.`;
          this.showToast(toast, 'success', 8000);
        } else {
          this.showToast('Alias deleted', 'success');
        }
      } catch (err) {
        this.showToast(err.message || 'Failed to delete alias', 'error');
      }
    },

    async uploadAliasFile(aliasId, file) {
      try {
        const text = await file.text();
        const result = await this.api.uploadAliasFile({ id: aliasId, text });
        await this.loadAliases();
        const count = result && result.entryCount ? result.entryCount : '?';
        this.showToast(`Uploaded ${count} entries from ${file.name}`, 'success');
      } catch (err) {
        this.showToast(err.message || 'Failed to upload file', 'error');
      }
    },

    async startAliasGenerate(alias) {
      await this._startAliasGenerate(alias.id, {
        source: this.aliasEdit.genSource,
        country: this.aliasEdit.genCountry,
        asn: this.aliasEdit.genAsn,
        asnList: this.aliasEdit.genAsnList,
      });
    },

    async startAliasGenerateCreate(aliasId) {
      await this._startAliasGenerate(aliasId, {
        source: this.aliasCreate.genSource,
        country: this.aliasCreate.genCountry,
        asn: this.aliasCreate.genAsn,
        asnList: this.aliasCreate.genAsnList,
      });
    },

    async _startAliasGenerate(aliasId, opts) {
      try {
        const genOpts = {};
        const src = opts?.source || 'country';
        if (src === 'country' && opts?.country) genOpts.country = opts.country;
        else if (src === 'asn' && opts?.asn) genOpts.asn = opts.asn;
        else if (src === 'asn-list' && opts?.asnList) genOpts.asnList = opts.asnList;
        else {
          this.showToast('Please specify a generation source (country, ASN, or ASN list)', 'error');
          return;
        }
        const { jobId } = await this.api.generateAlias({ id: aliasId, ...genOpts });
        this.aliasGeneratingId = aliasId;
        this.aliasGenerateJobId = jobId;
        this.aliasGenerateJobStatus = { status: 'running' };
        this.showToast('Generation started...', 'success');
        this._pollAliasJob(aliasId, jobId);
      } catch (err) {
        this.showToast(err.message || 'Failed to start generation', 'error');
      }
    },

    _pollAliasJob(aliasId, jobId) {
      const interval = setInterval(async () => {
        try {
          const status = await this.api.getAliasJobStatus({ id: aliasId, jobId });
          this.aliasGenerateJobStatus = status;
          if (status.status === 'done') {
            clearInterval(interval);
            this.aliasGeneratingId = null;
            this.aliasGenerateJobId = null;
            await this.loadAliases();
            this.showToast(`Generation done: ${status.entryCount} prefixes`, 'success');
          } else if (status.status === 'error') {
            clearInterval(interval);
            this.aliasGeneratingId = null;
            this.showToast(`Generation failed: ${status.error}`, 'error');
          }
        } catch (err) {
          clearInterval(interval);
          this.aliasGeneratingId = null;
          console.error('_pollAliasJob error:', err);
        }
      }, 3000);
    },

    _aliasLabel(aliasId) {
      if (!aliasId || aliasId === 'any') return 'Any';
      const a = this.aliases.find(x => x.id === aliasId);
      return a ? a.name : aliasId;
    },

    async openAliasFromBadge(aliasId) {
      if (!aliasId) return;
      this.activePage = 'firewall-aliases';
      if (!this.aliases.length) await this.loadAliases();
      const alias = this.aliases.find(a => a.id === aliasId);
      if (alias) await this.openAliasEdit(alias);
    },

    async showAliasTooltip(event, aliasId) {
      const alias = this.aliases.find(a => a.id === aliasId);
      if (!alias) return;
      const rect = event.target.getBoundingClientRect();
      this.aliasTooltip = { id: aliasId, alias, x: rect.left, y: rect.bottom + 4, ipsetEntries: null, ipsetLoading: false };

      if (alias.type === 'ipset' || alias.type === 'client-group') {
        this.aliasTooltip.ipsetLoading = true;
        try {
          const res = await this.api.getAliasEntries({ id: aliasId });
          // Only update if still hovering the same alias
          if (this.aliasTooltip && this.aliasTooltip.id === aliasId) {
            this.aliasTooltip = Object.assign({}, this.aliasTooltip, {
              ipsetEntries: res.entries || [],
              ipsetLoading: false,
            });
          }
        } catch (e) {
          if (this.aliasTooltip && this.aliasTooltip.id === aliasId) {
            this.aliasTooltip = Object.assign({}, this.aliasTooltip, { ipsetLoading: false });
          }
        }
      }
    },
    hideAliasTooltip() {
      this.aliasTooltip = null;
    },

    // ========================================================================
    // System Backup / Restore

    openBackupModal() {
      this.backupPassword = '';
      this.backupPasswordConfirm = '';
      this.showBackupModal = true;
    },

    async confirmDownloadBackup() {
      if (this.backupPassword && this.backupPassword !== this.backupPasswordConfirm) {
        this.showToast('Passwords do not match', 'error');
        return;
      }
      try {
        this.backupDownloading = true;
        const { blob, filename } = await this.api.downloadSystemBackup({ password: this.backupPassword, includeMetrics: this.backupIncludeMetrics });
        const url = URL.createObjectURL(blob);
        const a = document.createElement('a');
        a.href = url;
        a.download = filename;
        document.body.appendChild(a);
        a.click();
        document.body.removeChild(a);
        URL.revokeObjectURL(url);
        this.showBackupModal = false;
        this.showToast('Backup created' + (this.backupPassword ? ' (encrypted)' : ''), 'success');
      } catch (err) {
        this.showToast(err.message || 'Backup failed', 'error');
      } finally {
        this.backupDownloading = false;
      }
    },

    pickRestoreFile(file) {
      if (!file) return;
      const isEnc = file.name.endsWith('.enc');
      if (isEnc) {
        this.restoreFile = file;
        this.restorePassword = '';
        this.showRestorePasswordModal = true;
      } else {
        this._startRestorePreview(file, '');
      }
    },

    async confirmRestoreWithPassword() {
      this.showRestorePasswordModal = false;
      await this._startRestorePreview(this.restoreFile, this.restorePassword);
      this.restorePassword = '';
    },

    // Step 1: preview — upload file, get interface mapping info.
    async _startRestorePreview(file, password) {
      this.restorePreviewLoading = true;
      try {
        const preview = await this.api.previewSystemRestore({ file, password });
        this.restoreFile = file;
        this.restorePreview = preview;
        // Init ifaceMap: for each backup iface not found on server, default to first server iface.
        const map = {};
        for (const bi of (preview.backupIfaces || [])) {
          const found = (preview.serverIfaces || []).includes(bi);
          map[bi] = found ? bi : ((preview.serverIfaces || [])[0] || bi);
        }
        this.restoreIfaceMap = map;
        this.showRestorePreviewModal = true;
      } catch (err) {
        this.showToast(err.message || 'Preview failed', 'error');
      } finally {
        this.restorePreviewLoading = false;
      }
    },

    // Step 2: confirm — apply backup with optional remap.
    async confirmRestoreApply() {
      this.showRestorePreviewModal = false;
      const file = this.restoreFile;
      const password = this.restorePassword || '';
      // Only send ifaceMap if remapping is actually needed.
      const ifaceMap = (this.restorePreview && this.restorePreview.needsRemap) ? this.restoreIfaceMap : null;
      await this._doRestore(file, password, ifaceMap);
      this.restoreFile = null;
      this.restorePreview = null;
      this.restoreIfaceMap = {};
    },

    async _doRestore(file, password, ifaceMap) {
      try {
        this.systemRestoring = true;
        await this.api.restoreSystemBackup({ file, password, ifaceMap });
        this.showToast('Backup restored. Server is restarting…', 'success');
        // Poll until server is back online (up to 60s).
        const start = Date.now();
        const tryReload = () => {
          if (Date.now() - start > 60000) { window.location.reload(); return; }
          fetch(window.location.pathname).then(r => { if (r.ok) window.location.reload(); else setTimeout(tryReload, 2000); }).catch(() => setTimeout(tryReload, 2000));
        };
        setTimeout(tryReload, 3000);
      } catch (err) {
        this.showToast(err.message || 'Restore failed', 'error');
      } finally {
        this.systemRestoring = false;
      }
    },

    async loadPreRestoreBackups() {
      this.preRestoreBackupsLoading = true;
      try {
        const res = await this.api.listSystemBackups();
        this.preRestoreBackups = res.backups || [];
      } catch (err) {
        this.preRestoreBackups = [];
      } finally {
        this.preRestoreBackupsLoading = false;
      }
    },

    // ========================================================================
    // Import Client Configs — restore private keys for peers imported without
    // a server-side key (e.g. peers migrated from another server's backup).
    // ========================================================================

    openImportClientConfigs() {
      this.$refs.importClientConfigsInput.click();
    },

    async onImportClientConfigsSelected(event) {
      const files = Array.from(event.target.files || []);
      event.target.value = '';
      if (files.length === 0) return;
      const interfaceId = this.currentInterface && this.currentInterface.id;
      if (!interfaceId) return;

      this.importClientConfigsLoading = true;
      try {
        const res = await this.api.importClientConfigs({ interfaceId, files });
        this.importClientConfigsResult = res;
        this.showImportClientConfigsResult = true;
        await this._refreshPeersOrAll();
        this.showToast(`${res.matched} config${res.matched === 1 ? '' : 's'} matched`, res.matched > 0 ? 'success' : 'error');
      } catch (err) {
        this.showToast(err.message || 'Import failed', 'error');
      } finally {
        this.importClientConfigsLoading = false;
      }
    },

    // ========================================================================
    // Firewall Rules Methods  (поглощает PBR / Policy)
    // ========================================================================

    async loadFirewallRules() {
      this.firewallRulesLoading = true;
      try {
        const [rulesRes, pendingRes] = await Promise.all([
          this.api.getFirewallRules(),
          this.api.getFirewallPending(),
        ]);
        this.firewallRules = Array.isArray(rulesRes) ? rulesRes : (rulesRes.rules || []);
        this.firewallPending = pendingRes.hasPendingChanges || false;
      } catch (err) {
        console.error('loadFirewallRules error:', err);
        this.firewallRules = [];
      } finally {
        this.firewallRulesLoading = false;
      }
      // $nextTick must be AFTER finally so firewallRulesLoading=false is already
      // flushed to DOM before we look for tbody#fw-rules-tbody.
      this.$nextTick(() => this._initSortable());
    },

    // Initialise (or re-initialise) Sortable.js on the firewall rules tbody.
    // Called after every loadFirewallRules() via $nextTick so the DOM is up-to-date.
    _initSortable() {
      if (typeof Sortable === 'undefined') return;
      const tbody = document.getElementById('fw-rules-tbody');
      if (!tbody) return;
      if (this._sortableInstance) {
        this._sortableInstance.destroy();
        this._sortableInstance = null;
      }
      this._sortableInstance = Sortable.create(tbody, {
        handle: '.drag-handle',
        draggable: 'tr[data-id]',
        animation: 150,
        ghostClass: 'fw-drag-ghost',
        forceFallback: true,
        fallbackTolerance: 0,
        onEnd: (evt) => {
          if (evt.oldIndex === evt.newIndex) return;
          // Debounce: cancel any in-flight reorder and wait 120ms for rapid consecutive drags.
          clearTimeout(this._reorderTimer);
          this._reorderTimer = setTimeout(() => {
            const ids = Array.from(tbody.querySelectorAll('tr[data-id]')).map(tr => tr.dataset.id);
            this._reorderFirewallRules(ids);
          }, 120);
        },
      });
    },

    async _reorderFirewallRules(ids) {
      try {
        await this.api.reorderFirewallRules(ids);
        await this.loadFirewallRules();
      } catch (err) {
        this.showToast(err.message || 'Failed to reorder rules', 'error');
        await this.loadFirewallRules(); // revert DOM to server state
      }
    },

    async applyFirewallRules() {
      this.firewallApplying = true;
      try {
        await this.api.applyFirewallRules();
        this.firewallPending = false;
        this.showToast('Firewall rules applied', 'success');
      } catch (err) {
        this.showToast(err.message || 'Failed to apply firewall rules', 'error');
      } finally {
        this.firewallApplying = false;
      }
    },

    async discardFirewallChanges() {
      if (!confirm('Discard all unapplied changes and revert to last applied state?')) return;
      try {
        await this.api.discardFirewallChanges();
        await this.loadFirewallRules();
        this.showToast('Changes discarded', 'success');
      } catch (err) {
        this.showToast(err.message || 'Failed to discard changes', 'error');
      }
    },

    async loadFirewallInterfaces() {
      try {
        const res = await this.api.getFirewallInterfaces();
        this.firewallInterfaces = Array.isArray(res) ? res : (res.interfaces || []);
      } catch (err) {
        this.firewallInterfaces = [];
      }
    },

    _resetFirewallCreate() {
      this.firewallCreate = {
        name: '',
        interface: 'any',
        protocol: 'any',
        source:      { type: 'any', aliasId: '', value: '', invert: false, portMode: '', port: '', portAliasId: '' },
        destination: { type: 'any', aliasId: '', value: '', invert: false, portMode: '', port: '', portAliasId: '' },
        action: 'accept',
        gatewayId: '', gatewayGroupId: '', useGroup: false,
        fallbackToDefault: false,
        log: false, comment: '',
      };
    },

    _buildFirewallPayload(form) {
      const buildEp = (ep) => {
        if (!ep || ep.type === 'any') {
          // Even 'any' endpoints may carry port info
          const portInfo = ep?.portMode === 'alias'
            ? { port: null,               portAliasId: ep.portAliasId || null }
            : { port: ep?.port || null,   portAliasId: null };
          return { type: 'any', invert: false, ...portInfo };
        }
        const portInfo = ep.portMode === 'alias'
          ? { port: null,           portAliasId: ep.portAliasId || null }
          : { port: ep.port || null, portAliasId: null };
        const base = { type: ep.type, invert: Boolean(ep.invert), ...portInfo };
        if (ep.type === 'alias') return { ...base, aliasId: ep.aliasId };
        if (ep.type === 'cidr')  return { ...base, value: ep.value };
        return { type: 'any', invert: false, port: null, portAliasId: null };
      };
      return {
        name:           form.name,
        interface:      form.interface  || 'any',
        protocol:       form.protocol   || 'any',
        source:         buildEp(form.source),
        destination:    buildEp(form.destination),
        action:         form.action     || 'accept',
        gatewayId:         form.useGroup ? null : (form.gatewayId || null),
        gatewayGroupId:    form.useGroup ? (form.gatewayGroupId || null) : null,
        fallbackToDefault: Boolean(form.fallbackToDefault),
        log:               Boolean(form.log),
        comment:           form.comment || '',
      };
    },

    openFirewallCreate() {
      this._resetFirewallCreate();
      this.showFirewallCreate = true;
    },

    async createFirewallRule() {
      try {
        const payload = this._buildFirewallPayload(this.firewallCreate);
        await this.api.createFirewallRule(payload);
        this.showFirewallCreate = false;
        this._resetFirewallCreate();
        await this.loadFirewallRules();
        this.showToast('Firewall rule created', 'success');
      } catch (err) {
        this.showToast(err.message || 'Failed to create firewall rule', 'error');
      }
    },

    openFirewallEdit(rule) {
      const loadEp = (ep) => {
        if (!ep) return { type: 'any', aliasId: '', value: '', invert: false, portMode: '', port: '', portAliasId: '' };
        const portMode     = ep.portAliasId ? 'alias' : (ep.port ? 'plain' : '');
        const portAliasId  = ep.portAliasId || '';
        const port         = ep.port        || '';
        return {
          type:        ep.type      || 'any',
          aliasId:     ep.aliasId   || '',
          value:       ep.value     || '',
          invert:      Boolean(ep.invert),
          portMode,
          port,
          portAliasId,
        };
      };
      this.firewallEdit = {
        id:          rule.id,
        name:        rule.name,
        interface:   rule.interface  || 'any',
        protocol:    rule.protocol   || 'any',
        source:      loadEp(rule.source),
        destination: loadEp(rule.destination),
        action:      rule.action || 'accept',
        gatewayId:      rule.gatewayId      || '',
        gatewayGroupId: rule.gatewayGroupId || '',
        useGroup:          !!rule.gatewayGroupId,
        fallbackToDefault: Boolean(rule.fallbackToDefault),
        log:               Boolean(rule.log),
        comment:           rule.comment || '',
      };
      this.showFirewallEdit = true;
    },

    async saveFirewallEdit() {
      try {
        const payload = { id: this.firewallEdit.id, ...this._buildFirewallPayload(this.firewallEdit) };
        await this.api.updateFirewallRule(payload);
        this.showFirewallEdit = false;
        await this.loadFirewallRules();
        this.showToast('Firewall rule updated', 'success');
      } catch (err) {
        this.showToast(err.message || 'Failed to update firewall rule', 'error');
      }
    },

    async toggleFirewallRule(rule) {
      try {
        await this.api.toggleFirewallRule({ id: rule.id, enabled: !rule.enabled });
        await this.loadFirewallRules();
      } catch (err) {
        this.showToast(err.message || 'Failed to toggle firewall rule', 'error');
      }
    },

    async deleteFirewallRule(rule) {
      if (!confirm(`Delete firewall rule "${rule.name}"?`)) return;
      try {
        await this.api.deleteFirewallRule({ id: rule.id });
        await this.loadFirewallRules();
        this.showToast('Firewall rule deleted', 'success');
      } catch (err) {
        this.showToast(err.message || 'Failed to delete firewall rule', 'error');
      }
    },

    async moveFirewallRule(rule, direction) {
      try {
        await this.api.moveFirewallRule({ id: rule.id, direction });
        await this.loadFirewallRules();
      } catch (err) {
        this.showToast(err.message || 'Failed to move firewall rule', 'error');
      }
    },

    openAddSeparator() {
      this.separatorEdit = { name: '', color: '' };
      this.separatorEditId = null;
      this.showSeparatorModal = true;
    },

    openEditSeparator(rule) {
      this.separatorEdit = { name: rule.name, color: rule.separatorColor || '' };
      this.separatorEditId = rule.id;
      this.showSeparatorModal = true;
    },

    async saveSeparator() {
      const { name, color } = this.separatorEdit;
      try {
        if (this.separatorEditId) {
          await this.api.updateFirewallRule({ id: this.separatorEditId, ruleType: 'separator', name: name.trim() || 'Separator', color });
        } else {
          await this.api.createFirewallRule({ ruleType: 'separator', name: name.trim() || 'Separator', color });
        }
        this.showSeparatorModal = false;
        await this.loadFirewallRules();
        this.showToast(this.separatorEditId ? 'Separator updated' : 'Separator added', 'success');
      } catch (err) {
        this.showToast(err.message || 'Failed to save separator', 'error');
      }
    },

    // Returns inline style for a separator row based on its color value.
    _sepRowStyle(color) {
      const map = {
        '':       { border: '#6b7280', bg: '#f3f4f6', darkBg: '#404040' },
        'red':    { border: '#ef4444', bg: '#fff0f0', darkBg: '#4a1a1a' },
        'orange': { border: '#f97316', bg: '#fff4eb', darkBg: '#4a2a10' },
        'yellow': { border: '#eab308', bg: '#fefce8', darkBg: '#3a3510' },
        'green':  { border: '#22c55e', bg: '#f0fdf4', darkBg: '#1a3d22' },
        'cyan':   { border: '#06b6d4', bg: '#ecfeff', darkBg: '#153040' },
        'blue':   { border: '#3b82f6', bg: '#eff6ff', darkBg: '#1a2a4a' },
        'purple': { border: '#a855f7', bg: '#faf5ff', darkBg: '#2e1a4a' },
      };
      const c = map[color] || map[''];
      const bg = this.theme === 'dark' ? c.darkBg : c.bg;
      return `background:${bg}; border-left:3px solid ${c.border};`;
    },

    _firewallEndpointLabel(ep) {
      if (!ep || ep.type === 'any') return 'Any';
      const inv = ep.invert ? 'NOT ' : '';
      let label = '';
      if (ep.type === 'alias') label = inv + this._aliasLabel(ep.aliasId);
      else if (ep.type === 'cidr') label = inv + (ep.value || '');
      else label = 'Any';
      if (ep.port) label += ':' + ep.port;
      return label;
    },

    _firewallGatewayLabel(rule) {
      if (rule.gatewayGroupId) {
        const g = (this.gatewayGroups || []).find(x => x.id === rule.gatewayGroupId);
        return g ? `Group: ${g.name}` : '—';
      }
      if (rule.gatewayId) {
        const g = (this.gateways || []).find(x => x.id === rule.gatewayId);
        return g ? g.name : '—';
      }
      return '—';
    },

    _firewallActionStyle(action, enabled) {
      if (!enabled) {
        // Disabled rule — muted grey badge regardless of action.
        return 'background:#e5e7eb; color:#9ca3af;';
      }
      if (action === 'accept') return 'background:#dcfce7; color:#15803d;';
      if (action === 'drop')   return 'background:#fee2e2; color:#dc2626;';
      if (action === 'reject') return 'background:#ffedd5; color:#ea580c;';
      return '';
    },

    // ========================================================================
    // Quick peer create (one-click, admin-tunnel style)
    // ========================================================================

    async createQuickPeer() {
      if (!this.activeInterfaceId) return;
      const name = this.peerCreateName;
      if (!name) return;

      try {
        const payload = {
          name,
          autoAllocateIP: true,
          generateKeys: true,
          expiredAt: this.peerCreateExpiredDate || undefined,
          groupId: this.peerCreateGroupId || this.defaultGroupId() || undefined,
        };

        const res = await this.api.createTunnelInterfacePeer({
          interfaceId: this.activeInterfaceId,
          ...payload,
        });

        const peerId = res.peer && res.peer.id;
        const showQR = this.peerCreateShowQR;
        this.showQuickPeerCreate = false;
        this.peerCreateName = '';
        this.peerCreateExpiredDate = '';
        this.peerCreateShowQR = false;
        this.inlineGroupShowQuick = false;
        this.inlineGroupInput = '';

        await this.refreshPeers();
        await this.loadTunnelInterfaces();
        this.loadClientGroups();
        this.loadAliases();

        if (showQR && peerId) {
          this.qrcode = this.peerQrUrl(this.activeInterfaceId, peerId);
        } else {
          this.showToast('Client created!');
        }
      } catch (err) {
        console.error('Failed to create peer:', err);
        this.showToast(`Failed: ${err.message}`, 'error');
      }
    },

    // ========================================================================
    // Peer management methods (admin-tunnel style)
    // ========================================================================

    // Returns the correct interfaceId for a peer action, works both in per-interface view and dashboard
    _peerIfaceId(peer) {
      return (peer && peer.interfaceId) || this.activeInterfaceId;
    },

    // Extract IP from runtimeEndpoint "IP:port" string (IPv4 and IPv6)
    peerQrUrl(interfaceId, peerId) {
      const base = this.activeRemoteId
        ? `./api/remotes/${this.activeRemoteId}/proxy/tunnel-interfaces/${interfaceId}/peers/${peerId}/qrcode.svg`
        : `./api/tunnel-interfaces/${interfaceId}/peers/${peerId}/qrcode.svg`;
      return base;
    },

    peerPublicIP(endpoint) {
      if (!endpoint) return '';
      if (endpoint.startsWith('[')) {
        // IPv6: [::1]:51820 → ::1
        return endpoint.slice(1, endpoint.indexOf(']'));
      }
      // IPv4: 1.2.3.4:51820 → 1.2.3.4
      return endpoint.split(':')[0];
    },

    // Refresh peers: if an interface tab is selected, refresh that interface; otherwise refresh all (dashboard)
    async _refreshPeersOrAll(opts = {}) {
      if (this.activeInterfaceId) {
        await this.refreshPeers(opts);
      } else {
        await this.refreshAllPeers(opts);
      }
    },

    async enablePeer(peer) {
      try {
        await this.api.enablePeer({ interfaceId: this._peerIfaceId(peer), peerId: peer.id });
        await this._refreshPeersOrAll();
      } catch (err) {
        this.showToast(err.message || err.toString(), 'error');
      }
    },

    async disablePeer(peer) {
      try {
        await this.api.disablePeer({ interfaceId: this._peerIfaceId(peer), peerId: peer.id });
        await this._refreshPeersOrAll();
      } catch (err) {
        this.showToast(err.message || err.toString(), 'error');
      }
    },

    async updatePeerName(peer, name) {
      try {
        await this.api.updatePeerName({ interfaceId: this._peerIfaceId(peer), peerId: peer.id, name });
        await this._refreshPeersOrAll();
      } catch (err) {
        this.showToast(err.message || err.toString(), 'error');
      }
    },

    async updatePeerAddress(peer, address) {
      try {
        await this.api.updatePeerAddress({ interfaceId: this._peerIfaceId(peer), peerId: peer.id, address });
        await this._refreshPeersOrAll();
      } catch (err) {
        this.showToast(err.message || err.toString(), 'error');
      }
    },

    async updatePeerExpireDate(peer, expireDate) {
      try {
        await this.api.updatePeerExpireDate({ interfaceId: this._peerIfaceId(peer), peerId: peer.id, expireDate });
        await this._refreshPeersOrAll();
      } catch (err) {
        this.showToast(err.message || err.toString(), 'error');
      }
    },

    async showPeerOneTimeLink(peer) {
      try {
        const res = await this.api.generatePeerOneTimeLink({ interfaceId: this._peerIfaceId(peer), peerId: peer.id });
        const token = (res.peer || {}).oneTimeLink;
        if (token) {
          const url = `${location.protocol}//${location.host}/cnf/${token}`;
          await navigator.clipboard.writeText(url);
          this.showToast('One-time link copied to clipboard', 'success');
        }
        await this._refreshPeersOrAll();
      } catch (err) {
        this.showToast(err.message || err.toString(), 'error');
      }
    },

    async confirmDeletePeer() {
      if (!this.peerDelete) return;
      const label = this.peerDelete.peerType === 'interconnect' ? 'Peer' : 'Client';
      try {
        await this.api.deleteTunnelInterfacePeer({
          interfaceId: this._peerIfaceId(this.peerDelete),
          peerId: this.peerDelete.id,
        });
        this.peerDelete = null;
        await this._refreshPeersOrAll();
        await this.loadTunnelInterfaces();
        if (label === 'Client') {
          this.loadClientGroups();
          this.loadAliases();
        }
        this.showToast(`${label} deleted!`);
      } catch (err) {
        this.showToast(err.message || err.toString(), 'error');
      }
    },

    openPeerEdit(peer) {
      this.peerEditForm = {
        _peer: peer,
        name: peer.name || '',
        persistentKeepalive: peer.persistentKeepalive || 0,
        endpoint: peer.endpoint || '',
        allowedIPs: peer.allowedIPs || '',
        clientAllowedIPs: peer.clientAllowedIPs || '',
        rateDown: peer.rateDown ? peer.rateDown / 1000 : 0,
        rateUp:   peer.rateUp   ? peer.rateUp   / 1000 : 0,
        groupId: peer.groupId || (peer.peerType === 'client' ? this.defaultGroupId() : ''),
        expiredAt: peer.expiredAt ? (typeof peer.expiredAt === 'string' ? peer.expiredAt.slice(0, 10) : new Date(peer.expiredAt).toISOString().slice(0, 10)) : '',
      };
      this.showPeerEditModal = true;
    },

    // Format kbps rate for display. 0 = unlimited.
    formatRate(kbps) {
      if (!kbps || kbps <= 0) return '';
      if (kbps >= 1000) return (kbps / 1000).toFixed(kbps % 1000 === 0 ? 0 : 1) + 'M';
      return kbps + 'K';
    },

    // Return the client group name for a given groupId. Returns '' if not found.
    peerGroupName(groupId) {
      if (!groupId) return '';
      const g = this.clientGroups.find(g => g.id === groupId);
      return g ? g.name : '';
    },

    // Returns effective rate limits for a peer: individual limits take precedence,
    // otherwise falls back to the peer's group limits.
    // Returns { rateDown, rateUp, fromGroup } — fromGroup=true when limits come from group.
    peerEffectiveRate(peer) {
      if (peer.rateDown > 0 || peer.rateUp > 0) {
        return { rateDown: peer.rateDown, rateUp: peer.rateUp, fromGroup: false };
      }
      if (peer.groupId) {
        const g = this.clientGroups.find(g => g.id === peer.groupId);
        if (g && (g.rateDown > 0 || g.rateUp > 0)) {
          return { rateDown: g.rateDown, rateUp: g.rateUp, fromGroup: true };
        }
      }
      return { rateDown: 0, rateUp: 0, fromGroup: false };
    },

    async savePeerEdit() {
      const peer = this.peerEditForm._peer;
      if (!peer) return;
      const isInterconnect = peer.peerType === 'interconnect';
      const updates = {
        name: this.peerEditForm.name,
        persistentKeepalive: Number(this.peerEditForm.persistentKeepalive) || 0,
      };
      if (isInterconnect) {
        updates.endpoint = this.peerEditForm.endpoint;
        updates.allowedIPs = this.peerEditForm.allowedIPs;
      } else {
        updates.clientAllowedIPs = this.peerEditForm.clientAllowedIPs;
        updates.rateDown = Math.round((Number(this.peerEditForm.rateDown) || 0) * 1000);
        updates.rateUp   = Math.round((Number(this.peerEditForm.rateUp)   || 0) * 1000);
        updates.groupId  = this.peerEditForm.groupId || '';
        updates.expiredAt = this.peerEditForm.expiredAt || '';
      }
      try {
        await this.api.updateTunnelInterfacePeer({
          interfaceId: this._peerIfaceId(peer),
          peerId: peer.id,
          ...updates,
        });
        this.showPeerEditModal = false;
        this.peerEditForm._peer = null;
        await this._refreshPeersOrAll();
        if (!isInterconnect) {
          this.loadClientGroups();
          this.loadAliases();
        }
        this.showToast(isInterconnect ? 'Peer updated' : 'Client updated', 'success');
      } catch (err) {
        this.showToast(err.message || err.toString(), 'error');
      }
    },

    /**
     * Refresh peers with transfer stats (called periodically like admin tunnel's refresh()).
     */
    async refreshPeers({ updateCharts = false } = {}) {
      if (!this.authenticated || !this.activeInterfaceId) return;

      try {
        const res = await this.api.getTunnelInterfacePeers({ interfaceId: this.activeInterfaceId });
        const peers = (res.peers || []).map(peer => {
          // Tag with interfaceId so actions work from dashboard too
          peer.interfaceId = this.activeInterfaceId;

          // Parse dates
          peer.createdAt = peer.createdAt ? new Date(peer.createdAt) : null;
          peer.updatedAt = peer.updatedAt ? new Date(peer.updatedAt) : null;
          peer.expiredAt = peer.expiredAt ? new Date(peer.expiredAt) : null;
          peer.latestHandshakeAt = peer.latestHandshakeAt ? new Date(peer.latestHandshakeAt) : null;

          // Avatar
          if (peer.name && this.avatarSettings.dicebear) {
            peer.avatar = `https://api.dicebear.com/9.x/${this.avatarSettings.dicebear}/svg?seed=${sha256(peer.name.toLowerCase().trim())}`;
          }

          // Transfer stats persistence (delta calculation for charts)
          if (!this.peersPersist[peer.id]) {
            this.peersPersist[peer.id] = {};
            this.peersPersist[peer.id].transferRxHistory = Array(50).fill(0);
            this.peersPersist[peer.id].transferRxPrevious = peer.transferRx || 0;
            this.peersPersist[peer.id].transferTxHistory = Array(50).fill(0);
            this.peersPersist[peer.id].transferTxPrevious = peer.transferTx || 0;
          }

          const pp = this.peersPersist[peer.id];
          pp.transferRxCurrent = (peer.transferRx || 0) - pp.transferRxPrevious;
          pp.transferRxPrevious = peer.transferRx || 0;
          pp.transferTxCurrent = (peer.transferTx || 0) - pp.transferTxPrevious;
          pp.transferTxPrevious = peer.transferTx || 0;

          if (updateCharts) {
            pp.transferRxHistory.push(pp.transferRxCurrent);
            pp.transferRxHistory.shift();
            pp.transferTxHistory.push(pp.transferTxCurrent);
            pp.transferTxHistory.shift();

            pp.transferTxSeries = [{ name: 'Tx', data: pp.transferTxHistory }];
            pp.transferRxSeries = [{ name: 'Rx', data: pp.transferRxHistory }];

            peer.transferTxHistory = pp.transferTxHistory;
            peer.transferRxHistory = pp.transferRxHistory;
            peer.transferMax = Math.max(...peer.transferTxHistory, ...peer.transferRxHistory);
            peer.transferTxSeries = pp.transferTxSeries;
            peer.transferRxSeries = pp.transferRxSeries;
          }

          peer.transferTxCurrent = pp.transferTxCurrent;
          peer.transferRxCurrent = pp.transferRxCurrent;
          peer.hoverTx = pp.hoverTx;
          peer.hoverRx = pp.hoverRx;

          return peer;
        });

        this.selectedInterfacePeers = peers;
      } catch (err) {
        console.error('refreshPeers failed:', err);
      }
    },

    /**
     * Dashboard mode: load peers from ALL interfaces into this.allPeers.
     * Each peer gets peer.interfaceId and peer.interfaceName set.
     */
    async refreshAllPeers({ updateCharts = false } = {}) {
      if (!this.authenticated) return;
      const all = [];
      for (const iface of this.tunnelInterfaces) {
        try {
          const res = await this.api.getTunnelInterfacePeers({ interfaceId: iface.id });
          const peers = (res.peers || []).map(peer => {
            peer.interfaceId   = iface.id;
            peer.interfaceName = iface.name || iface.id;

            peer.createdAt = peer.createdAt ? new Date(peer.createdAt) : null;
            peer.updatedAt = peer.updatedAt ? new Date(peer.updatedAt) : null;
            peer.expiredAt = peer.expiredAt ? new Date(peer.expiredAt) : null;
            peer.latestHandshakeAt = peer.latestHandshakeAt ? new Date(peer.latestHandshakeAt) : null;

            if (peer.name && this.avatarSettings.dicebear) {
              peer.avatar = `https://api.dicebear.com/9.x/${this.avatarSettings.dicebear}/svg?seed=${sha256(peer.name.toLowerCase().trim())}`;
            }

            if (!this.peersPersist[peer.id]) {
              this.peersPersist[peer.id] = {};
              this.peersPersist[peer.id].transferRxHistory  = Array(50).fill(0);
              this.peersPersist[peer.id].transferRxPrevious = peer.transferRx || 0;
              this.peersPersist[peer.id].transferTxHistory  = Array(50).fill(0);
              this.peersPersist[peer.id].transferTxPrevious = peer.transferTx || 0;
            }
            const pp = this.peersPersist[peer.id];
            pp.transferRxCurrent = (peer.transferRx || 0) - pp.transferRxPrevious;
            pp.transferRxPrevious = peer.transferRx || 0;
            pp.transferTxCurrent = (peer.transferTx || 0) - pp.transferTxPrevious;
            pp.transferTxPrevious = peer.transferTx || 0;

            if (updateCharts) {
              pp.transferRxHistory.push(pp.transferRxCurrent);
              pp.transferRxHistory.shift();
              pp.transferTxHistory.push(pp.transferTxCurrent);
              pp.transferTxHistory.shift();
              pp.transferTxSeries = [{ name: 'Tx', data: pp.transferTxHistory }];
              pp.transferRxSeries = [{ name: 'Rx', data: pp.transferRxHistory }];
              peer.transferTxHistory = pp.transferTxHistory;
              peer.transferRxHistory = pp.transferRxHistory;
              peer.transferMax       = Math.max(...peer.transferTxHistory, ...peer.transferRxHistory);
              peer.transferTxSeries  = pp.transferTxSeries;
              peer.transferRxSeries  = pp.transferRxSeries;
            }
            peer.transferTxCurrent = pp.transferTxCurrent;
            peer.transferRxCurrent = pp.transferRxCurrent;
            peer.hoverTx = pp.hoverTx;
            peer.hoverRx = pp.hoverRx;

            return peer;
          });
          all.push(...peers);
        } catch (err) {
          // skip failed interface silently
        }
      }
      this.allPeers = all;
    },

    async backupInterface() {
      if (!this.activeInterfaceId) return;
      try {
        const data = await this.api.backupTunnelInterface({ interfaceId: this.activeInterfaceId });
        const blob = new Blob([JSON.stringify(data, null, 2)], { type: 'application/json' });
        const url = window.URL.createObjectURL(blob);
        const a = document.createElement('a');
        a.href = url;
        a.download = `${this.activeInterfaceId}.json`;
        document.body.appendChild(a);
        a.click();
        document.body.removeChild(a);
        window.URL.revokeObjectURL(url);
      } catch (err) {
        this.showToast(`Backup failed: ${err.message}`, 'error');
      }
    },

    restoreInterface(e) {
      if (!this.activeInterfaceId) return;
      const fileInput = e.currentTarget.files.item(0);
      if (!fileInput) return;
      fileInput.text()
        .then((content) => {
          const file = JSON.parse(content);
          return this.api.restoreTunnelInterface({ interfaceId: this.activeInterfaceId, file });
        })
        .then(() => {
          this.showToast('Configuration restored!');
          this.refreshPeers();
          this.loadTunnelInterfaces();
        })
        .catch((err) => this.showToast(`Restore failed: ${err.message}`, 'error'));
    },

    // ============================================================
    // Interconnect Export / Import
    // ============================================================

    /**
     * Export THIS interface's parameters as JSON for the remote side to import.
     *
     * Workflow: this side clicks "Export My Params" → sends file to remote side →
     * remote side clicks "Import JSON" → peer for us is created automatically.
     *
     * File contains: name, publicKey, endpoint, address, protocol, AWG2 settings.
     * Remote side derives AllowedIPs subnet from our address (10.x.x.1/24 → 10.x.x.0/24).
     */
    async exportMyInterfaceParams(iface) {
      try {
        const params = await this.api.exportInterfaceParams({ interfaceId: iface.id });
        const blob = new Blob([JSON.stringify(params, null, 2)], { type: 'application/json' });
        const url = URL.createObjectURL(blob);
        const a = document.createElement('a');
        a.href = url;
        a.download = `${iface.id}-params.json`;
        a.click();
        URL.revokeObjectURL(url);
      } catch (err) {
        this.showToast(`Failed to export interface params: ${err.message}`, 'error');
      }
    },

    /**
     * Import remote side's interface params → create an interconnect peer for them.
     *
     * Workflow: remote side sends their export file → we click "Import JSON" →
     * peer is created automatically with their publicKey, endpoint, and derived AllowedIPs.
     */
    importInterconnectPeerJSON() {
      if (!this.activeInterfaceId) return;
      const input = document.createElement('input');
      input.type = 'file';
      input.accept = '.json,application/json';
      input.onchange = async (e) => {
        const file = e.target.files[0];
        if (!file) return;
        try {
          const text = await file.text();
          const data = JSON.parse(text);
          const res = await this.api.importPeerJSON({ interfaceId: this.activeInterfaceId, ...data });
          await this.refreshPeers();
          await this.loadTunnelInterfaces();

          // Если PSK был в файле — он уже согласован. Если мы его сгенерили —
          // нужно передать его другой стороне через Export My Params.
          const pskWasInFile = !!data.presharedKey;
          if (pskWasInFile) {
            this.showToast('Peer imported! PSK taken from the file — both sides are in sync.');
          } else {
            this.showToast('Peer imported! PSK generated — export your params and send to the remote side.', 'success', 10000);
          }
        } catch (err) {
          this.showToast(`Failed to import peer: ${err.message}`, 'error');
        }
      };
      input.click();
    },

    async toggleDisableRoutes(iface) {
      try {
        await this.api.updateTunnelInterface({
          interfaceId: iface.id,
          disableRoutes: !iface.disableRoutes,
        });
        await this.loadTunnelInterfaces();
      } catch (err) {
        this.showToast(`Failed: ${err.message}`, 'error');
      }
    },

    // ============================================================
    // Settings & Templates
    // ============================================================

    async loadSettings() {
      try {
        this.globalSettings = await this.api.getSettings();
        if (typeof this.globalSettings.chartType === 'number') {
          this.uiChartType = this.globalSettings.chartType;
        }
        if (this.globalSettings.lang && i18n.availableLocales.includes(this.globalSettings.lang)) {
          i18n.locale = this.globalSettings.lang;
          localStorage.setItem('lang', this.globalSettings.lang);
        }
        // Capture local server name once — never overwrite with remote settings.
        if (!this.activeRemoteId) {
          this.localServerName = this.globalSettings.routerName || this.globalSettings.hostname || '';
        }
        const { templates } = await this.api.getTemplates();
        this.templates = templates;
      } catch (err) {
        console.error('loadSettings failed:', err);
      }
    },

    async saveSettings() {
      try {
        // Strip runtime-only fields and fields managed by dedicated save handlers.
        // defaultFwPolicy has its own saveDefaultFwPolicy() path — exclude here to
        // avoid triggering an unnecessary firewall RebuildChains on every Settings save.
        const { hostname, resolvedPublicIP, publicIPWarning, defaultFwPolicy, ...storable } = this.globalSettings;
        const updated = await this.api.updateSettings(storable);
        // Merge response back (includes fresh resolvedPublicIP / hostname).
        this.globalSettings = { ...this.globalSettings, ...updated };
        // Apply language change immediately.
        if (updated.lang && i18n.availableLocales.includes(updated.lang)) {
          i18n.locale = updated.lang;
          localStorage.setItem('lang', updated.lang);
        }
        this.settingsSaved = true;
        setTimeout(() => { this.settingsSaved = false; }, 2500);
      } catch (err) {
        this.showToast(`Failed to save settings: ${err.message}`, 'error');
      }
    },

    async saveDefaultFwPolicy() {
      const policy = this.globalSettings.defaultFwPolicy;
      // Warn before applying DROP — remote sessions may lose connectivity if no
      // matching ACCEPT rule exists for management traffic (SSH, WireGuard, etc.).
      if (policy === 'drop') {
        const ok = window.confirm(
          'Set default policy to DROP?\n\n' +
          'All forward traffic not matched by an explicit rule above will be silently discarded.\n\n' +
          'Make sure you have ACCEPT rules covering your management traffic (SSH, WireGuard peers, etc.) ' +
          'before applying — otherwise you may lose remote access.'
        );
        if (!ok) {
          // Revert the select back to accept without saving
          await this.loadSettings();
          return;
        }
      }
      try {
        const updated = await this.api.updateSettings({ defaultFwPolicy: policy });
        this.globalSettings = { ...this.globalSettings, ...updated };
        this.showToast(`Default policy set to ${policy.toUpperCase()}`, 'success');
      } catch (err) {
        this.showToast(`Failed to update policy: ${err.message}`, 'error');
        // Revert optimistic change
        await this.loadSettings();
      }
    },

    // H1-H4: non-overlapping random ranges in the uint32 space.
    // 4 equal zones, each gets a ~50 M wide sub-range.
    randomiseTemplateH() {
      const RANGE_SIZE = 50_000_000;
      const ZONE_SIZE = Math.floor((0xFFFFFFFF - 5) / 4);
      const r = (zone) => {
        const zs = 5 + zone * ZONE_SIZE;
        const ze = zs + ZONE_SIZE - 1;
        const start = zs + Math.floor(Math.random() * (ze - zs - RANGE_SIZE));
        return `${start}-${start + RANGE_SIZE}`;
      };
      this.templateForm.h1 = r(0);
      this.templateForm.h2 = r(1);
      this.templateForm.h3 = r(2);
      this.templateForm.h4 = r(3);
    },

    openTemplateCreate() {
      this.templateForm = {
        name: '',
        isDefault: false,
        jc: 6, jmin: 10, jmax: 50,
        s1: 64, s2: 67, s3: 64, s4: 4,
        h1: '', h2: '', h3: '', h4: '',
        i1: '', i2: '', i3: '', i4: '', i5: '',
      };
      this.randomiseTemplateH();
      this.templateEditTarget = null;
      this.showTemplateModal = true;
    },

    openTemplateEdit(tmpl) {
      this.templateForm = { ...tmpl };
      this.templateEditTarget = tmpl;
      this.showTemplateModal = true;
    },

    async saveTemplate() {
      try {
        if (this.templateEditTarget) {
          await this.api.updateTemplate({ templateId: this.templateEditTarget.id, ...this.templateForm });
        } else {
          await this.api.createTemplate(this.templateForm);
        }
        this.showTemplateModal = false;
        await this.loadSettings();
      } catch (err) {
        this.showToast(`Failed to save template: ${err.message}`, 'error');
      }
    },

    async setDefaultTemplate(templateId) {
      try {
        await this.api.setDefaultTemplate({ templateId });
        await this.loadSettings();
      } catch (err) {
        this.showToast(`Failed: ${err.message}`, 'error');
      }
    },

    async deleteTemplate(templateId) {
      if (!confirm('Delete this template?')) return;
      try {
        await this.api.deleteTemplate({ templateId });
        await this.loadSettings();
      } catch (err) {
        this.showToast(`Failed to delete template: ${err.message}`, 'error');
      }
    },

    /**
     * Экспортировать профиль обфускации в JSON файл.
     * Скачивает файл с AWG2 параметрами шаблона.
     * Формат: { name, jc, jmin, jmax, s1-s4, h1-h4, i1-i5 }.
     * Поля meta (id, isDefault, createdAt) не включаются — они специфичны для этого сервера.
     */
    exportTemplateJSON(tmpl) {
      const params = {
        name: tmpl.name,
        jc: tmpl.jc, jmin: tmpl.jmin, jmax: tmpl.jmax,
        s1: tmpl.s1, s2: tmpl.s2, s3: tmpl.s3, s4: tmpl.s4,
        h1: tmpl.h1, h2: tmpl.h2, h3: tmpl.h3, h4: tmpl.h4,
        i1: tmpl.i1 || null, i2: tmpl.i2 || null, i3: tmpl.i3 || null,
        i4: tmpl.i4 || null, i5: tmpl.i5 || null,
      };
      const blob = new Blob([JSON.stringify(params, null, 2)], { type: 'application/json' });
      const url = URL.createObjectURL(blob);
      const a = document.createElement('a');
      a.href = url;
      a.download = `awg2-profile-${tmpl.name.replace(/[^a-zA-Z0-9_-]/g, '-')}.json`;
      a.click();
      URL.revokeObjectURL(url);
    },

    /**
     * Импортировать профиль обфускации из JSON файла.
     * Открывает выбор файла → читает JSON → создаёт новый шаблон через API.
     * Если в JSON нет поля name — запрашивает имя у пользователя.
     * Формат файла должен содержать AWG2 параметры (jc, jmin, ..., h1-h4).
     */
    importTemplateJSON() {
      const input = document.createElement('input');
      input.type = 'file';
      input.accept = '.json,application/json';
      input.onchange = async (e) => {
        const file = e.target.files[0];
        if (!file) return;
        try {
          const text = await file.text();
          const data = JSON.parse(text);
          // Если имени нет в файле — просим пользователя
          if (!data.name) {
            const name = prompt('Enter a name for this profile:', file.name.replace(/\.json$/i, ''));
            if (!name) return;
            data.name = name;
          }
          await this.api.createTemplate(data);
          await this.loadSettings();
        } catch (err) {
          this.showToast(`Failed to import profile: ${err.message}`, 'error');
        }
      };
      input.click();
    },

    // ── Generate AWG2 modal ───────────────────────────────────────────────────

    openGenerateModal() {
      this.generateForm = { profile: 'random', intensity: 'medium', host: '', browser: '', saveName: '' };
      this.generatedParams = null;
      this.showGenerateModal = true;
    },

    async generateParams() {
      this.generatingParams = true;
      try {
        const res = await this.api.generateTemplate({
          profile:   this.generateForm.profile,
          intensity: this.generateForm.intensity,
          host:      this.generateForm.host || undefined,
          browser:   this.generateForm.browser || undefined,
        });
        this.generatedParams = res.params;
        if (!this.generateProfiles.length && res.profiles) {
          this.generateProfiles = res.profiles;
        }
      } catch (err) {
        this.showToast(`Generate failed: ${err.message}`, 'error');
      } finally {
        this.generatingParams = false;
      }
    },

    async saveGeneratedTemplate() {
      if (!this.generatedParams) return;
      const name = this.generateForm.saveName.trim();
      if (!name) {
        this.showToast('Enter a template name before saving', 'error');
        return;
      }
      try {
        await this.api.createTemplate({ name, ...this.generatedParams });
        await this.loadSettings();
        this.showGenerateModal = false;
        this.showToast(`Profile "${name}" saved`, 'success');
      } catch (err) {
        this.showToast(`Save failed: ${err.message}`, 'error');
      }
    },

    useGeneratedInForm() {
      if (!this.generatedParams) return;
      const p = this.generatedParams;
      this.templateForm = {
        name: this.generateForm.saveName || '',
        isDefault: false,
        host: this.generateForm.host || '',
        jc: p.jc, jmin: p.jmin, jmax: p.jmax,
        s1: p.s1, s2: p.s2, s3: p.s3, s4: p.s4,
        h1: p.h1, h2: p.h2, h3: p.h3, h4: p.h4,
        i1: p.i1 || '', i2: p.i2 || '', i3: p.i3 || '',
        i4: p.i4 || '', i5: p.i5 || '',
      };
      this.templateEditTarget = null;
      this.showGenerateModal = false;
      this.showTemplateModal = true;
    },

    /**
     * Заполнить AWG2 поля формы из выбранного шаблона профиля обфускации.
     * Вызывается при смене значения в дропдауне "Obfuscation Profile".
     * Если templateId пустой ("-- Manual entry --") — поля НЕ сбрасываются,
     * пользователь продолжает вводить вручную.
     */
    onInterfaceTemplateSelect(templateId) {
      if (!templateId) return; // manual entry — не трогаем поля
      const tmpl = (this.templates || []).find(t => t.id === templateId);
      if (!tmpl) return;
      this.interfaceCreate.settings = {
        jc: tmpl.jc,    jmin: tmpl.jmin,  jmax: tmpl.jmax,
        s1: tmpl.s1,    s2: tmpl.s2,      s3: tmpl.s3,   s4: tmpl.s4,
        h1: tmpl.h1,    h2: tmpl.h2,      h3: tmpl.h3,   h4: tmpl.h4,
        i1: tmpl.i1 || '', i2: tmpl.i2 || '', i3: tmpl.i3 || '',
        i4: tmpl.i4 || '', i5: tmpl.i5 || '',
      };
    },

    // ========================================================================
    // Users Management
    // ========================================================================

    async loadUsers() {
      try {
        const res = await this.api.getUsers();
        this.users = res.users || [];
      } catch (err) {
        console.error('loadUsers failed:', err);
        this.users = [];
      }
    },

    async loadCurrentUser() {
      try {
        this.currentUser = await this.api.getCurrentUser();
      } catch (err) {
        this.currentUser = null;
      }
    },

    async createFirstUser() {
      const { username, password, passwordConfirm } = this.firstRunForm;
      if (!username) { this.showToast('Username is required', 'error'); return; }
      if (!password) { this.showToast('Password is required', 'error'); return; }
      if (password.length < 8) { this.showToast('Password must be at least 8 characters', 'error'); return; }
      if (password !== passwordConfirm) { this.showToast('Passwords do not match', 'error'); return; }
      this.firstRunSaving = true;
      try {
        await this.api.createUser({ username, password });
        // Open mode ended — reload to show login screen with real authentication.
        this.firstRunSaving = false;
        window.location.reload();
      } catch (err) {
        this.showToast(err.message || 'Failed to create admin account', 'error');
        this.firstRunSaving = false;
      }
    },

    async createUser() {
      const { username, password, passwordConfirm } = this.addUserForm;
      if (!username) { this.showToast('Username is required', 'error'); return; }
      if (!password) { this.showToast('Password is required', 'error'); return; }
      if (password.length < 8) { this.showToast('Password must be at least 8 characters', 'error'); return; }
      if (password !== passwordConfirm) { this.showToast('Passwords do not match', 'error'); return; }
      try {
        await this.api.createUser({ username, password });
        this.showAddUserModal = false;
        this.addUserForm = { username: '', password: '', passwordConfirm: '' };
        await this.loadUsers();
        this.showToast(`User "${username}" created`);
      } catch (err) {
        this.showToast(err.message || 'Failed to create user', 'error');
      }
    },

    async deleteUser(user) {
      if (!confirm(`Delete user "${user.username}"?`)) return;
      try {
        await this.api.deleteUser(user.id);
        await this.loadUsers();
        this.showToast(`User "${user.username}" deleted`);
      } catch (err) {
        this.showToast(err.message || 'Failed to delete user', 'error');
      }
    },

    async setUserAdmin(user) {
      const granting = !user.is_admin;
      const action = granting ? `grant admin to "${user.username}"` : `remove admin from "${user.username}"`;
      if (!confirm(`Are you sure you want to ${action}?`)) return;
      try {
        const res = await this.api.setUserAdmin(user.id, granting);
        if (res && res.user) {
          const idx = this.users.findIndex(u => u.id === res.user.id);
          if (idx !== -1) this.users.splice(idx, 1, res.user);
          if (this.currentUser && res.user.id === this.currentUser.id) {
            this.currentUser = res.user;
          }
        }
        this.showToast(granting ? `Admin granted to "${user.username}"` : `Admin removed from "${user.username}"`);
      } catch (err) {
        this.showToast(err.message || 'Failed to update admin role', 'error');
      }
    },

    // ========================================================================
    // API Tokens
    // ========================================================================

    async loadApiTokens() {
      try {
        const res = await this.api.getApiTokens();
        this.apiTokens = res.tokens || [];
      } catch (err) {
        this.apiTokens = [];
      }
    },

    async createApiToken() {
      const { name } = this.createTokenForm;
      if (!name) { this.showToast('Token name is required', 'error'); return; }
      try {
        const res = await this.api.createApiToken({ name });
        this.showCreateTokenModal = false;
        this.createTokenForm = { name: '' };
        this.newTokenValue = res.raw_token || '';
        this.showNewTokenModal = true;
        await this.loadApiTokens();
      } catch (err) {
        this.showToast(err.message || 'Failed to create token', 'error');
      }
    },

    async deleteApiToken(token) {
      if (!confirm(`Revoke token "${token.name}"? This cannot be undone.`)) return;
      try {
        await this.api.deleteApiToken({ id: token.id });
        await this.loadApiTokens();
        this.showToast(`Token "${token.name}" revoked`);
      } catch (err) {
        this.showToast(err.message || 'Failed to revoke token', 'error');
      }
    },

    copyTokenToClipboard() {
      if (!this.newTokenValue) return;
      navigator.clipboard.writeText(this.newTokenValue)
        .then(() => this.showToast('Token copied to clipboard'))
        .catch(() => this.showToast('Failed to copy — select and copy manually', 'error'));
    },

    // ========================================================================
    // TOTP Setup
    // ========================================================================

    async openTOTPSetup() {
      try {
        const res = await this.api.getTOTPSetup();
        this.totpSetupSecret = res.secret || '';
        this.totpSetupQrPng  = res.qr_png  || '';
        this.totpSetupQrUri  = res.qr_uri  || '';
        this.totpSetupCode   = '';
        this.totpSetupSaving = false;
        this.showTOTPSetupModal = true;
      } catch (err) {
        this.showToast(err.message || 'Failed to start TOTP setup', 'error');
      }
    },

    async confirmTOTPEnable() {
      if (!this.totpSetupCode) { this.showToast('Enter the 6-digit code', 'error'); return; }
      this.totpSetupSaving = true;
      try {
        const res = await this.api.enableTOTP({ code: this.totpSetupCode });
        this.showTOTPSetupModal = false;
        this.totpSetupCode = '';
        // Update the user list and current user to reflect totp_enabled=true.
        if (res && res.user) {
          this.currentUser = res.user;
          const idx = this.users.findIndex(u => u.id === res.user.id);
          if (idx !== -1) this.users.splice(idx, 1, res.user);
        }
        this.showToast('Two-factor authentication enabled');
      } catch (err) {
        this.showToast(err.message || 'Failed to enable 2FA', 'error');
      } finally {
        this.totpSetupSaving = false;
      }
    },

    openTOTPDisable() {
      this.totpDisableCode = '';
      this.showTOTPDisableModal = true;
    },

    async confirmTOTPDisable() {
      if (!this.totpDisableCode) { this.showToast('Enter the 6-digit code', 'error'); return; }
      try {
        const res = await this.api.disableTOTP({ code: this.totpDisableCode });
        this.showTOTPDisableModal = false;
        this.totpDisableCode = '';
        if (res && res.user) {
          this.currentUser = res.user;
          const idx = this.users.findIndex(u => u.id === res.user.id);
          if (idx !== -1) this.users.splice(idx, 1, res.user);
        }
        this.showToast('Two-factor authentication disabled');
      } catch (err) {
        this.showToast(err.message || 'Failed to disable 2FA', 'error');
      }
    },
    // ========================================================================
    // Wizard: Simple Client VPN
    // ========================================================================

    // Returns a random low port (400–999) not used by any existing interface.
    // 443 is excluded — Caddy binds UDP 443 for QUIC/HTTP3 in the standard deploy.
    wizardVPNPickPort() {
      const usedPorts = new Set((this.tunnelInterfaces || []).map(i => i.listenPort));
      usedPorts.add(443); // always skip — Caddy QUIC
      for (let attempts = 0; attempts < 200; attempts++) {
        const p = Math.floor(Math.random() * 600) + 400; // 400–999
        if (!usedPorts.has(p)) return p;
      }
      return 8443; // last-resort fallback
    },

    wizardVPNInit() {
      this.wizardVPN = {
        step: 1,
        protocol: 'wireguard',
        ifaceName: '',
        dns: this.globalSettings.dns || '',
        peerName: 'My Device',
        running: false,
        error: '',
        ifaceId: '',
        peerId: '',
        qrUrl: '',
        ifaceAddr: '',
        ifacePort: 0,
        startWarning: '',
      };
    },

    async wizardVPNRun() {
      this.wizardVPN.running = true;
      this.wizardVPN.error = '';
      try {
        const proto = this.wizardVPN.protocol;
        let awgParams = null;

        // Generate AWG obfuscation params if needed
        if (proto === 'amneziawg') {
          const host = (this.globalSettings.resolvedPublicIP || '').trim();
          const isIPv4 = /^\d+\.\d+\.\d+\.\d+$/.test(host);
          const isIPv6 = host.includes(':');
          const isDomain = host && !isIPv4 && !isIPv6;
          const profile = isDomain ? 'quic_burst' : 'tls_client_hello';
          const res = await this.api.call({
            method: 'post',
            path: '/templates/generate',
            body: { profile, intensity: 'medium', host: isDomain ? host : '' },
          });
          awgParams = res.params;
        }

        // Pick a low port (443 preferred, fallback random 400–999)
        const wizardPort = this.wizardVPNPickPort();

        // Create + start interface via quick-create
        const qcBody = {
          protocol: proto === 'amneziawg' ? 'amneziawg-2.0' : 'wireguard-1.0',
        };
        const trimmedName = (this.wizardVPN.ifaceName || '').trim();
        if (trimmedName) qcBody.name = trimmedName;

        const qcData = await this.api.call({ method: 'post', path: '/tunnel-interfaces/quick-create', body: qcBody });
        const ifaceId = qcData.interface.id;
        this.wizardVPN.ifaceId = ifaceId;
        this.wizardVPN.ifaceAddr = qcData.interface.address || '';

        // Apply port, DNS override and/or AWG params via PATCH (always needed for port)
        const patch = { listenPort: wizardPort };
        if (this.wizardVPN.dns) patch.dns = this.wizardVPN.dns;
        if (awgParams) patch.settings = awgParams;
        await this.api.call({ method: 'patch', path: `/tunnel-interfaces/${ifaceId}`, body: patch });
        // PATCH with listenPort triggers restart automatically (wg-quick rebind)
        this.wizardVPN.ifacePort = wizardPort;

        // Create first peer
        const peerRes = await this.api.call({
          method: 'post',
          path: `/tunnel-interfaces/${ifaceId}/peers`,
          body: {
            name: (this.wizardVPN.peerName || '').trim() || 'My Device',
            peerType: 'client',
            generateKeys: true,
            autoAllocateIP: true,
            persistentKeepalive: 25,
          },
        });
        const peerId = peerRes.peer.id;
        this.wizardVPN.peerId = peerId;
        this.wizardVPN.qrUrl = this.peerQrUrl(ifaceId, peerId);
        this.wizardVPN.step = 4;

        // Wait for the async restart goroutine (PATCH with listenPort triggers
        // restart in a goroutine — the API returns before wg-quick finishes).
        await new Promise(r => setTimeout(r, 2500));
        await this.loadTunnelInterfaces();
        const created = this.tunnelInterfaces.find(i => i.id === ifaceId);
        if (created && !created.enabled) {
          this.wizardVPN.startWarning = `Interface ${ifaceId} was created but failed to start — UDP port ${wizardPort} may be in use. Go to Interfaces to start it manually or change the port.`;
        }
      } catch (err) {
        this.wizardVPN.error = err.message || 'Unknown error';
      } finally {
        this.wizardVPN.running = false;
      }
    },

    wizardVPNReset() {
      this.wizardVPNInit();
    },

    wizardVPNDownload() {
      this.downloadPeerConfig({
        id: this.wizardVPN.peerId,
        name: this.wizardVPN.peerName || 'My Device',
        interfaceId: this.wizardVPN.ifaceId,
      });
    },

    // ── Wizard: Cascade via WireGuard Uplink ─────────────────────────────────

    wizardUplinkInit() {
      const w = this.wizardUplink;
      w.step = 1;
      w.confText = '';
      w.confFileName = '';
      w.preview = null;
      w.ifaceName = '';
      w.selectedIfaceIds = [];
      w.createSrcAlias = true;
      w.srcAliasName = '';
      w.showInlineCreate = false;
      w.inlineIfaceName = '';
      w.inlineIfaceAddr = '';
      w.inlineIfacePort = '';
      w.inlineIfaceCreating = false;
      w.dstType = 'all';
      w.dstCountries = [];
      w.dstASN = '';
      w.dstNegate = false;
      w.dstAliasName = '';
      w.mssClamp = true;
      w.fallback = 'drop';
      w.gatewayName = '';
      w.fwRuleName = '';
      w.natRuleName = '';
      w.gatewayMonitorIP = '';
      w.applying = false;
      w.steps = [];
      w.createdIfaceId = '';
      w.createdSrcAliasId = '';
      w.createdDstAliasId = '';
      w.createdGatewayId = '';
      w.createdFwRuleId = '';
      w.createdNatRuleId = '';
      w.done = false;
      w.fatalError = '';
    },

    wizardUplinkOnFileSelect(event) {
      const file = event.target.files[0];
      if (!file) return;
      this.wizardUplink.confFileName = file.name.replace(/\.conf$/i, '');
      if (!this.wizardUplink.ifaceName) {
        this.wizardUplink.ifaceName = this.wizardUplink.confFileName;
      }
      const reader = new FileReader();
      reader.onload = (e) => {
        this.wizardUplink.confText = e.target.result;
        this.wizardUplinkPreview();
      };
      reader.readAsText(file);
    },

    async wizardUplinkPreview() {
      const w = this.wizardUplink;
      w.preview = null;
      if (!w.confText.trim()) return;
      try {
        const res = await this.api.parseTunnelConf({ conf: w.confText });
        w.preview = res;
        const base = (w.ifaceName || w.confFileName || 'uplink').trim();
        if (!w.srcAliasName) w.srcAliasName = 'src-' + base;
        if (!w.dstAliasName) w.dstAliasName = 'dst-' + base;
        if (!w.gatewayName) w.gatewayName = 'gw-' + base;
        if (!w.fwRuleName) w.fwRuleName = 'pbr-' + base;
        if (!w.natRuleName) w.natRuleName = 'nat-' + base;
        if (!w.gatewayMonitorIP) w.gatewayMonitorIP = res.peerMonitorIP || '';
      } catch (e) {
        // invalid conf — show nothing
      }
    },

    wizardUplinkOnNameChange() {
      const w = this.wizardUplink;
      const base = (w.ifaceName || 'uplink').trim();
      w.srcAliasName = 'src-' + base;
      w.dstAliasName = 'dst-' + base;
      w.gatewayName  = 'gw-'  + base;
      w.fwRuleName   = 'pbr-' + base;
      w.natRuleName  = 'nat-' + base;
    },

    wizardUplinkClientIfaces() {
      return this.tunnelInterfaces.filter(i => !i.disableRoutes);
    },

    wizardUplinkIfaceSubnet(iface) {
      if (!iface.address) return '';
      const parts = iface.address.split('/');
      if (parts.length !== 2) return iface.address;
      const ip = parts[0].split('.');
      const prefix = parseInt(parts[1], 10);
      ip[3] = '0';
      return ip.join('.') + '/' + (prefix <= 24 ? prefix : 24);
    },

    wizardUplinkToggleIface(id) {
      const idx = this.wizardUplink.selectedIfaceIds.indexOf(id);
      if (idx === -1) this.wizardUplink.selectedIfaceIds.push(id);
      else this.wizardUplink.selectedIfaceIds.splice(idx, 1);
    },

    wizardUplinkSelectedSubnets() {
      return this.wizardUplink.selectedIfaceIds.map(id => {
        const iface = this.tunnelInterfaces.find(i => i.id === id);
        return iface ? this.wizardUplinkIfaceSubnet(iface) : '';
      }).filter(Boolean);
    },

    async wizardUplinkInlineCreate() {
      const w = this.wizardUplink;
      w.inlineIfaceCreating = true;
      try {
        const body = { protocol: w.inlineIfaceProto };
        if (w.inlineIfaceName.trim()) body.name = w.inlineIfaceName.trim();
        if (w.inlineIfaceAddr.trim()) body.address = w.inlineIfaceAddr.trim();
        if (w.inlineIfacePort) body.listenPort = parseInt(w.inlineIfacePort, 10);
        const res = await this.api.createTunnelInterface(body);
        const newId = (res.interface || res).id;
        if (newId) {
          await this.api.startTunnelInterface({ interfaceId: newId }).catch(() => {});
        }
        await this.loadTunnelInterfaces();
        if (newId) w.selectedIfaceIds.push(newId);
        w.showInlineCreate = false;
        w.inlineIfaceName = '';
        w.inlineIfaceAddr = '';
        w.inlineIfacePort = '';
      } catch (e) {
        this.showToast(e.message || 'Failed to create interface', 'error');
      } finally {
        w.inlineIfaceCreating = false;
      }
    },

    // ── Apply ─────────────────────────────────────────────────────────────────

    wizardUplinkStepAdd(label) {
      this.wizardUplink.steps.push({ label, status: 'pending', detail: '' });
      return this.wizardUplink.steps.length - 1;
    },

    wizardUplinkStepSet(idx, status, detail) {
      this.$set(this.wizardUplink.steps, idx, {
        ...this.wizardUplink.steps[idx],
        status,
        detail: detail || '',
      });
    },

    async wizardUplinkApply() {
      const w = this.wizardUplink;
      w.applying = true;
      w.steps = [];
      w.done = false;
      w.fatalError = '';
      const base = (w.ifaceName || 'uplink').trim();

      // Step 1: Create uplink interface
      const s0 = this.wizardUplinkStepAdd('Creating uplink interface');
      this.wizardUplinkStepSet(s0, 'running');
      let ifaceId = '';
      try {
        const res = await this.api.importTunnelConf({ name: w.ifaceName.trim(), conf: w.confText });
        ifaceId = res.interface.id;
        w.createdIfaceId = ifaceId;
        await this.loadTunnelInterfaces();
        this.wizardUplinkStepSet(s0, 'ok', ifaceId + (res.started ? '' : ' (not started)'));
      } catch (e) {
        this.wizardUplinkStepSet(s0, 'error', e.message);
        w.fatalError = e.message;
        w.applying = false;
        return;
      }

      // Step 2: Propagate MTU to client interfaces (if uplink conf specifies MTU)
      const uplinkMTU = w.preview && w.preview.mtu ? parseInt(w.preview.mtu, 10) : 0;
      if (uplinkMTU > 0 && w.selectedIfaceIds.length > 0) {
        const s1b = this.wizardUplinkStepAdd('Setting client interface MTU');
        this.wizardUplinkStepSet(s1b, 'running');
        try {
          await Promise.all(w.selectedIfaceIds.map(id =>
            this.api.updateTunnelInterface({ interfaceId: id, mtu: uplinkMTU })
          ));
          this.wizardUplinkStepSet(s1b, 'ok', uplinkMTU + ' bytes');
        } catch (e) {
          this.wizardUplinkStepSet(s1b, 'warn', e.message + ' — continuing');
        }
      }

      // Step 3: Ping check
      const s1 = this.wizardUplinkStepAdd('Checking reachability');
      this.wizardUplinkStepSet(s1, 'running');
      try {
        const host = w.preview && w.preview.peerEndpoint
          ? w.preview.peerEndpoint.split(':')[0] : '';
        if (host) {
          const pr = await this.api.ping({ host, count: 3 });
          if (pr.reachable) {
            this.wizardUplinkStepSet(s1, 'ok', host + ' — ' + pr.latencyMs + 'ms');
          } else {
            this.wizardUplinkStepSet(s1, 'warn', host + ' not reachable — continuing');
          }
        } else {
          this.wizardUplinkStepSet(s1, 'warn', 'No endpoint — skipped');
        }
      } catch (e) {
        this.wizardUplinkStepSet(s1, 'warn', 'Ping failed — continuing');
      }

      // Step 3: Source alias
      if (w.createSrcAlias && w.selectedIfaceIds.length > 0) {
        const s2 = this.wizardUplinkStepAdd('Creating source alias');
        this.wizardUplinkStepSet(s2, 'running');
        try {
          const subnets = this.wizardUplinkSelectedSubnets();
          const res = await this.api.createAlias({
            name: w.srcAliasName || ('src-' + base),
            type: 'network',
            entries: subnets,
          });
          w.createdSrcAliasId = (res.alias || res).id || '';
          this.wizardUplinkStepSet(s2, 'ok', subnets.join(', '));
        } catch (e) {
          this.wizardUplinkStepSet(s2, 'warn', e.message + ' — continuing');
        }
      }

      // Step 4: Destination alias (GEO / AS)
      if (w.dstType !== 'all') {
        const s3 = this.wizardUplinkStepAdd('Creating destination alias');
        this.wizardUplinkStepSet(s3, 'running');
        try {
          const res = await this.api.createAlias({
            name: w.dstAliasName || ('dst-' + base),
            type: 'ipset',
            entries: [],
          });
          const aliasId = (res.alias || res).id || '';
          w.createdDstAliasId = aliasId;
          const genBody = w.dstType === 'geo'
            ? { country: w.dstCountries.join(','), asn: '', asnList: '' }
            : { country: '', asn: w.dstASN.trim(), asnList: '' };
          const genRes = await this.api.generateAlias({ id: aliasId, ...genBody });
          const jobId = genRes.jobId;
          let finished = false;
          while (!finished) {
            await new Promise(r => setTimeout(r, 1000));
            const st = await this.api.getAliasJobStatus({ id: aliasId, jobId });
            this.wizardUplinkStepSet(s3, 'running', (st.progress || 0) + '% loaded…');
            if (st.status === 'done') { finished = true; }
            if (st.status === 'error') throw new Error(st.error || 'Generation failed');
          }
          this.wizardUplinkStepSet(s3, 'ok', w.dstAliasName + ' created');
        } catch (e) {
          this.wizardUplinkStepSet(s3, 'warn', e.message + ' — continuing');
        }
      }

      // Step 5: MSS clamping
      if (w.mssClamp && ifaceId) {
        const s4 = this.wizardUplinkStepAdd('Enabling MSS clamping');
        this.wizardUplinkStepSet(s4, 'running');
        try {
          await this.api.updateTunnelInterface({ interfaceId: ifaceId, mss: -1 });
          this.wizardUplinkStepSet(s4, 'ok', 'Auto PMTU');
        } catch (e) {
          this.wizardUplinkStepSet(s4, 'warn', e.message + ' — continuing');
        }
      }

      // Step 6: Gateway
      const s5 = this.wizardUplinkStepAdd('Creating gateway');
      this.wizardUplinkStepSet(s5, 'running');
      let gatewayId = '';
      try {
        const res = await this.api.createGateway({
          name: w.gatewayName || ('gw-' + base),
          interface: ifaceId,
          gatewayIP: w.gatewayMonitorIP,
          monitorAddress: w.gatewayMonitorIP,
          monitor: true,
          monitorInterval: 5,
          latencyThreshold: 500,
          monitorRule: 'icmp_only',
        });
        gatewayId = (res.gateway || res).id || '';
        w.createdGatewayId = gatewayId;
        this.wizardUplinkStepSet(s5, 'ok', w.gatewayName);
      } catch (e) {
        this.wizardUplinkStepSet(s5, 'warn', e.message + ' — continuing');
      }

      // Step 7: Firewall PBR rule
      const s6 = this.wizardUplinkStepAdd('Creating firewall PBR rule');
      this.wizardUplinkStepSet(s6, 'running');
      try {
        const srcPart = w.createdSrcAliasId
          ? { type: 'alias', aliasId: w.createdSrcAliasId }
          : { type: 'any' };
        const dstPart = w.dstType === 'all'
          ? { type: 'any' }
          : w.createdDstAliasId
            ? { type: 'alias', aliasId: w.createdDstAliasId, invert: w.dstNegate }
            : { type: 'any' };
        const res = await this.api.createFirewallRule({
          name: w.fwRuleName || ('pbr-' + base),
          interface: 'any',
          protocol: 'any',
          source: srcPart,
          destination: dstPart,
          action: 'accept',
          gatewayId: gatewayId || undefined,
          fallbackToDefault: w.fallback === 'allow',
        });
        w.createdFwRuleId = (res.rule || res).id || '';
        await this.api.applyFirewallRules();
        this.wizardUplinkStepSet(s6, 'ok', w.fwRuleName);
      } catch (e) {
        this.wizardUplinkStepSet(s6, 'warn', e.message + ' — continuing');
      }

      // Step 8: NAT MASQUERADE
      const s7 = this.wizardUplinkStepAdd('Creating NAT MASQUERADE rule');
      this.wizardUplinkStepSet(s7, 'running');
      try {
        const natBody = {
          name: w.natRuleName || ('nat-' + base),
          outInterface: ifaceId,
          type: 'MASQUERADE',
        };
        if (w.createdSrcAliasId) natBody.sourceAliasId = w.createdSrcAliasId;
        const res = await this.api.createNatRule(natBody);
        w.createdNatRuleId = (res.rule || res).id || '';
        this.wizardUplinkStepSet(s7, 'ok', w.natRuleName);
      } catch (e) {
        this.wizardUplinkStepSet(s7, 'warn', e.message);
      }

      w.applying = false;
      w.done = true;
    },

    // ══════════════════════════════════════════════════════════════════════════
    // Wizard: Cascade ↔ Cascade S2S
    // ══════════════════════════════════════════════════════════════════════════

    async wizardS2SInit() {
      const w = this.wizardS2S;
      w.step = 1;
      w.remoteId = ''; w.showAddRemote = false;
      w.addRemoteName = ''; w.addRemoteURL = ''; w.addRemoteMode = 'password';
      w.addRemoteUser = ''; w.addRemotePass = ''; w.addRemoteToken = '';
      w.addRemoteSkipTLS = false; w.addRemoteLoading = false;
      w.selectedIfaceIds = []; w.createSrcAlias = true; w.srcAliasName = '';
      w.dstType = 'all'; w.dstCountries = []; w.dstASN = ''; w.dstNegate = false; w.dstAliasName = '';
      w.protocol = 'wireguard'; w.mssClamp = true; w.fallback = 'drop';
      w.localIfaceName = ''; w.remoteIfaceName = ''; w.gatewayName = ''; w.fwRuleName = '';
      w.applying = false; w.steps = [];
      w.createdLocalIfaceId = ''; w.createdRemoteIfaceId = '';
      w.createdSrcAliasId = ''; w.createdDstAliasId = '';
      w.createdGatewayId = ''; w.createdFwRuleId = '';
      w.done = false; w.fatalError = '';
      try {
        const res = await this.api.call({ method: 'get', path: '/remotes' });
        w.remotes = res.remotes || res || [];
      } catch (e) {
        w.remotes = [];
      }
    },

    wizardS2SOnNameChange() {
      const w = this.wizardS2S;
      const base = (w.localIfaceName || 's2s').trim();
      if (!w.remoteIfaceName || w.remoteIfaceName === 'remote-' + base) w.remoteIfaceName = 'remote-' + base;
      if (!w.srcAliasName || w.srcAliasName === 'src-' + base) w.srcAliasName = 'src-' + base;
      if (!w.dstAliasName || w.dstAliasName === 'dst-' + base) w.dstAliasName = 'dst-' + base;
      if (!w.gatewayName || w.gatewayName === 'gw-' + base) w.gatewayName = 'gw-' + base;
      if (!w.fwRuleName || w.fwRuleName === 'pbr-' + base) w.fwRuleName = 'pbr-' + base;
    },

    wizardS2SAutoNames() {
      const w = this.wizardS2S;
      const remote = w.remotes.find(r => r.id === w.remoteId);
      const base = (remote ? remote.name.toLowerCase().replace(/[^a-z0-9]/g, '-') : 'cascade') + '-s2s';
      if (!w.localIfaceName) w.localIfaceName = base;
      if (!w.remoteIfaceName) w.remoteIfaceName = 'remote-' + base;
      if (!w.srcAliasName) w.srcAliasName = 'src-' + base;
      if (!w.dstAliasName) w.dstAliasName = 'dst-' + base;
      if (!w.gatewayName) w.gatewayName = 'gw-' + base;
      if (!w.fwRuleName) w.fwRuleName = 'pbr-' + base;
    },

    async wizardS2SAddRemote() {
      const w = this.wizardS2S;
      w.addRemoteLoading = true;
      try {
        const body = {
          name: w.addRemoteName.trim(),
          url: w.addRemoteURL.trim(),
          skipTLS: w.addRemoteSkipTLS,
        };
        if (w.addRemoteMode === 'password') {
          body.username = w.addRemoteUser.trim();
          body.password = w.addRemotePass;
        } else {
          body.token = w.addRemoteToken.trim();
        }
        const res = await this.api.call({ method: 'post', path: '/remotes', body });
        const added = res.remote || res;
        w.remotes.push(added);
        w.remoteId = added.id;
        w.showAddRemote = false;
        w.addRemoteName = ''; w.addRemoteURL = ''; w.addRemoteUser = ''; w.addRemotePass = ''; w.addRemoteToken = '';
      } catch (e) {
        this.showToast(e.message || 'Failed to add remote', 'error');
      } finally {
        w.addRemoteLoading = false;
      }
    },

    wizardS2SClientIfaces() {
      return (this.tunnelInterfaces || []).filter(i => !i.disableRoutes);
    },

    wizardS2SIfaceSubnet(iface) {
      if (!iface || !iface.address) return '';
      const parts = iface.address.split('/');
      if (parts.length < 2) return iface.address;
      const ipParts = parts[0].split('.');
      ipParts[3] = '0';
      return ipParts.join('.') + '/' + parts[1];
    },

    wizardS2SToggleIface(id) {
      const idx = this.wizardS2S.selectedIfaceIds.indexOf(id);
      if (idx === -1) this.wizardS2S.selectedIfaceIds.push(id);
      else this.wizardS2S.selectedIfaceIds.splice(idx, 1);
    },

    wizardS2SSelectedSubnets() {
      return this.wizardS2S.selectedIfaceIds.map(id => {
        const iface = (this.tunnelInterfaces || []).find(i => i.id === id);
        return iface ? this.wizardS2SIfaceSubnet(iface) : '';
      }).filter(Boolean);
    },

    wizardS2SFreeSubnet(remoteAddresses) {
      const localUsed  = (this.tunnelInterfaces || []).map(i => i.address).filter(Boolean);
      const remoteUsed = remoteAddresses || [];
      const used = [...localUsed, ...remoteUsed];
      for (let base = 0; base < 256; base += 4) {
        const localIP  = `10.255.255.${base + 1}`;
        const remoteIP = `10.255.255.${base + 2}`;
        const inUse = used.some(addr =>
          addr.startsWith(localIP + '/') || addr.startsWith(localIP + ' ') || addr === localIP ||
          addr.startsWith(remoteIP + '/') || addr.startsWith(remoteIP + ' ') || addr === remoteIP
        );
        if (!inUse) return { localAddr: localIP + '/30', remoteAddr: remoteIP + '/30', localIP, remoteIP };
      }
      return null;
    },

    wizardS2SStepAdd(label) {
      this.wizardS2S.steps.push({ label, status: 'pending', detail: '' });
      return this.wizardS2S.steps.length - 1;
    },

    wizardS2SStepSet(idx, status, detail) {
      this.$set(this.wizardS2S.steps, idx, {
        ...this.wizardS2S.steps[idx],
        status,
        detail: detail || '',
      });
    },

    async wizardS2SApply() {
      const w = this.wizardS2S;
      w.applying = true; w.steps = []; w.done = false; w.fatalError = '';
      const rid = w.remoteId;

      // Step 0: Pre-flight — verify source subnets are not already routed/NATed on remote.
      // Prevents partial execution when two servers share the same client subnets
      // (e.g. two Cascade nodes routing to the same exit via the same remote).
      if (w.selectedIfaceIds.length > 0) {
        const sp = this.wizardS2SStepAdd('Checking source subnet availability on remote');
        this.wizardS2SStepSet(sp, 'running');

        const localSubnets = this.wizardS2SSelectedSubnets();
        this.wizardS2SStepSet(sp, 'running', `Local subnets: ${localSubnets.join(', ')}`);

        // Fetch NAT rules and static routes from remote independently so we can report
        // which fetch failed; do NOT silently swallow errors as "no subnets".
        let natRules = null, remoteRoutes = null, fetchErr = '';
        try { natRules = (await this.api.remoteCall({ remoteId: rid, method: 'get', path: '/nat/rules' })).rules || []; }
        catch (e) { fetchErr += `NAT rules: ${e.message}; `; }
        try { remoteRoutes = (await this.api.remoteCall({ remoteId: rid, method: 'get', path: '/routing/routes' })).routes || []; }
        catch (e) { fetchErr += `Routes: ${e.message}; `; }

        if (natRules === null && remoteRoutes === null) {
          // Both fetches failed — cannot verify, block the wizard.
          const msg = `Cannot reach remote to verify subnet availability: ${fetchErr.trimEnd()}`;
          this.wizardS2SStepSet(sp, 'error', msg);
          w.fatalError = msg; w.applying = false; return;
        }

        // Collect all subnets already known on remote: NAT sources + static route destinations
        const remoteSubnets = [];
        for (const rule of (natRules || [])) {
          if (rule.source) remoteSubnets.push(rule.source);
        }
        for (const route of (remoteRoutes || [])) {
          if (route.destination) remoteSubnets.push(route.destination);
        }

        if (fetchErr) {
          // Partial fetch — note it but continue with what we have
          this.wizardS2SStepSet(sp, 'running', `Partial data (${fetchErr.trimEnd()}) — ${remoteSubnets.length} subnets found`);
        }

        const conflicts = localSubnets.filter(s => remoteSubnets.includes(s));
        if (conflicts.length > 0) {
          const msg = `Source subnet(s) already present on remote: ${conflicts.join(', ')}. Another server may already route these subnets through this exit.`;
          this.wizardS2SStepSet(sp, 'error', msg);
          w.fatalError = msg; w.applying = false; return;
        }

        this.wizardS2SStepSet(sp, 'ok', `No conflicts — remote has ${remoteSubnets.length} existing subnet entries${fetchErr ? ' (partial data)' : ''}`);
      }

      // Step 1: Find free /30 — check both local and remote interfaces
      const s0 = this.wizardS2SStepAdd('Allocating S2S subnet');
      this.wizardS2SStepSet(s0, 'running');
      let remoteAddresses = [];
      try {
        const remoteIfaces = await this.api.remoteCall({ remoteId: rid, method: 'get', path: '/tunnel-interfaces' });
        remoteAddresses = ((remoteIfaces.interfaces || remoteIfaces || []).map(i => i.address)).filter(Boolean);
        this.wizardS2SStepSet(s0, 'running', `Local: ${(this.tunnelInterfaces||[]).length} ifaces, remote: ${remoteAddresses.length} ifaces`);
      } catch (e) {
        this.wizardS2SStepSet(s0, 'running', 'Could not fetch remote interfaces — checking local only');
      }
      const subnet = this.wizardS2SFreeSubnet(remoteAddresses);
      if (!subnet) {
        this.wizardS2SStepSet(s0, 'error', '10.255.255.0/24 exhausted on local or remote');
        w.fatalError = 'No free /30 in 10.255.255.0/24'; w.applying = false; return;
      }
      this.wizardS2SStepSet(s0, 'ok', `${subnet.localAddr} ↔ ${subnet.remoteAddr}`);

      // Step 2: Create local S2S interface
      const s1 = this.wizardS2SStepAdd('Creating local S2S interface');
      this.wizardS2SStepSet(s1, 'running');
      let localIfaceId = '';
      let localSettings = null;
      try {
        const body = { name: w.localIfaceName, address: subnet.localAddr, disableRoutes: true, protocol: w.protocol };
        const res = await this.api.createTunnelInterface(body);
        const iface = res.interface || res;
        localIfaceId = iface.id || '';
        localSettings = iface.settings || null;
        w.createdLocalIfaceId = localIfaceId;
        await this.api.startTunnelInterface({ interfaceId: localIfaceId }).catch(() => {});
        await this.loadTunnelInterfaces();
        this.wizardS2SStepSet(s1, 'ok', w.localIfaceName);
      } catch (e) {
        this.wizardS2SStepSet(s1, 'error', e.message);
        w.fatalError = e.message; w.applying = false; return;
      }

      // Step 3: Create remote S2S interface
      const s2 = this.wizardS2SStepAdd('Creating remote S2S interface');
      this.wizardS2SStepSet(s2, 'running');
      let remoteIfaceId = '';
      try {
        const body = { name: w.remoteIfaceName, address: subnet.remoteAddr, disableRoutes: true, protocol: w.protocol };
        // Pass AWG2 settings from local interface so remote doesn't need to generate them independently.
        if (w.protocol === 'amneziawg-2.0' && localSettings) body.settings = localSettings;
        const res = await this.api.remoteCall({ remoteId: rid, method: 'post', path: '/tunnel-interfaces', body });
        const remoteIface = res.interface || res;
        remoteIfaceId = remoteIface.id || '';
        w.createdRemoteIfaceId = remoteIfaceId;
        await this.api.remoteCall({ remoteId: rid, method: 'post', path: `/tunnel-interfaces/${remoteIfaceId}/start`, body: {} }).catch(() => {});
        this.wizardS2SStepSet(s2, 'ok', w.remoteIfaceName);
      } catch (e) {
        this.wizardS2SStepSet(s2, 'error', e.message);
        w.fatalError = e.message; w.applying = false; return;
      }

      // Step 4: Exchange WireGuard params
      // Order matters for PSK sync:
      //   1. Export local (no PSK yet)  → import into remote  → remote generates PSK
      //   2. Export remote (PSK present) → import into local   → local receives same PSK
      // Reversing the export order causes both sides to generate independent PSKs → handshake failure.
      const s3 = this.wizardS2SStepAdd('Exchanging WireGuard keys');
      this.wizardS2SStepSet(s3, 'running');
      try {
        // export-params for disableRoutes=true already returns allowedIPs=0.0.0.0/0 — keep as-is.
        const localParams = await this.api.call({ method: 'get', path: `/tunnel-interfaces/${localIfaceId}/export-params` });
        await this.api.remoteCall({ remoteId: rid, method: 'post', path: `/tunnel-interfaces/${remoteIfaceId}/peers/import-json`, body: localParams });
        // Remote now has an interconnect peer with a generated PSK — re-export to get it.
        const remoteParams = await this.api.remoteCall({ remoteId: rid, method: 'get', path: `/tunnel-interfaces/${remoteIfaceId}/export-params` });
        await this.api.call({ method: 'post', path: `/tunnel-interfaces/${localIfaceId}/peers/import-json`, body: remoteParams });
        this.wizardS2SStepSet(s3, 'ok', 'Keys exchanged');
      } catch (e) {
        this.wizardS2SStepSet(s3, 'error', e.message);
        w.fatalError = e.message; w.applying = false; return;
      }

      // Step 6: Source alias
      if (w.createSrcAlias && w.selectedIfaceIds.length > 0) {
        const s4 = this.wizardS2SStepAdd('Creating source alias');
        this.wizardS2SStepSet(s4, 'running');
        try {
          const subnets = this.wizardS2SSelectedSubnets();
          const res = await this.api.createAlias({ name: w.srcAliasName, type: 'network', entries: subnets });
          w.createdSrcAliasId = (res.alias || res).id || '';
          this.wizardS2SStepSet(s4, 'ok', subnets.join(', '));
        } catch (e) {
          this.wizardS2SStepSet(s4, 'warn', e.message + ' — continuing');
        }
      }

      // Step 6: Destination alias (GEO/AS)
      if (w.dstType !== 'all') {
        const s5 = this.wizardS2SStepAdd('Creating destination alias');
        this.wizardS2SStepSet(s5, 'running');
        try {
          const res = await this.api.createAlias({ name: w.dstAliasName, type: 'ipset', entries: [] });
          const aliasId = (res.alias || res).id || '';
          w.createdDstAliasId = aliasId;
          const genBody = w.dstType === 'geo'
            ? { country: w.dstCountries.join(','), asn: '', asnList: '' }
            : { country: '', asn: w.dstASN.trim(), asnList: '' };
          const genRes = await this.api.generateAlias({ id: aliasId, ...genBody });
          const jobId = genRes.jobId;
          let finished = false;
          while (!finished) {
            await new Promise(r => setTimeout(r, 1000));
            const st = await this.api.getAliasJobStatus({ id: aliasId, jobId });
            this.wizardS2SStepSet(s5, 'running', (st.progress || 0) + '% loaded…');
            if (st.status === 'done') { finished = true; }
            if (st.status === 'error') throw new Error(st.error || 'Generation failed');
          }
          this.wizardS2SStepSet(s5, 'ok', w.dstAliasName + ' created');
        } catch (e) {
          this.wizardS2SStepSet(s5, 'warn', e.message + ' — continuing');
        }
      }

      // Step 7: MSS clamping on local S2S interface
      if (w.mssClamp && localIfaceId) {
        const s6 = this.wizardS2SStepAdd('Enabling MSS clamping');
        this.wizardS2SStepSet(s6, 'running');
        try {
          await this.api.updateTunnelInterface({ interfaceId: localIfaceId, mss: -1 });
          this.wizardS2SStepSet(s6, 'ok', 'Auto PMTU');
        } catch (e) {
          this.wizardS2SStepSet(s6, 'warn', e.message + ' — continuing');
        }
      }

      // Step 8: Local gateway
      const s7 = this.wizardS2SStepAdd('Creating gateway');
      this.wizardS2SStepSet(s7, 'running');
      let gatewayId = '';
      try {
        const res = await this.api.createGateway({
          name: w.gatewayName,
          interface: localIfaceId,
          gatewayIP: subnet.remoteIP,
          monitorAddress: subnet.remoteIP,
          monitor: true,
          monitorInterval: 5,
          latencyThreshold: 500,
          monitorRule: 'icmp_only',
        });
        gatewayId = (res.gateway || res).id || '';
        w.createdGatewayId = gatewayId;
        this.wizardS2SStepSet(s7, 'ok', w.gatewayName);
      } catch (e) {
        this.wizardS2SStepSet(s7, 'warn', e.message + ' — continuing');
      }

      // Step 9: Local PBR firewall rule
      const s8 = this.wizardS2SStepAdd('Creating firewall PBR rule');
      this.wizardS2SStepSet(s8, 'running');
      try {
        const srcPart = w.createdSrcAliasId ? { type: 'alias', aliasId: w.createdSrcAliasId } : { type: 'any' };
        const dstPart = w.dstType === 'all' ? { type: 'any' }
          : w.createdDstAliasId ? { type: 'alias', aliasId: w.createdDstAliasId, invert: w.dstNegate } : { type: 'any' };
        const res = await this.api.createFirewallRule({
          name: w.fwRuleName,
          interface: 'any',
          protocol: 'any',
          source: srcPart,
          destination: dstPart,
          action: 'accept',
          gatewayId: gatewayId || undefined,
          fallbackToDefault: w.fallback === 'allow',
        });
        w.createdFwRuleId = (res.rule || res).id || '';
        await this.api.applyFirewallRules();
        this.wizardS2SStepSet(s8, 'ok', w.fwRuleName);
      } catch (e) {
        this.wizardS2SStepSet(s8, 'warn', e.message + ' — continuing');
      }

      // Step 10: Remote return route (client subnets → local S2S IP)
      const s9 = this.wizardS2SStepAdd('Adding return routes on remote');
      this.wizardS2SStepSet(s9, 'running');
      try {
        const subnets = this.wizardS2SSelectedSubnets();
        if (subnets.length > 0) {
          await Promise.all(subnets.map(cidr =>
            this.api.remoteCall({ remoteId: rid, method: 'post', path: '/routing/routes', body: {
              destination: cidr,
              gateway: subnet.localIP,
              dev: remoteIfaceId,
              enabled: true,
            }})
          ));
          this.wizardS2SStepSet(s9, 'ok', subnets.join(', '));
        } else {
          this.wizardS2SStepSet(s9, 'warn', 'No subnets selected — skipped');
        }
      } catch (e) {
        this.wizardS2SStepSet(s9, 'warn', e.message + ' — continuing');
      }

      // Step 11: Remote NAT MASQUERADE on system interface
      const s10 = this.wizardS2SStepAdd('Adding NAT on remote');
      this.wizardS2SStepSet(s10, 'running');
      try {
        const ifacesRes = await this.api.remoteCall({ remoteId: rid, method: 'get', path: '/system/interfaces' });
        const ifaces = ifacesRes.interfaces || [];
        const sysIface = ifaces.find(i => !i.name.startsWith('wg') && !i.name.startsWith('awg') && i.name !== 'lo');
        if (!sysIface) throw new Error('No system interface found on remote');
        const natBody = { name: 'nat-' + w.remoteIfaceName, outInterface: sysIface.name, type: 'MASQUERADE' };
        if (w.createdSrcAliasId) {
          // Create matching alias on remote too
          const srcSubnets = this.wizardS2SSelectedSubnets();
          const remoteAlias = await this.api.remoteCall({ remoteId: rid, method: 'post', path: '/aliases', body: {
            name: w.srcAliasName + '-remote', type: 'network', entries: srcSubnets,
          }});
          const remoteAliasId = (remoteAlias.alias || remoteAlias).id || '';
          if (remoteAliasId) natBody.sourceAliasId = remoteAliasId;
        }
        await this.api.remoteCall({ remoteId: rid, method: 'post', path: '/nat/rules', body: natBody });
        this.wizardS2SStepSet(s10, 'ok', sysIface.name);
      } catch (e) {
        this.wizardS2SStepSet(s10, 'warn', e.message + ' — continuing');
      }

      w.applying = false;
      w.done = true;
    },

  },
  filters: {
    bytes,
    timeago: (value) => {
      return timeago.format(value, i18n.locale);
    },
    expiredDateFormat: (value) => {
      if (value === null) return i18n.t('Permanent');
      const dateTime = new Date(value);
      const options = { year: 'numeric', month: 'long', day: 'numeric' };
      return dateTime.toLocaleDateString(i18n.locale, options);
    },
    expiredDateEditFormat: (value) => {
      if (value === null) return 'yyyy-MM-dd';
    },
  },
  mounted() {
    // Remove splash screen — Vue is mounted, content is ready
    const splash = document.getElementById('app-splash');
    if (splash) {
      // Hide immediately — Vue is mounted, content is ready.
      splash.classList.add('splash-hide');
      setTimeout(() => { if (splash.parentNode) splash.parentNode.removeChild(splash); }, 400);
    }

    this.prefersDarkScheme.addListener(this.handlePrefersChange);
    this.setTheme(this.uiTheme);

    this.api = new API();

    // Auto-switch back to local when the remote becomes unreachable.
    // This fires when a proxy call returns 401 (token revoked/expired) or
    // 5xx (remote server or network error), preventing a confusing login
    // window after a remote goes down.
    this.api._onRemoteError = (status, path) => {
      if (!this.activeRemoteId) return; // already local, ignore
      const remoteName = (this.activeRemote && this.activeRemote.name) || 'Remote server';
      this.switchToLocal();
      const reason = status === 401 ? 'authentication failed' : `error ${status}`;
      this.showToast(`${remoteName} disconnected (${reason}). Switched back to local.`, 'error');
    };

    this.api.getSession()
      .then((session) => {
        this.authenticated = session.authenticated;
        this.requiresPassword = session.requiresPassword;
        // First run: no users → show setup modal (non-dismissible)
        if (!session.requiresPassword) this.showFirstRunSetup = true;
        this.refresh({
          updateCharts: this.updateCharts,
        }).catch((err) => {
          this.showToast(err.message || err.toString(), 'error');
        });
        // Load tunnel interfaces at startup (default page); then populate dashboard immediately
        this.loadTunnelInterfaces().then(() => {
          if (!this.activeInterfaceId) this.refreshAllPeers();
        }).catch(console.error);
        // Load settings + templates at startup so they are available
        // on any page (e.g. "Obfuscation Profile" dropdown in Create Interface modal).
        this.loadSettings();
        // Load client groups at startup — needed for peer create/edit dropdowns.
        this.loadClientGroups();
        // Load users and current user for the Users section in Settings.
        this.loadUsers();
        this.loadCurrentUser();
        // Load registered remote servers.
        this.loadRemotes();
        // Load dashboard layout + start metrics poller.
        this.loadDashboard();
        this.metricsStartPoller();
      })
      .catch((err) => {
        this.showToast(err.message || err.toString(), 'error');
      });

    this.api.getRememberMeEnabled()
      .then((rememberMeEnabled) => {
        this.rememberMeEnabled = rememberMeEnabled;
      });

    // Version info — fetch immediately (unauthenticated endpoint) and refresh
    // every 24 h so the update badge appears without a page reload.
    this.loadVersionInfo();
    setInterval(() => this.loadVersionInfo(), 24 * 60 * 60 * 1000);

    setInterval(() => {
      this.refresh({
        updateCharts: this.updateCharts,
      }).catch(console.error);
      if (this.activePage === 'interfaces') {
        if (this.activeInterfaceId) {
          this.refreshPeers({
            updateCharts: this.updateCharts,
          }).catch(console.error);
        } else {
          // Dashboard mode: refresh all interfaces' peers
          this.refreshAllPeers({
            updateCharts: this.updateCharts,
          }).catch(console.error);
        }
      }
      if (this.activePage === 'gateways') {
        this.refreshGateways().catch(console.error);
      }
      if (this.activePage === 'dashboard') {
        this.refreshGateways().catch(console.error);
        this.refreshAllPeers().catch(console.error);
      }
    }, 1000);

    // System info polling every 30s (dashboard only)
    setInterval(() => {
      if (this.activePage === 'dashboard' && this.dashWidgets.some(w => w.type === 'server-info')) {
        this.loadSystemInfo();
      }
    }, 30000);

    this.api.getuiTrafficStats()
      .then((res) => {
        this.uiTrafficStats = res;
      })
      .catch(() => {
        this.uiTrafficStats = false;
      });

    // uiChartType is now loaded via loadSettings() from globalSettings.chartType

    this.api.getWGEnableOneTimeLinks()
      .then((res) => {
        this.enableOneTimeLinks = res;
      })
      .catch(() => {
        this.enableOneTimeLinks = false;
      });

    this.api.getUiSortClients()
      .then((res) => {
        this.enableSortClient = res;
      })
      .catch(() => {
        this.enableSortClient = false;
      });

    this.api.getWGEnableExpireTime()
      .then((res) => {
        this.enableExpireTime = res;
      })
      .catch(() => {
        this.enableExpireTime = false;
      });

    this.api.getAvatarSettings()
      .then((res) => {
        this.avatarSettings = res;
      })
      .catch(() => {
          this.avatarSettings = {
            'dicebear': null,
            'gravatar': false,
          };
      });

    Promise.resolve().then(async () => {
      const lang = await this.api.getLang();
      if (lang !== localStorage.getItem('lang') && i18n.availableLocales.includes(lang)) {
        localStorage.setItem('lang', lang);
        i18n.locale = lang;
      }

      const currentRelease = await this.api.getRelease();
      const latestRelease = await fetch('https://wg-easy.github.io/wg-easy/changelog.json')
        .then((res) => res.json())
        .then((releases) => {
          const releasesArray = Object.entries(releases).map(([version, changelog]) => ({
            version: parseInt(version, 10),
            changelog,
          }));
          releasesArray.sort((a, b) => {
            return b.version - a.version;
          });

          return releasesArray[0];
        });

      if (currentRelease >= latestRelease.version) return;

      this.currentRelease = currentRelease;
      this.latestRelease = latestRelease;
    }).catch((err) => console.error(err));
  },
  watch: {
    // Update browser tab title whenever router name or hostname changes.
    pageTitle: {
      immediate: true,
      handler(val) {
        document.title = val;
      },
    },
    showRemoteAdd(val) {
      if (!val) {
        this.remoteAddNeedsTOTP = false;
        this.remoteAddError = '';
        this.remoteAddForm = { name: '', url: '', mode: 'login', username: '', password: '', totpCode: '', token: '' };
      }
    },
    activeInterfaceId(newId) {
      if (newId) {
        this.selectedInterface = this.currentInterface;
        this.refreshPeers({ updateCharts: false });
      } else {
        this.selectedInterface = null;
        this.selectedInterfacePeers = [];
        // Switch to dashboard — immediately load all peers
        this.refreshAllPeers({ updateCharts: false });
      }
    },
  },
  computed: {
    // Browser tab title: routerName if set, otherwise hostname, otherwise 'Cascade'.
    pageTitle() {
      return this.globalSettings.routerName || this.globalSettings.hostname || 'Cascade';
    },

    // The currently active remote record, or null if we're on the local server.
    activeRemote() {
      if (!this.activeRemoteId) return null;
      return this.remotes.find(r => r.id === this.activeRemoteId) || null;
    },

    // Label shown in the server switcher dropdown.
    activeServerLabel() {
      return this.activeRemote ? this.activeRemote.name : (this.localServerName || this.pageTitle || 'Local');
    },
    // MSS clamping mode derived from the sentinel int value in interfaceEdit.mss.
    // Used to drive the select dropdown in Edit Interface modal.
    mssMode() {
      if (this.interfaceEdit.mss === -1) return 'auto';
      if (this.interfaceEdit.mss > 0)   return 'manual';
      return 'disabled';
    },
    filteredCountries() {
      const q = (this.countrySearch || '').trim().toLowerCase();
      if (!q) return this.countries;
      return this.countries.filter(c =>
        c.name.toLowerCase().startsWith(q) || c.code.toLowerCase().startsWith(q)
      );
    },
    currentInterface() {
      if (!this.activeInterfaceId) return null;
      return this.tunnelInterfaces.find(i => i.id === this.activeInterfaceId) || null;
    },
    chartOptionsTX() {
      const opts = {
        ...this.chartOptions,
        colors: [CHART_COLORS.tx[this.theme]],
      };
      opts.chart.type = UI_CHART_TYPES[this.uiChartType].type || false;
      opts.stroke.width = UI_CHART_TYPES[this.uiChartType].strokeWidth;
      return opts;
    },
    chartOptionsRX() {
      const opts = {
        ...this.chartOptions,
        colors: [CHART_COLORS.rx[this.theme]],
      };
      opts.chart.type = UI_CHART_TYPES[this.uiChartType].type || false;
      opts.stroke.width = UI_CHART_TYPES[this.uiChartType].strokeWidth;
      return opts;
    },
    updateCharts() {
      return this.uiChartType > 0 && this.uiShowCharts;
    },
    theme() {
      if (this.uiTheme === 'auto') {
        return this.prefersDarkScheme.matches ? 'dark' : 'light';
      }
      return this.uiTheme;
    },
  },
});
