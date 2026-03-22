const fs = require('fs').promises;
const path = require('path');
const TunnelInterface = require('./TunnelInterface');
const Util = require('./Util');
const debug = require('debug')('awg:InterfaceManager');

/**
 * InterfaceManager - менеджер туннельных интерфейсов
 * Управляет созданием, загрузкой, удалением интерфейсов
 */
class InterfaceManager {
  constructor() {
    this.interfaces = new Map(); // interfaceId -> TunnelInterface
    this.dataDir = '/etc/wireguard/data';
    this.interfacesDir = `${this.dataDir}/interfaces`;
  }

  /**
   * Инициализация - загрузить все интерфейсы
   */
  async init() {
    debug('Initializing InterfaceManager...');
    
    // Создать директории
    await fs.mkdir(this.interfacesDir, { recursive: true });
    await fs.mkdir(`${this.dataDir}/peers`, { recursive: true });
    
    // Загрузить существующие интерфейсы
    try {
      const files = await fs.readdir(this.interfacesDir);
      
      for (const file of files) {
        if (!file.endsWith('.json')) continue;
        
        const id = file.replace('.json', '');
        try {
          const iface = await TunnelInterface.load(id);
          this.interfaces.set(id, iface);
          debug(`Loaded interface: ${id}`);
        } catch (err) {
          debug(`Error loading interface ${id}:`, err.message);
        }
      }
      
      debug(`Initialized with ${this.interfaces.size} interfaces`);
    } catch (err) {
      if (err.code !== 'ENOENT') throw err;
      debug('No existing interfaces found');
    }
  }

  /**
   * Получить следующий доступный номер интерфейса
   */
  _getNextInterfaceNumber() {
    let num = 10;
    while (this.interfaces.has(`wg${num}`)) {
      num++;
    }
    return num;
  }

  /**
   * Получить следующий доступный порт
   */
  _getNextListenPort() {
    let port = 51830;
    const usedPorts = new Set();
    
    for (const iface of this.interfaces.values()) {
      usedPorts.add(iface.data.listenPort);
    }
    
    while (usedPorts.has(port)) {
      port++;
    }
    
    return port;
  }

  /**
   * Создать новый интерфейс
   * 
   * @param {Object} options
   * @param {string} options.name - Friendly name
   * @param {string} options.protocol - 'wireguard-1.0' или 'amneziawg-2.0'
   * @param {string} options.address - Tunnel address (например "10.100.0.1/24")
   * @param {number} options.listenPort - Порт (опционально, auto-assign)
   * @param {Object} options.settings - AWG параметры (если AWG 2.0)
   */
  async createInterface(options) {
    debug(`Creating interface: ${options.name}`);
    
    // Валидация
    if (!options.name) {
      throw new Error('Interface name is required');
    }
    
    if (!options.protocol) {
      options.protocol = 'wireguard-1.0';
    }
    
    if (options.protocol === 'amneziawg-2.0' && !options.settings) {
      throw new Error('AWG settings required for amneziawg-2.0 protocol');
    }
    
    // Генерация ключей
    const privateKey = await Util.exec('wg genkey');
    const publicKey = await Util.exec(`echo ${privateKey} | wg pubkey`, {
      log: 'echo ***hidden*** | wg pubkey',
    });
    
    // Получить ID и порт
    const id = `wg${this._getNextInterfaceNumber()}`;
    const listenPort = options.listenPort || this._getNextListenPort();
    
    // Создать объект интерфейса
    const ifaceData = {
      name: options.name,
      protocol: options.protocol,
      privateKey: privateKey.trim(),
      publicKey: publicKey.trim(),
      listenPort,
      address: options.address || '',
      settings: options.settings || {},
      enabled: false,
      createdAt: new Date().toISOString(),
      peerIds: [],
    };
    
    const iface = new TunnelInterface(id, ifaceData);
    
    // Сохранить
    await iface.save();
    
    // Сгенерировать конфиг
    await iface.regenerateConfig();
    
    // Добавить в Map
    this.interfaces.set(id, iface);
    
    debug(`Interface ${id} created`);
    return iface;
  }

  /**
   * Получить интерфейс по ID
   */
  getInterface(id) {
    return this.interfaces.get(id);
  }

  /**
   * Получить все интерфейсы
   */
  getAllInterfaces() {
    return Array.from(this.interfaces.values());
  }

  /**
   * Обновить интерфейс
   */
  async updateInterface(id, updates) {
    const iface = this.interfaces.get(id);
    if (!iface) {
      throw new Error(`Interface ${id} not found`);
    }
    
    // Обновить данные
    Object.assign(iface.data, updates);
    
    // Сохранить
    await iface.save();
    
    // Регенерировать конфиг
    await iface.regenerateConfig();
    
    // Перезагрузить если запущен
    if (iface.data.enabled) {
      await iface.reload();
    }
    
    debug(`Interface ${id} updated`);
    return iface;
  }

  /**
   * Удалить интерфейс
   */
  async deleteInterface(id) {
    const iface = this.interfaces.get(id);
    if (!iface) {
      throw new Error(`Interface ${id} not found`);
    }
    
    // Удалить интерфейс
    await iface.delete();
    
    // Удалить из Map
    this.interfaces.delete(id);
    
    debug(`Interface ${id} deleted`);
  }

  /**
   * Запустить интерфейс
   */
  async startInterface(id) {
    const iface = this.interfaces.get(id);
    if (!iface) {
      throw new Error(`Interface ${id} not found`);
    }
    
    await iface.start();
    return iface;
  }

  /**
   * Остановить интерфейс
   */
  async stopInterface(id) {
    const iface = this.interfaces.get(id);
    if (!iface) {
      throw new Error(`Interface ${id} not found`);
    }
    
    await iface.stop();
    return iface;
  }

  /**
   * Перезапустить интерфейс
   */
  async restartInterface(id) {
    const iface = this.interfaces.get(id);
    if (!iface) {
      throw new Error(`Interface ${id} not found`);
    }
    
    await iface.restart();
    return iface;
  }

  /**
   * Добавить peer к интерфейсу
   */
  async addPeer(interfaceId, peerData) {
    const iface = this.interfaces.get(interfaceId);
    if (!iface) {
      throw new Error(`Interface ${interfaceId} not found`);
    }
    
    return await iface.addPeer(peerData);
  }

  /**
   * Обновить peer
   */
  async updatePeer(interfaceId, peerId, updates) {
    const iface = this.interfaces.get(interfaceId);
    if (!iface) {
      throw new Error(`Interface ${interfaceId} not found`);
    }
    
    return await iface.updatePeer(peerId, updates);
  }

  /**
   * Удалить peer
   */
  async removePeer(interfaceId, peerId) {
    const iface = this.interfaces.get(interfaceId);
    if (!iface) {
      throw new Error(`Interface ${interfaceId} not found`);
    }
    
    await iface.removePeer(peerId);
  }

  /**
   * Получить peer
   */
  getPeer(interfaceId, peerId) {
    const iface = this.interfaces.get(interfaceId);
    if (!iface) {
      throw new Error(`Interface ${interfaceId} not found`);
    }
    
    return iface.getPeer(peerId);
  }

  /**
   * Получить все peers интерфейса
   */
  getPeers(interfaceId) {
    const iface = this.interfaces.get(interfaceId);
    if (!iface) {
      throw new Error(`Interface ${interfaceId} not found`);
    }
    
    return iface.getAllPeers();
  }

  /**
   * Получить конфиг для peer (для скачивания на удалённой стороне)
   */
  async getPeerRemoteConfig(interfaceId, peerId) {
    const iface = this.interfaces.get(interfaceId);
    if (!iface) {
      throw new Error(`Interface ${interfaceId} not found`);
    }
    
    const peer = iface.getPeer(peerId);
    if (!peer) {
      throw new Error(`Peer ${peerId} not found`);
    }
    
    // Генерировать конфиг для удалённой стороны
    return peer.generateRemoteConfig(iface.data);
  }
}

// Singleton instance
let instance = null;

module.exports = {
  /**
   * Получить singleton instance
   */
  getInstance: async () => {
    if (!instance) {
      instance = new InterfaceManager();
      await instance.init();
    }
    return instance;
  },
  
  /**
   * Для тестирования - создать новый instance
   */
  createInstance: async () => {
    const manager = new InterfaceManager();
    await manager.init();
    return manager;
  },
};
