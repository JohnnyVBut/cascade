#!/usr/bin/env python3
"""
cascade-s2s.py — Connect two Cascade routers via an S2S WireGuard/AmneziaWG tunnel.

Requirements: Python 3.8+, no external dependencies.

Usage:
  python3 cascade-s2s.py \\
    --url-a  https://1.2.3.4/ADMIN_PATH_A  --token-a ws_<token_a> \\
    --url-b  https://5.6.7.8/ADMIN_PATH_B  --token-b ws_<token_b> \\
    --network 10.200.0.0/30

Optional:
  --name       "Berlin-Moscow"      Human-readable link name
  --protocol   amneziawg-2.0        or wireguard-1.0 (default: amneziawg-2.0)
  --port-a     51850                Listen port on server A (default: auto)
  --port-b     51851                Listen port on server B (default: auto)
  --no-verify-ssl                   Skip TLS verification (self-signed / IP certs)

What this script does:
  1. Verifies connectivity to both servers
  2. Calculates tunnel IPs from --network (.1 for A, .2 for B)
  3. Generates matching AWG2 obfuscation params (same on both sides)
  4. Creates tunnel interfaces on both servers
  5. Starts both interfaces
  6. Exports params from A → imports on B  (B auto-generates PSK)
  7. Exports params from B → imports on A  (PSK is now synchronized)
  8. Creates monitoring gateways on both sides
"""

import argparse
import ipaddress
import json
import ssl
import sys
import urllib.error
import urllib.request
from typing import Any, Dict, Optional, Tuple


# ─────────────────────────────────────────────────────────────────────────────
# API client
# ─────────────────────────────────────────────────────────────────────────────

class CascadeAPI:
    def __init__(self, base_url: str, token: str, verify_ssl: bool = True):
        self.base_url = base_url.rstrip('/')
        self.token = token
        self._ssl = self._make_ssl_ctx(verify_ssl)

    @staticmethod
    def _make_ssl_ctx(verify: bool) -> ssl.SSLContext:
        ctx = ssl.create_default_context()
        if not verify:
            ctx.check_hostname = False
            ctx.verify_mode = ssl.CERT_NONE
        return ctx

    def _request(self, method: str, path: str, body: Optional[Dict] = None) -> Any:
        url = f"{self.base_url}/api{path}"
        data = json.dumps(body).encode() if body is not None else None
        req = urllib.request.Request(
            url, data=data, method=method,
            headers={
                'Authorization': f'Bearer {self.token}',
                'Content-Type': 'application/json',
                'Accept': 'application/json',
            },
        )
        try:
            with urllib.request.urlopen(req, context=self._ssl, timeout=30) as resp:
                return json.loads(resp.read())
        except urllib.error.HTTPError as exc:
            raw = exc.read().decode(errors='replace')
            try:
                msg = json.loads(raw).get('error', raw)
            except json.JSONDecodeError:
                msg = raw
            raise RuntimeError(f"HTTP {exc.code} {method} {path}: {msg}") from exc

    def get(self, path: str) -> Any:
        return self._request('GET', path)

    def post(self, path: str, body: Optional[Dict] = None) -> Any:
        return self._request('POST', path, body or {})

    def health(self) -> bool:
        try:
            r = self.get('/health')
            return r.get('status') == 'ok'
        except Exception as exc:
            info(f'health check error: {exc}')
            return False


# ─────────────────────────────────────────────────────────────────────────────
# Logging helpers
# ─────────────────────────────────────────────────────────────────────────────

def log(msg: str = '') -> None:
    print(msg, flush=True)

def ok(msg: str) -> None:
    print(f'  \033[32m✓\033[0m {msg}', flush=True)

def info(msg: str) -> None:
    print(f'  → {msg}', flush=True)

def fail(msg: str) -> None:
    print(f'  \033[31m✗\033[0m {msg}', flush=True, file=sys.stderr)
    sys.exit(1)


# ─────────────────────────────────────────────────────────────────────────────
# Helpers
# ─────────────────────────────────────────────────────────────────────────────

def pick_ips(network: str) -> Tuple[str, str]:
    """Return (ip_a/mask, ip_b/mask) — first two host IPs from the network."""
    net = ipaddress.ip_network(network, strict=False)
    hosts = list(net.hosts())
    if len(hosts) < 2:
        raise ValueError(f"Network {network} has fewer than 2 host addresses")
    mask = net.prefixlen
    return f'{hosts[0]}/{mask}', f'{hosts[1]}/{mask}'


def strip_mask(addr: str) -> str:
    """'10.200.0.1/30' → '10.200.0.1'"""
    return addr.split('/')[0]


def next_free_port(api: CascadeAPI, preferred: Optional[int]) -> int:
    """Return preferred port if free, otherwise the first unused port ≥ 51830."""
    ifaces = api.get('/tunnel-interfaces').get('interfaces', [])
    used = {i.get('listenPort') for i in ifaces}
    if preferred and preferred not in used:
        return preferred
    port = 51830
    while port in used:
        port += 1
    return port


def awg_settings_from_params(params: Dict) -> Dict:
    """Extract AWG2Settings-compatible dict from generate response (drop 'profile')."""
    keys = ['jc', 'jmin', 'jmax', 's1', 's2', 's3', 's4',
            'h1', 'h2', 'h3', 'h4', 'i1', 'i2', 'i3', 'i4', 'i5']
    return {k: params[k] for k in keys if k in params}


# ─────────────────────────────────────────────────────────────────────────────
# API actions
# ─────────────────────────────────────────────────────────────────────────────

def generate_awg_params(api: CascadeAPI) -> Optional[Dict]:
    """Generate AWG2 obfuscation params via /api/templates/generate."""
    try:
        result = api.post('/templates/generate', {
            'profile':    'tls13',
            'intensity':  'medium',
            'host':       'example.com',
        })
        return awg_settings_from_params(result)
    except Exception as exc:
        info(f'AWG param generation failed ({exc}), using server defaults')
        return None


def create_interface(api: CascadeAPI, name: str, address: str,
                     port: int, protocol: str,
                     settings: Optional[Dict]) -> Dict:
    body: Dict = {
        'name':          name,
        'address':       address,
        'listenPort':    port,
        'protocol':      protocol,
        'disableRoutes': True,   # S2S interconnect: no NAT masquerade
    }
    if settings and protocol == 'amneziawg-2.0':
        body['settings'] = settings
    return api.post('/tunnel-interfaces', body)


def start_interface(api: CascadeAPI, iface_id: str) -> None:
    api.post(f'/tunnel-interfaces/{iface_id}/start')


def export_params(api: CascadeAPI, iface_id: str) -> Dict:
    return api.get(f'/tunnel-interfaces/{iface_id}/export-params')


def import_peer(api: CascadeAPI, iface_id: str, params: Dict) -> Dict:
    result = api.post(f'/tunnel-interfaces/{iface_id}/peers/import-json', params)
    return result.get('peer', result)


def create_gateway(api: CascadeAPI, name: str, iface_id: str,
                   gateway_ip: str, monitor_ip: str) -> Dict:
    result = api.post('/gateways', {
        'name':               name,
        'interface':          iface_id,
        'gatewayIP':          gateway_ip,
        'monitorAddress':     monitor_ip,
        'interval':           5,
        'windowSeconds':      60,
        'healthyThreshold':   80,
        'degradedThreshold':  50,
    })
    return result.get('gateway', result)


# ─────────────────────────────────────────────────────────────────────────────
# Main
# ─────────────────────────────────────────────────────────────────────────────

def main() -> None:
    parser = argparse.ArgumentParser(
        description='Connect two Cascade routers via an S2S tunnel.',
        formatter_class=argparse.RawDescriptionHelpFormatter,
        epilog=__doc__,
    )
    parser.add_argument('--url-a',   required=True, metavar='URL',
                        help='Server A base URL incl. admin path')
    parser.add_argument('--token-a', required=True, metavar='TOKEN',
                        help='Server A API token (ws_...)')
    parser.add_argument('--url-b',   required=True, metavar='URL',
                        help='Server B base URL incl. admin path')
    parser.add_argument('--token-b', required=True, metavar='TOKEN',
                        help='Server B API token (ws_...)')
    parser.add_argument('--network', required=True, metavar='CIDR',
                        help='Interconnect subnet, e.g. 10.200.0.0/30')
    parser.add_argument('--name',    default='',    metavar='NAME',
                        help='Human-readable link name (default: s2s-<net>)')
    parser.add_argument('--protocol', default='amneziawg-2.0',
                        choices=['amneziawg-2.0', 'wireguard-1.0'],
                        help='VPN protocol (default: amneziawg-2.0)')
    parser.add_argument('--port-a',  type=int, default=None,
                        help='Listen port on server A (default: auto)')
    parser.add_argument('--port-b',  type=int, default=None,
                        help='Listen port on server B (default: auto)')
    parser.add_argument('--no-verify-ssl', action='store_true',
                        help='Disable TLS certificate verification')
    args = parser.parse_args()

    verify_ssl = not args.no_verify_ssl
    net_safe   = args.network.replace('/', '-').replace('.', '-')
    link_name  = args.name or f's2s-{net_safe}'

    # ── Header ──────────────────────────────────────────────────────────────
    log()
    log('╔══════════════════════════════════════════════════╗')
    log('║         Cascade S2S Tunnel Setup                 ║')
    log('╚══════════════════════════════════════════════════╝')
    log()

    # ── Step 1: Connectivity ─────────────────────────────────────────────────
    log('Step 1: Connecting to both servers...')
    api_a = CascadeAPI(args.url_a, args.token_a, verify_ssl)
    api_b = CascadeAPI(args.url_b, args.token_b, verify_ssl)

    if not api_a.health():
        fail(f'Cannot reach Server A: {args.url_a}')
    ok(f'Server A reachable: {args.url_a}')

    if not api_b.health():
        fail(f'Cannot reach Server B: {args.url_b}')
    ok(f'Server B reachable: {args.url_b}')

    # ── Step 2: IP allocation ────────────────────────────────────────────────
    log()
    log('Step 2: Allocating tunnel IP addresses...')
    try:
        ip_a, ip_b = pick_ips(args.network)
    except ValueError as exc:
        fail(str(exc))
    ok(f'Server A tunnel IP : {ip_a}')
    ok(f'Server B tunnel IP : {ip_b}')

    # ── Step 3: AWG2 params ──────────────────────────────────────────────────
    settings = None
    if args.protocol == 'amneziawg-2.0':
        log()
        log('Step 3: Generating AWG2 obfuscation parameters...')
        settings = generate_awg_params(api_a)
        if settings:
            ok('Generated (TLS 1.3 profile, medium intensity)')
            ok('Same params will be applied to both sides')
        else:
            ok('Using server-side defaults')
    else:
        log()
        log('Step 3: Protocol is WireGuard 1.0 — no obfuscation params needed')

    # ── Step 4: Create interfaces ────────────────────────────────────────────
    log()
    log('Step 4: Creating tunnel interfaces...')

    port_a = next_free_port(api_a, args.port_a)
    info(f'Server A port: {port_a}')
    try:
        iface_a = create_interface(api_a, link_name, ip_a, port_a, args.protocol, settings)
    except RuntimeError as exc:
        fail(f'Failed to create interface on Server A: {exc}')
    iface_a_id = iface_a.get('id', '?')
    ok(f'Server A: interface {iface_a_id} created ({ip_a}:{port_a})')

    port_b = next_free_port(api_b, args.port_b)
    info(f'Server B port: {port_b}')
    try:
        iface_b = create_interface(api_b, link_name, ip_b, port_b, args.protocol, settings)
    except RuntimeError as exc:
        fail(f'Failed to create interface on Server B: {exc}')
    iface_b_id = iface_b.get('id', '?')
    ok(f'Server B: interface {iface_b_id} created ({ip_b}:{port_b})')

    # ── Step 5: Start interfaces ─────────────────────────────────────────────
    log()
    log('Step 5: Starting interfaces...')
    try:
        start_interface(api_a, iface_a_id)
        ok(f'Server A: {iface_a_id} started')
    except RuntimeError as exc:
        fail(f'Failed to start interface on Server A: {exc}')

    try:
        start_interface(api_b, iface_b_id)
        ok(f'Server B: {iface_b_id} started')
    except RuntimeError as exc:
        fail(f'Failed to start interface on Server B: {exc}')

    # ── Step 6: Exchange peer params ─────────────────────────────────────────
    log()
    log('Step 6: Exchanging peer parameters (PSK sync)...')

    # A → B: export A's public key + endpoint → B creates peer, generates PSK
    try:
        params_a = export_params(api_a, iface_a_id)
        info('Exported Server A params')
        peer_on_b = import_peer(api_b, iface_b_id, params_a)
        ok(f'Server B: created peer for A (id: {peer_on_b.get("id", "?")})')
    except RuntimeError as exc:
        fail(f'Failed to exchange A→B: {exc}')

    # B → A: export B's params (now includes PSK) → A imports PSK
    try:
        params_b = export_params(api_b, iface_b_id)
        info('Exported Server B params (includes PSK)')
        peer_on_a = import_peer(api_a, iface_a_id, params_b)
        ok(f'Server A: created peer for B (id: {peer_on_a.get("id", "?")})')
    except RuntimeError as exc:
        fail(f'Failed to exchange B→A: {exc}')

    # ── Step 7: Gateways ─────────────────────────────────────────────────────
    log()
    log('Step 7: Creating monitoring gateways...')

    ip_a_plain = strip_mask(ip_a)
    ip_b_plain = strip_mask(ip_b)

    try:
        gw_a = create_gateway(api_a,
                               name=f'{link_name}-remote',
                               iface_id=iface_a_id,
                               gateway_ip=ip_b_plain,
                               monitor_ip=ip_b_plain)
        ok(f'Server A: gateway "{gw_a.get("name", "?")}" → ping {ip_b_plain} via {iface_a_id}')
    except RuntimeError as exc:
        info(f'Gateway on Server A failed (non-fatal): {exc}')

    try:
        gw_b = create_gateway(api_b,
                               name=f'{link_name}-remote',
                               iface_id=iface_b_id,
                               gateway_ip=ip_a_plain,
                               monitor_ip=ip_a_plain)
        ok(f'Server B: gateway "{gw_b.get("name", "?")}" → ping {ip_a_plain} via {iface_b_id}')
    except RuntimeError as exc:
        info(f'Gateway on Server B failed (non-fatal): {exc}')

    # ── Summary ──────────────────────────────────────────────────────────────
    log()
    log('╔══════════════════════════════════════════════════╗')
    log('║              Tunnel setup complete!              ║')
    log('╚══════════════════════════════════════════════════╝')
    log()
    log(f'  Link name  : {link_name}')
    log(f'  Protocol   : {args.protocol}')
    log(f'  Network    : {args.network}')
    log(f'  Server A   : {ip_a}  →  {iface_a_id}  port {port_a}')
    log(f'  Server B   : {ip_b}  →  {iface_b_id}  port {port_b}')
    log()
    log('  Verify connectivity:')
    log(f'    On Server A:  ping {ip_b_plain}')
    log(f'    On Server B:  ping {ip_a_plain}')
    log()


if __name__ == '__main__':
    main()
