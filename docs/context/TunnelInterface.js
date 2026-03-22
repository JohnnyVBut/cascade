const fs = require('fs').promises;
const path = require('path');
const Util = require('../Util');
const Peer = require('../Peer');
const debug = require('debug')('awg:TunnelInterface');

/**
 * TunnelInterface - туннельный интерфейс (wg10, wg11, etc.)
 * Один интерфейс может иметь множество peers (hub-and-spoke модель)
 */
class TunnelInterface {
  /**
   * @param {string} id - имя интерфейса (wg10, wg11, etc.)
   * @param {Object} data - данные интерфейса
   */
  constructor(id, data) {
    this.id = id;
    this.data = {
      name: data.name,                    // Friendly name
      protocol: data.protocol || 'wireguard-1.0',
      privateKey: data.privateKey,
      publicKey: data.publicKey,
      listenPort: data.listenPort,
      address: data.address || '',        // Tunnel address (например 10.100.0.1/24)
      settings: data.settings || {},      // AWG параметры
      enabled: data.enabled !== false,
      createdAt: data.createdAt || new Date().toISOString(),
      peerIds: data.peerIds || [],        // Массив ID peers
    };
    
    this.peers = new Map(); // peerId -> Peer instance
    
    // Пути
    this.dataDir = '/etc/wireguard/data';
    this.interfaceFile = `${this.dataDir}/interfaces/${this.id}.json`;
    this.peersDir = `${this.dataDir}/peers/${this.id}`;
    this.confFile = `/etc/wireguard/${this.id}.conf`;
    
    debug(`TunnelInterface created: ${this.id} (${this.data.name})`);
  }

  /**
   * Сохранить данные интерфейса
   */
  async save() {
    await fs.mkdir(path.dirname(this.interfaceFile), { recursive: true });
    await fs.mkdir(this.peersDir, { recursive: true });
    
    const json = JSON.stringify(this.data, null, 2);
    await fs.writeFile(this.interfaceFile, json);
    
    debug(`Interface ${this.id} saved`);
  }

  /**
   * Загрузить интерфейс из файла
   */
  static async load(id) {
    const interfaceFile = `/etc/wireguard/data/interfaces/${id}.json`;
    const json = await fs.readFile(interfaceFile, 'utf8');
    const data = JSON.parse(json);
    
    const iface = new TunnelInterface(id, data);
    await iface.loadPeers();
    
    debug(`Interface ${id} loaded`);
    return iface;
  }

  /**
   * Загрузить все peers для этого интерфейса
   */
  async loadPeers() {
    try {
      const files = await fs.readdir(this.peersDir);
      
      for (const file of files) {
        if (!file.endsWith('.json')) continue;
        
        const peerFile = path.join(this.peersDir, file);
        const json = await fs.readFile(peerFile, 'utf8');
        const peerData = JSON.parse(json);
        
        const peer = new Peer(peerData);
        this.peers.set(peer.id, peer);
      }
      
      debug(`Loaded ${this.peers.size} peers for ${this.id}`);
    } catch (err) {
      if (err.code !== 'ENOENT') throw err;
      // Директория не существует - нет peers
    }
  }

  /**
   * Добавить peer
   */
  async addPeer(peerData) {
    // Валидация
    const peer = new Peer({
      ...peerData,
      interfaceId: this.id,
    });
    
    const errors = peer.validate();
    if (errors.length > 0) {
      throw new Error(`Peer validation failed: ${errors.join(', ')}`);
    }
    
    // Сохранить peer
    const peerFile = path.join(this.peersDir, `${peer.id}.json`);
    await fs.mkdir(this.peersDir, { recursive: true });
    await fs.writeFile(peerFile, JSON.stringify(peer.toJSON(), null, 2));
    
    // Добавить в Map
    this.peers.set(peer.id, peer);
    
    // Обновить список peer IDs
    this.data.peerIds.push(peer.id);
    await this.save();
    
    // Регенерировать конфиг
    await this.regenerateConfig();
    
    // Перезагрузить интерфейс если запущен
    if (this.data.enabled) {
      await this.reload();
    }
    
    debug(`Peer ${peer.id} added to ${this.id}`);
    return peer;
  }

  /**
   * Обновить peer
   */
  async updatePeer(peerId, updates) {
    const peer = this.peers.get(peerId);
    if (!peer) {
      throw new Error(`Peer ${peerId} not found`);
    }
    
    // Обновить данные
    Object.assign(peer, updates);
    
    // Валидация
    const errors = peer.validate();
    if (errors.length > 0) {
      throw new Error(`Peer validation failed: ${errors.join(', ')}`);
    }
    
    // Сохранить
    const peerFile = path.join(this.peersDir, `${peer.id}.json`);
    await fs.writeFile(peerFile, JSON.stringify(peer.toJSON(), null, 2));
    
    // Регенерировать конфиг
    await this.regenerateConfig();
    
    // Перезагрузить
    if (this.data.enabled) {
      await this.reload();
    }
    
    debug(`Peer ${peerId} updated`);
    return peer;
  }

  /**
   * Удалить peer
   */
  async removePeer(peerId) {
    const peer = this.peers.get(peerId);
    if (!peer) {
      throw new Error(`Peer ${peerId} not found`);
    }
    
    // Удалить файл
    const peerFile = path.join(this.peersDir, `${peer.id}.json`);
    await fs.unlink(peerFile);
    
    // Удалить из Map
    this.peers.delete(peerId);
    
    // Обновить список
    this.data.peerIds = this.data.peerIds.filter(id => id !== peerId);
    await this.save();
    
    // Регенерировать конфиг
    await this.regenerateConfig();
    
    // Перезагрузить
    if (this.data.enabled) {
      await this.reload();
    }
    
    debug(`Peer ${peerId} removed`);
  }

  /**
   * Получить peer по ID
   */
  getPeer(peerId) {
    return this.peers.get(peerId);
  }

  /**
   * Получить все peers
   */
  getAllPeers() {
    return Array.from(this.peers.values());
  }

  /**
   * Сгенерировать WireGuard конфиг
   */
  generateWgConfig() {
    let config = '';
    
    // ======== [Interface] ========
    config += '[Interface]\n';
    config += `# ${this.data.name}\n`;
    config += `PrivateKey = ${this.data.privateKey}\n`;
    config += `ListenPort = ${this.data.listenPort}\n`;
    
    if (this.data.address) {
      config += `Address = ${this.data.address}\n`;
    }
    
    // AWG параметры
    if (this.data.protocol === 'amneziawg-2.0') {
      const s = this.data.settings;
      config += '\n# AmneziaWG 2.0 Parameters\n';
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
    }
    
    // ======== [Peer] секции ========
    for (const peer of this.peers.values()) {
      config += peer.toWgConfig();
    }
    
    return config;
  }

  /**
   * Регенерировать WireGuard конфиг
   */
  async regenerateConfig() {
    const config = this.generateWgConfig();
    await fs.writeFile(this.confFile, config, { mode: 0o600 });
    debug(`Config regenerated: ${this.confFile}`);
  }

  /**
   * Запустить интерфейс
   */
  async start() {
    const cmd = this.data.protocol === 'amneziawg-2.0' ? 'awg-quick' : 'wg-quick';
    await Util.exec(`${cmd} up ${this.id}`);
    
    this.data.enabled = true;
    await this.save();
    
    debug(`Interface ${this.id} started`);
  }

  /**
   * Остановить интерфейс
   */
  async stop() {
    try {
      const cmd = this.data.protocol === 'amneziawg-2.0' ? 'awg-quick' : 'wg-quick';
      await Util.exec(`${cmd} down ${this.id}`);
    } catch (err) {
      if (!err.message.includes('is not a WireGuard interface')) {
        throw err;
      }
    }
    
    this.data.enabled = false;
    await this.save();
    
    debug(`Interface ${this.id} stopped`);
  }

  /**
   * Перезапустить интерфейс
   */
  async restart() {
    await this.stop();
    await this.start();
  }

  /**
   * Перезагрузить конфиг без остановки (hot reload)
   */
  async reload() {
    try {
      const cmd = this.data.protocol === 'amneziawg-2.0' ? 'awg' : 'wg';
      const stripCmd = this.data.protocol === 'amneziawg-2.0' ? 'awg-quick' : 'wg-quick';
      
      // wg syncconf wg10 <(wg-quick strip wg10)
      await Util.exec(`${cmd} syncconf ${this.id} <(${stripCmd} strip ${this.id})`);
      
      debug(`Interface ${this.id} reloaded (hot)`);
    } catch (err) {
      debug(`Hot reload failed, restarting:`, err.message);
      await this.restart();
    }
  }

  /**
   * Удалить интерфейс
   */
  async delete() {
    // Остановить
    if (this.data.enabled) {
      await this.stop();
    }
    
    // Удалить peers
    for (const peerId of Array.from(this.peers.keys())) {
      await this.removePeer(peerId);
    }
    
    // Удалить файлы
    await fs.unlink(this.interfaceFile);
    await fs.unlink(this.confFile);
    
    try {
      await fs.rmdir(this.peersDir);
    } catch (err) {
      // Ignore if directory not empty
    }
    
    debug(`Interface ${this.id} deleted`);
  }

  /**
   * Экспорт для API
   */
  toJSON() {
    return {
      id: this.id,
      name: this.data.name,
      protocol: this.data.protocol,
      listenPort: this.data.listenPort,
      address: this.data.address,
      publicKey: this.data.publicKey,
      enabled: this.data.enabled,
      createdAt: this.data.createdAt,
      peerCount: this.peers.size,
      peers: Array.from(this.peers.values()).map(p => p.toJSON()),
    };
  }
}

module.exports = TunnelInterface;
