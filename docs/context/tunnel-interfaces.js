const express = require('express');
const router = express.Router();
const InterfaceManager = require('../lib/InterfaceManager');
const debug = require('debug')('awg:api:tunnel-interfaces');

// ============================================================================
// TUNNEL INTERFACES
// ============================================================================

/**
 * GET /api/tunnel-interfaces
 * Получить список всех интерфейсов
 */
router.get('/', async (req, res) => {
  try {
    const manager = await InterfaceManager.getInstance();
    const interfaces = manager.getAllInterfaces();
    
    res.json({
      interfaces: interfaces.map(iface => iface.toJSON()),
    });
  } catch (err) {
    debug('Error getting interfaces:', err);
    res.status(500).json({ error: err.message });
  }
});

/**
 * POST /api/tunnel-interfaces
 * Создать новый интерфейс
 * 
 * Body:
 * {
 *   name: "Main VPN Hub",
 *   protocol: "wireguard-1.0" | "amneziawg-2.0",
 *   address: "10.100.0.1/24",
 *   listenPort: 51830  // optional
 *   settings: { ... }  // required if protocol === 'amneziawg-2.0'
 * }
 */
router.post('/', async (req, res) => {
  try {
    const { name, protocol, address, listenPort, settings } = req.body;
    
    // Валидация
    if (!name) {
      return res.status(400).json({ error: 'Name is required' });
    }
    
    if (protocol === 'amneziawg-2.0' && !settings) {
      return res.status(400).json({ error: 'Settings required for AmneziaWG 2.0' });
    }
    
    const manager = await InterfaceManager.getInstance();
    const iface = await manager.createInterface({
      name,
      protocol,
      address,
      listenPort,
      settings,
    });
    
    debug(`Interface created: ${iface.id}`);
    
    res.status(201).json({
      interface: iface.toJSON(),
    });
  } catch (err) {
    debug('Error creating interface:', err);
    res.status(500).json({ error: err.message });
  }
});

/**
 * GET /api/tunnel-interfaces/:id
 * Получить информацию об интерфейсе
 */
router.get('/:id', async (req, res) => {
  try {
    const manager = await InterfaceManager.getInstance();
    const iface = manager.getInterface(req.params.id);
    
    if (!iface) {
      return res.status(404).json({ error: 'Interface not found' });
    }
    
    res.json({
      interface: iface.toJSON(),
    });
  } catch (err) {
    debug('Error getting interface:', err);
    res.status(500).json({ error: err.message });
  }
});

/**
 * PATCH /api/tunnel-interfaces/:id
 * Обновить интерфейс
 * 
 * Body:
 * {
 *   name: "New Name",
 *   address: "10.200.0.1/24",
 *   ... (любые поля из данных интерфейса)
 * }
 */
router.patch('/:id', async (req, res) => {
  try {
    const manager = await InterfaceManager.getInstance();
    const iface = await manager.updateInterface(req.params.id, req.body);
    
    debug(`Interface updated: ${req.params.id}`);
    
    res.json({
      interface: iface.toJSON(),
    });
  } catch (err) {
    debug('Error updating interface:', err);
    
    if (err.message.includes('not found')) {
      return res.status(404).json({ error: err.message });
    }
    
    res.status(500).json({ error: err.message });
  }
});

/**
 * DELETE /api/tunnel-interfaces/:id
 * Удалить интерфейс
 */
router.delete('/:id', async (req, res) => {
  try {
    const manager = await InterfaceManager.getInstance();
    await manager.deleteInterface(req.params.id);
    
    debug(`Interface deleted: ${req.params.id}`);
    
    res.json({ success: true });
  } catch (err) {
    debug('Error deleting interface:', err);
    
    if (err.message.includes('not found')) {
      return res.status(404).json({ error: err.message });
    }
    
    res.status(500).json({ error: err.message });
  }
});

/**
 * POST /api/tunnel-interfaces/:id/start
 * Запустить интерфейс
 */
router.post('/:id/start', async (req, res) => {
  try {
    const manager = await InterfaceManager.getInstance();
    const iface = await manager.startInterface(req.params.id);
    
    debug(`Interface started: ${req.params.id}`);
    
    res.json({
      interface: iface.toJSON(),
    });
  } catch (err) {
    debug('Error starting interface:', err);
    res.status(500).json({ error: err.message });
  }
});

/**
 * POST /api/tunnel-interfaces/:id/stop
 * Остановить интерфейс
 */
router.post('/:id/stop', async (req, res) => {
  try {
    const manager = await InterfaceManager.getInstance();
    const iface = await manager.stopInterface(req.params.id);
    
    debug(`Interface stopped: ${req.params.id}`);
    
    res.json({
      interface: iface.toJSON(),
    });
  } catch (err) {
    debug('Error stopping interface:', err);
    res.status(500).json({ error: err.message });
  }
});

/**
 * POST /api/tunnel-interfaces/:id/restart
 * Перезапустить интерфейс
 */
router.post('/:id/restart', async (req, res) => {
  try {
    const manager = await InterfaceManager.getInstance();
    const iface = await manager.restartInterface(req.params.id);
    
    debug(`Interface restarted: ${req.params.id}`);
    
    res.json({
      interface: iface.toJSON(),
    });
  } catch (err) {
    debug('Error restarting interface:', err);
    res.status(500).json({ error: err.message });
  }
});

// ============================================================================
// PEERS
// ============================================================================

/**
 * GET /api/tunnel-interfaces/:id/peers
 * Получить список peers интерфейса
 */
router.get('/:id/peers', async (req, res) => {
  try {
    const manager = await InterfaceManager.getInstance();
    const peers = manager.getPeers(req.params.id);
    
    res.json({
      peers: peers.map(peer => peer.toJSON()),
    });
  } catch (err) {
    debug('Error getting peers:', err);
    
    if (err.message.includes('not found')) {
      return res.status(404).json({ error: err.message });
    }
    
    res.status(500).json({ error: err.message });
  }
});

/**
 * POST /api/tunnel-interfaces/:id/peers
 * Добавить peer к интерфейсу
 * 
 * Body:
 * {
 *   name: "Office-A",
 *   publicKey: "...",
 *   endpoint: "office-a.com:51820",
 *   allowedIPs: "192.168.1.0/24",
 *   remoteAddress: "10.100.0.2/24",
 *   persistentKeepalive: 25
 * }
 */
router.post('/:id/peers', async (req, res) => {
  try {
    const { name, publicKey, endpoint, allowedIPs, remoteAddress, persistentKeepalive } = req.body;
    
    // Валидация
    if (!name || !publicKey || !allowedIPs) {
      return res.status(400).json({ 
        error: 'name, publicKey, and allowedIPs are required' 
      });
    }
    
    const manager = await InterfaceManager.getInstance();
    const peer = await manager.addPeer(req.params.id, {
      name,
      publicKey,
      endpoint,
      allowedIPs,
      remoteAddress,
      persistentKeepalive,
    });
    
    debug(`Peer added: ${peer.id} to ${req.params.id}`);
    
    res.status(201).json({
      peer: peer.toJSON(),
    });
  } catch (err) {
    debug('Error adding peer:', err);
    
    if (err.message.includes('not found')) {
      return res.status(404).json({ error: err.message });
    }
    
    if (err.message.includes('validation failed')) {
      return res.status(400).json({ error: err.message });
    }
    
    res.status(500).json({ error: err.message });
  }
});

/**
 * GET /api/tunnel-interfaces/:id/peers/:peerId
 * Получить информацию о peer
 */
router.get('/:id/peers/:peerId', async (req, res) => {
  try {
    const manager = await InterfaceManager.getInstance();
    const peer = manager.getPeer(req.params.id, req.params.peerId);
    
    if (!peer) {
      return res.status(404).json({ error: 'Peer not found' });
    }
    
    res.json({
      peer: peer.toJSON(),
    });
  } catch (err) {
    debug('Error getting peer:', err);
    res.status(500).json({ error: err.message });
  }
});

/**
 * PATCH /api/tunnel-interfaces/:id/peers/:peerId
 * Обновить peer
 */
router.patch('/:id/peers/:peerId', async (req, res) => {
  try {
    const manager = await InterfaceManager.getInstance();
    const peer = await manager.updatePeer(req.params.id, req.params.peerId, req.body);
    
    debug(`Peer updated: ${req.params.peerId}`);
    
    res.json({
      peer: peer.toJSON(),
    });
  } catch (err) {
    debug('Error updating peer:', err);
    
    if (err.message.includes('not found')) {
      return res.status(404).json({ error: err.message });
    }
    
    res.status(500).json({ error: err.message });
  }
});

/**
 * DELETE /api/tunnel-interfaces/:id/peers/:peerId
 * Удалить peer
 */
router.delete('/:id/peers/:peerId', async (req, res) => {
  try {
    const manager = await InterfaceManager.getInstance();
    await manager.removePeer(req.params.id, req.params.peerId);
    
    debug(`Peer deleted: ${req.params.peerId}`);
    
    res.json({ success: true });
  } catch (err) {
    debug('Error deleting peer:', err);
    
    if (err.message.includes('not found')) {
      return res.status(404).json({ error: err.message });
    }
    
    res.status(500).json({ error: err.message });
  }
});

/**
 * GET /api/tunnel-interfaces/:id/peers/:peerId/config
 * Скачать конфиг для peer (для применения на удалённой стороне)
 */
router.get('/:id/peers/:peerId/config', async (req, res) => {
  try {
    const manager = await InterfaceManager.getInstance();
    const config = await manager.getPeerRemoteConfig(req.params.id, req.params.peerId);
    
    const peer = manager.getPeer(req.params.id, req.params.peerId);
    const filename = `${peer.name.replace(/\s+/g, '-')}.conf`;
    
    res.setHeader('Content-Type', 'text/plain');
    res.setHeader('Content-Disposition', `attachment; filename="${filename}"`);
    res.send(config);
    
    debug(`Config downloaded for peer: ${req.params.peerId}`);
  } catch (err) {
    debug('Error getting peer config:', err);
    
    if (err.message.includes('not found')) {
      return res.status(404).json({ error: err.message });
    }
    
    res.status(500).json({ error: err.message });
  }
});

module.exports = router;
