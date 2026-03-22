const { v4: uuidv4 } = require('uuid');
const debug = require('debug')('awg:peer');

/**
 * Peer - удалённое подключение к интерфейсу
 * Представляет одну удалённую сторону в Site-to-Site VPN
 */
class Peer {
  /**
   * @param {Object} data - данные peer
   * @param {string} data.id - UUID peer (генерируется если не указан)
   * @param {string} data.name - friendly name (например "Office-A")
   * @param {string} data.interfaceId - к какому интерфейсу принадлежит (например "wg10")
   * @param {string} data.publicKey - публичный ключ удалённой стороны
   * @param {string} data.presharedKey - pre-shared key (опционально)
   * @param {string} data.endpoint - endpoint удалённой стороны (например "office-a.com:51820")
   * @param {string} data.allowedIPs - разрешённые IP/подсети (например "192.168.1.0/24")
   * @param {number} data.persistentKeepalive - keepalive в секундах (default 25)
   * @param {string} data.remoteAddress - IP адрес удалённой стороны в туннеле (например "10.100.0.2/24")
   * @param {boolean} data.enabled - включен ли peer
   * @param {string} data.createdAt - дата создания
   */
  constructor(data) {
    this.id = data.id || uuidv4();
    this.name = data.name;
    this.interfaceId = data.interfaceId;
    this.publicKey = data.publicKey;
    this.presharedKey = data.presharedKey || '';
    this.endpoint = data.endpoint || '';
    this.allowedIPs = data.allowedIPs;
    this.persistentKeepalive = data.persistentKeepalive || 25;
    this.remoteAddress = data.remoteAddress || '';
    this.enabled = data.enabled !== false; // default true
    this.createdAt = data.createdAt || new Date().toISOString();
    
    debug(`Peer created: ${this.id} (${this.name}) for interface ${this.interfaceId}`);
  }

  /**
   * Конвертировать в JSON для сохранения
   */
  toJSON() {
    return {
      id: this.id,
      name: this.name,
      interfaceId: this.interfaceId,
      publicKey: this.publicKey,
      presharedKey: this.presharedKey,
      endpoint: this.endpoint,
      allowedIPs: this.allowedIPs,
      persistentKeepalive: this.persistentKeepalive,
      remoteAddress: this.remoteAddress,
      enabled: this.enabled,
      createdAt: this.createdAt,
    };
  }

  /**
   * Валидация данных peer
   */
  validate() {
    const errors = [];
    
    if (!this.name || this.name.trim() === '') {
      errors.push('Peer name is required');
    }
    
    if (!this.interfaceId) {
      errors.push('Interface ID is required');
    }
    
    if (!this.publicKey || this.publicKey.length !== 44) {
      errors.push('Invalid public key (must be 44 characters base64)');
    }
    
    if (!this.allowedIPs) {
      errors.push('AllowedIPs is required');
    }
    
    // Валидация формата AllowedIPs (базовая)
    if (this.allowedIPs) {
      const ips = this.allowedIPs.split(',').map(ip => ip.trim());
      for (const ip of ips) {
        if (!this._isValidCIDR(ip)) {
          errors.push(`Invalid AllowedIPs format: ${ip}`);
        }
      }
    }
    
    // Валидация endpoint (опционально)
    if (this.endpoint && !this._isValidEndpoint(this.endpoint)) {
      errors.push('Invalid endpoint format (should be host:port)');
    }
    
    return errors;
  }

  /**
   * Проверка валидности CIDR нотации
   */
  _isValidCIDR(cidr) {
    // Упрощённая валидация (можно улучшить)
    const pattern = /^(\d{1,3}\.){3}\d{1,3}\/\d{1,2}$/;
    return pattern.test(cidr);
  }

  /**
   * Проверка валидности endpoint
   */
  _isValidEndpoint(endpoint) {
    // host:port
    const pattern = /^.+:\d+$/;
    return pattern.test(endpoint);
  }

  /**
   * Генерация секции [Peer] для WireGuard конфига
   */
  toWgConfig() {
    if (!this.enabled) {
      return ''; // Disabled peer не добавляется в конфиг
    }
    
    let config = `\n[Peer]\n`;
    config += `# ${this.name}\n`;
    config += `PublicKey = ${this.publicKey}\n`;
    
    if (this.presharedKey) {
      config += `PresharedKey = ${this.presharedKey}\n`;
    }
    
    config += `AllowedIPs = ${this.allowedIPs}\n`;
    
    if (this.endpoint) {
      config += `Endpoint = ${this.endpoint}\n`;
    }
    
    if (this.persistentKeepalive > 0) {
      config += `PersistentKeepalive = ${this.persistentKeepalive}\n`;
    }
    
    return config;
  }

  /**
   * Генерация конфига для удалённой стороны
   * @param {Object} interfaceData - данные интерфейса к которому подключается peer
   */
  generateRemoteConfig(interfaceData) {
    let config = '';
    
    // Header
    config += '# ═══════════════════════════════════════════════════════════════\n';
    config += `# Remote Configuration for: ${this.name}\n`;
    config += `# Connect to: ${interfaceData.name}\n`;
    config += `# Protocol: ${interfaceData.protocol === 'amneziawg-2.0' ? 'AmneziaWG 2.0' : 'WireGuard 1.0'}\n`;
    config += '# ═══════════════════════════════════════════════════════════════\n';
    config += '\n';
    
    // [Interface] для удалённой стороны
    config += '[Interface]\n';
    config += '# IMPORTANT: Generate your own private key on remote side:\n';
    config += '#   wg genkey > privatekey\n';
    config += '#   cat privatekey | wg pubkey > publickey\n';
    config += '# Then replace YOUR_PRIVATE_KEY with content of privatekey\n';
    config += 'PrivateKey = YOUR_PRIVATE_KEY\n';
    config += '\n';
    config += '# Listen port (choose any free UDP port)\n';
    config += 'ListenPort = 51820\n';
    config += '\n';
    
    // Remote address если указан
    if (this.remoteAddress) {
      config += `# Tunnel address for this side\n`;
      config += `Address = ${this.remoteAddress}\n`;
      config += '\n';
    }
    
    // AWG параметры если нужно (должны совпадать!)
    if (interfaceData.protocol === 'amneziawg-2.0' && interfaceData.settings) {
      const s = interfaceData.settings;
      config += '# ═══════════════════════════════════════════════════════════════\n';
      config += '# AmneziaWG 2.0 Parameters (MUST match EXACTLY on both sides!)\n';
      config += '# ═══════════════════════════════════════════════════════════════\n';
      config += `Jc = ${s.jc}\n`;
      config += `Jmin = ${s.jmin}\n`;
      config += `Jmax = ${s.jmax}\n`;
      config += `S1 = ${s.s1}\n`;
      config += `S2 = ${s.s2}\n`;
      config += `S3 = ${s.s3}\n`;
      config += `S4 = ${s.s4}\n`;
      config += `H1 = ${s.h1}\n`;
      config += `H2 = ${s.h2}\n`;
      config += `H3 = ${s.h3}\n`;
      config += `H4 = ${s.h4}\n`;
      
      if (s.i1) config += `I1 = ${s.i1}\n`;
      if (s.i2) config += `I2 = ${s.i2}\n`;
      if (s.i3) config += `I3 = ${s.i3}\n`;
      if (s.i4) config += `I4 = ${s.i4}\n`;
      if (s.i5) config += `I5 = ${s.i5}\n`;
      config += '\n';
    }
    
    // [Peer] - подключение к нашему интерфейсу
    config += '[Peer]\n';
    config += `# Hub: ${interfaceData.name}\n`;
    config += `PublicKey = ${interfaceData.publicKey}\n`;
    config += '\n';
    
    // AllowedIPs - что маршрутизировать через туннель
    // Можно указать конкретную подсеть интерфейса или 0.0.0.0/0
    if (interfaceData.address) {
      config += `# Route traffic to hub's network\n`;
      config += `AllowedIPs = ${interfaceData.address}\n`;
    } else {
      config += `# AllowedIPs = 0.0.0.0/0  # Route all traffic through hub\n`;
      config += `AllowedIPs = 10.0.0.0/8  # Or specify networks\n`;
    }
    config += '\n';
    
    // Endpoint
    // Нужно знать публичный IP/домен hub'а
    const hubEndpoint = process.env.WG_HOST || 'YOUR_HUB_PUBLIC_IP';
    config += `# Endpoint of hub\n`;
    config += `Endpoint = ${hubEndpoint}:${interfaceData.listenPort}\n`;
    config += '\n';
    config += 'PersistentKeepalive = 25\n';
    
    // Footer с инструкциями
    config += '\n';
    config += '# ═══════════════════════════════════════════════════════════════\n';
    config += '# IMPORTANT: Update hub with your public key!\n';
    config += '# ═══════════════════════════════════════════════════════════════\n';
    config += '# 1. Generate keys on THIS (remote) side:\n';
    config += '#    wg genkey | tee privatekey | wg pubkey > publickey\n';
    config += '# 2. Replace YOUR_PRIVATE_KEY above with content from privatekey\n';
    config += '# 3. Copy publickey and update peer configuration on hub\n';
    config += '# 4. Save this file as /etc/wireguard/wg0.conf\n';
    
    if (interfaceData.protocol === 'amneziawg-2.0') {
      config += '# 5. Run: awg-quick up wg0\n';
    } else {
      config += '# 5. Run: wg-quick up wg0\n';
    }
    
    config += '# ═══════════════════════════════════════════════════════════════\n';
    
    return config;
  }
}

module.exports = Peer;
