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
      { id: 'settings',         label: 'Settings' },
      { id: 'administration',   label: 'Administration' },
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
    ],
    dashPeersState: {},   // per-widget: { [widgetId]: { iface: '', sort: 'name' } }

    // Tunnel Interfaces
    tunnelInterfaces: [],
    selectedInterface: null,
    selectedInterfacePeers: [],
    allPeers: [],            // dashboard: flat list of peers from all interfaces
    showInterfaceCreate: false,
    createMode: 'quick',        // 'quick' | 'manual' — controls which form is shown in the create modal
    showImportConf: false,
    importConfForm: { name: '', conf: '', fileName: '' },
    importConfWarning: '',
    showImportBackup: false,
    importBackupForm: { json: '', listenPort: '', fileName: '' },
    showInterfaceEdit: false,
    interfaceEdit: {
      id: null,
      name: '',
      address: '',
      listenPort: '',
      disableRoutes: false,
      natDisabled: false,
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
    backupDownloading: false,
    showRestorePasswordModal: false, // password prompt for encrypted restore
    restorePassword: '',
    restoreFile: null,          // File object pending restore after password entry

    // Firewall Rules (поглощает PBR)
    firewallRules: [],
    firewallRulesLoading: false,
    firewallInterfaces: [],
    showFirewallCreate: false,
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
      if (this.activePage === 'dashboard' && pageId !== 'dashboard' && this.dashGrid) {
        // destroy(false) — remove GridStack listeners/styles but keep DOM nodes
        // so that Vue's v-if can cleanly remove the subtree without conflict.
        // destroy(true) removed GridStack-owned nodes before v-if ran, desynchronising
        // Vue's vdom from the real DOM (visible as blank dashboard on return).
        this.dashGrid.destroy(false);
        this.dashGrid = null;
      }
      this.activePage = pageId;
      if (pageId === 'dashboard') {
        // loadDashboard already calls dashInitGrid after await $nextTick —
        // do not call it separately here to avoid a double-init race.
        this.loadDashboard();
        this.loadSystemInfo();
      }
      if (pageId === 'interfaces') this.loadTunnelInterfaces();
      if (pageId === 'settings') { this.loadSettings(); this.loadUsers(); this.loadApiTokens(); }
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
        const res = await fetch('./api/tunnel-interfaces', { credentials: 'include' });
        if (!res.ok) throw new Error(res.statusText);
        const data = await res.json();
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
        };

        if (this.interfaceCreate.protocol === 'amneziawg-2.0') {
          payload.settings = this.interfaceCreate.settings;
        }

        const res = await fetch('./api/tunnel-interfaces', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          credentials: 'include',
          body: JSON.stringify(payload),
        });

        if (!res.ok) {
          const error = await res.json();
          throw new Error(error.message || res.statusText);
        }

        const newIface = await res.json();
        this.showInterfaceCreate = false;
        this._resetInterfaceCreate();

        await this.loadTunnelInterfaces();
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

        const res = await fetch('./api/tunnel-interfaces/quick-create', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          credentials: 'include',
          body: JSON.stringify(body),
        });

        if (!res.ok) {
          const error = await res.json().catch(() => ({}));
          throw new Error(error.message || res.statusText);
        }

        const data = await res.json();
        this.showInterfaceCreate = false;
        this._resetInterfaceCreate();
        await this.loadTunnelInterfaces();

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
        const res = await this.api.importTunnelConf({ name, conf });
        this.showImportConf = false;
        this.importConfForm = { name: '', conf: '', fileName: '' };
        await this.loadTunnelInterfaces();

        const iface = res.interface || {};
        const proto = iface.protocol === 'amneziawg-2.0' ? ' · AWG2' : ' · WG1';
        if (res.conflictWarning) {
          this.showToast(`⚠️ ${res.conflictWarning}`, 'error');
        }
        if (res.started) {
          this.showToast(`✅ ${iface.id} imported & started · ${iface.address}${proto}`);
          this.activeInterfaceId = iface.id;
        } else {
          this.showToast(
            `⚠️ ${iface.id} imported but failed to start\n${res.startError || 'Unknown error'}`,
            'error'
          );
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
        this.importBackupForm.json = e.target.result || '';
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

    // generateAndFillInterfaceParams — fills AWG2 fields in the Manual form
    // by generating a random profile via the /templates/generate endpoint.
    async generateAndFillInterfaceParams() {
      try {
        const res = await fetch('./api/templates/generate', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          credentials: 'include',
          body: JSON.stringify({ profile: 'random', intensity: 'medium' }),
        });
        if (!res.ok) {
          const error = await res.json().catch(() => ({}));
          throw new Error(error.message || res.statusText);
        }
        const { params } = await res.json();
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
        disableRoutes: false, selectedTemplateId: '',
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

    async loadVersionInfo() {
      try {
        const res = await fetch('./api/version');
        if (res.ok) {
          this.versionInfo = await res.json();
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
      const { id, name, address, listenPort, disableRoutes, natDisabled, publicHost, mtu, mss, protocol, settings } = this.interfaceEdit;

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

      const { mode, peerType, name, publicKey, endpoint, allowedIPs, clientAllowedIPs, persistentKeepalive, groupId } = this.peerCreate;

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
        this.peerCreate = { mode: 'generate', peerType: 'client', name: '', publicKey: '', endpoint: '', allowedIPs: '', clientAllowedIPs: '', persistentKeepalive: 25, groupId: this.defaultGroupId(), showQR: false };

        await this.refreshPeers();
        await this.loadTunnelInterfaces();
        if (peerType === 'client') {
          this.loadClientGroups();
          this.loadAliases();
        }

        if (showQR && mode === 'generate' && peerType === 'client' && peerId) {
          this.qrcode = `./api/tunnel-interfaces/${interfaceId}/peers/${peerId}/qrcode.svg`;
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
        const segs = window.location.pathname.split('/').filter(Boolean);
        const apiBase = segs.length > 0
          ? `${window.location.origin}/${segs[0]}/api`
          : `${window.location.origin}/api`;
        const res = await fetch(`${apiBase}/tunnel-interfaces/${this._peerIfaceId(peer)}/peers/${peer.id}/config`, {
          credentials: 'include',
        });
        if (!res.ok) throw new Error(res.statusText);
        const config = await res.text();
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
        resizable: { handles: 'se' },
      }, el);

      grid.on('change', () => {
        if (this._dashSaveEnabled) this.dashSaveLayout(grid);
      });
      this.dashGrid = grid;

      // Enable saving after GridStack's init-time change events have all fired
      setTimeout(() => { this._dashSaveEnabled = true; }, 300);

      if (this.dashWidgets.some(w => w.type === 'server-info')) {
        this.loadSystemInfo();
      }
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
        return {
          id: item.id,
          type: existing ? existing.type : item.id.replace('w-', ''),
          x: item.x, y: item.y, w: item.w, h: item.h,
        };
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
        if (el) this.dashGrid.makeWidget(el);
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
      if (iface.status === 'up') {
        await this.stopTunnelInterface(iface);
      } else {
        await this.startTunnelInterface(iface);
      }
    },

    // Open "Add peer" quick-create modal for a specific interface from dashboard
    dashOpenAddPeer(iface) {
      this.selectedInterface = iface;
      this.activeInterfaceId = iface.id;
      this.peerCreate = {
        mode: 'generate',
        peerType: 'client',
        name: '',
        publicKey: '',
        endpoint: '',
        allowedIPs: '',
        clientAllowedIPs: '',
        persistentKeepalive: 25,
        groupId: this.defaultGroupId(),
        showQR: false,
      };
      this.showPeerCreate = true;
    },

    // Human-readable protocol label for dashboard badges
    dashProtoLabel(protocol) {
      if (protocol === 'wireguard') return 'WG 1.0';
      if (protocol === 'amneziawg-2.0') return 'AWG 2.0';
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
        file: null,
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
        const { blob, filename } = await this.api.downloadSystemBackup({ password: this.backupPassword });
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
        // Ask for password before restoring.
        this.restoreFile = file;
        this.restorePassword = '';
        this.showRestorePasswordModal = true;
      } else {
        this._doRestore(file, '');
      }
    },

    async confirmRestoreWithPassword() {
      this.showRestorePasswordModal = false;
      await this._doRestore(this.restoreFile, this.restorePassword);
      this.restoreFile = null;
      this.restorePassword = '';
    },

    async _doRestore(file, password) {
      if (!confirm(`Restore backup from "${file.name}"?\n\nThis will replace ALL data and restart the server.`)) return;
      try {
        this.systemRestoring = true;
        await this.api.restoreSystemBackup({ file, password });
        this.showToast('Backup restored. Server is restarting…', 'success');
        setTimeout(() => window.location.reload(), 4000);
      } catch (err) {
        this.showToast(err.message || 'Restore failed', 'error');
      } finally {
        this.systemRestoring = false;
      }
    },

    // ========================================================================
    // Firewall Rules Methods  (поглощает PBR / Policy)
    // ========================================================================

    async loadFirewallRules() {
      this.firewallRulesLoading = true;
      try {
        const res = await this.api.getFirewallRules();
        this.firewallRules = Array.isArray(res) ? res : (res.rules || []);
      } catch (err) {
        console.error('loadFirewallRules error:', err);
        this.firewallRules = [];
      } finally {
        this.firewallRulesLoading = false;
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
          expiredDate: this.peerCreateExpiredDate || undefined,
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
          this.qrcode = `./api/tunnel-interfaces/${this.activeInterfaceId}/peers/${peerId}/qrcode.svg`;
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
        await this.api.generatePeerOneTimeLink({ interfaceId: this._peerIfaceId(peer), peerId: peer.id });
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
    this.prefersDarkScheme.addListener(this.handlePrefersChange);
    this.setTheme(this.uiTheme);

    this.api = new API();
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
        // Load dashboard layout.
        this.loadDashboard();
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
