#!/usr/bin/env python3
"""Generate an opt-in Go test overlay; never modify the installed toolchain."""
from pathlib import Path
import argparse
import hashlib
import json
import os
import subprocess

parser = argparse.ArgumentParser()
parser.add_argument("--output", type=Path, required=True)
args = parser.parse_args()
version = subprocess.check_output(["go", "env", "GOVERSION"], text=True).strip()
if version != "go1.27.2":
    raise SystemExit("offline guard requires reviewed Go 1.27.2, found " + version)
goroot = Path(subprocess.check_output(["go", "env", "GOROOT"], text=True).strip())
root = goroot / "src" / "net"
out = args.output.resolve()
if out.is_relative_to(goroot.resolve()):
    raise SystemExit("overlay output must not modify the installed toolchain")
out.mkdir(parents=True, exist_ok=True)
expected_sha256 = {'dial.go': '1ea8719962e33747db841fae362a339c29315fde2c5554a344b2935aa6f803b5', 'lookup.go': 'e8b6b0a0dc7ca0715fe14e6599b1784dc85ce59689e0cb3f9a7fe3eea26ada19', 'tcpsock_posix.go': '01136ba949dd7d7f70511d514ec12a89a22c216457b2ab6220c16387bb62dbed', 'udpsock_posix.go': '374963d9933fc3447905a2958cd0179a3559ea0b4016bb05deac5b26dff629c3', 'iprawsock_posix.go': 'f2ef8c16772021695b2380ba5a3dde6e4a16c953ec0236b7a92d3619483c811c', 'sock_posix.go': '0f45c21302132c84686b3d64b5bc5b64764e42447ec229cfca609491cccc2d1b', 'http/transport.go': 'c9146e77adc22d4b74ba77384ec7be7fc27c3af21ff20485f642b4d611b381a4'}
# Check every input before generating anything. Toolchain changes require review.
for name, expected in expected_sha256.items():
    actual = hashlib.sha256((root / name).read_bytes()).hexdigest()
    if actual != expected:
        raise SystemExit("unreviewed Go source: " + name)
replace = {}
def edit(name, edits, append=''):
    src = (root/name).read_text(encoding='utf-8')
    if name == 'dial.go':
        assert src.count('\t"net/netip"') == 1
        src = src.replace('\t"net/netip"', '\t"net/netip"\n\t"net/url"\n\t"os"\n\t"strings"', 1)
    if name == 'http/transport.go':
        assert src.count('\t"net/url"') == 1
        src = src.replace('\t"net/url"', '\t"net/url"\n\t"os"', 1)
    for sig, body in edits:
        assert src.count(sig) == 1, (name,sig,src.count(sig))
        src = src.replace(sig, sig+'\n'+body,1)
    dst = out/name
    dst.parent.mkdir(parents=True, exist_ok=True)
    dst.write_text(src+append, encoding='utf-8', newline='\n')
    replace[str(root/name)] = str(dst)
helper = '''
// offline279GuardHost is a local verification-only guard. No repository source uses it.
func offline279GuardHost(host string) bool {
    if host == "localhost" || host == "localhost." { return true }
    if ip, err := netip.ParseAddr(host); err == nil { return ip.IsLoopback() }
    return false
}
func offline279InfrastructureProxy(address string) bool {
    host, port, err := SplitHostPort(address)
    if err != nil { return false }
    values := strings.Split(os.Getenv("OFFLINE279_BLOCKED_PROXY_URLS"), ",")
    for _, key := range []string{"HTTP_PROXY", "HTTPS_PROXY", "ALL_PROXY", "http_proxy", "https_proxy", "all_proxy"} {
        values = append(values, os.Getenv(key))
    }
    for _, raw := range values {
        u, err := url.Parse(raw)
        if err != nil || u.Host == "" { continue }
        proxyHost, proxyPort, err := SplitHostPort(u.Host)
        if err != nil { continue }
        if port == proxyPort && (host == proxyHost || (offline279GuardHost(host) && offline279GuardHost(proxyHost))) { return true }
    }
    return false
}
func offline279Blocked(addr string) error {
    // One small O_APPEND write per blocked operation is safe across test processes.
    // Never log the address: it may contain credentials, tokens, or query data.
    const marker = "OFFLINE279_BLOCKED\\n"
    _, _ = os.Stderr.WriteString(marker)
    if path := os.Getenv("OFFLINE279_AUDIT_LOG"); path != "" {
        f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0600)
        if err != nil { panic("offline279: cannot open required audit log") }
        _, err = f.WriteString(marker)
        closeErr := f.Close()
        if err != nil || closeErr != nil { panic("offline279: cannot append required audit log") }
    } else { panic("offline279: required audit log is unset") }
    return &AddrError{Err: "offline279: external network blocked before DNS/connect", Addr: addr}
}
'''
edit('dial.go',[(
'func (d *Dialer) DialContext(ctx context.Context, network, address string) (Conn, error) {',
'''    if offline279InfrastructureProxy(address) { return nil, offline279Blocked(address) }
    switch network {
    case "unix", "unixgram", "unixpacket":
    default:
        host, _, err := SplitHostPort(address)
        if err != nil { host = address }
        if !offline279GuardHost(host) { return nil, offline279Blocked(address) }
    }''')],helper)
edit('lookup.go',[
('func (r *Resolver) LookupHost(ctx context.Context, host string) (addrs []string, err error) {','''    if host == "localhost" || host == "localhost." { return []string{"127.0.0.1", "::1"}, nil }
    if _, err := netip.ParseAddr(host); err != nil { return nil, offline279Blocked(host) }'''),
('func (r *Resolver) lookupIPAddr(ctx context.Context, network, host string) ([]IPAddr, error) {','''    if host == "localhost" || host == "localhost." {
        if network == "ip4" { return []IPAddr{{IP: IPv4(127, 0, 0, 1)}}, nil }
        if network == "ip6" { return []IPAddr{{IP: IPv6loopback}}, nil }
        return []IPAddr{{IP: IPv4(127, 0, 0, 1)}, {IP: IPv6loopback}}, nil
    }
    if _, err := netip.ParseAddr(host); err != nil { return nil, offline279Blocked(host) }'''),
('func (r *Resolver) LookupCNAME(ctx context.Context, host string) (string, error) {','    if true { return "", offline279Blocked(host) }'),
('func (r *Resolver) LookupSRV(ctx context.Context, service, proto, name string) (string, []*SRV, error) {','    if true { return "", nil, offline279Blocked(name) }'),
('func (r *Resolver) LookupMX(ctx context.Context, name string) ([]*MX, error) {','    if true { return nil, offline279Blocked(name) }'),
('func (r *Resolver) LookupNS(ctx context.Context, name string) ([]*NS, error) {','    if true { return nil, offline279Blocked(name) }'),
('func (r *Resolver) LookupTXT(ctx context.Context, name string) ([]string, error) {','    if true { return nil, offline279Blocked(name) }'),
('func (r *Resolver) LookupAddr(ctx context.Context, addr string) ([]string, error) {','    if true { return nil, offline279Blocked(addr) }'),
])
edit('tcpsock_posix.go',[(
'func (sd *sysDialer) dialTCP(ctx context.Context, laddr, raddr *TCPAddr) (*TCPConn, error) {',
'    if raddr != nil && !raddr.IP.IsLoopback() { return nil, offline279Blocked(raddr.String()) }')])
edit('udpsock_posix.go',[
('func (sd *sysDialer) dialUDP(ctx context.Context, laddr, raddr *UDPAddr) (*UDPConn, error) {','    if raddr != nil && !raddr.IP.IsLoopback() { return nil, offline279Blocked(raddr.String()) }'),
('func (c *UDPConn) writeTo(b []byte, addr *UDPAddr) (int, error) {','    if addr != nil && !addr.IP.IsLoopback() { return 0, offline279Blocked(addr.String()) }'),
('func (c *UDPConn) writeToAddrPort(b []byte, addr netip.AddrPort) (int, error) {','    if addr.IsValid() && !addr.Addr().IsLoopback() { return 0, offline279Blocked(addr.String()) }'),
('func (c *UDPConn) writeMsg(b, oob []byte, addr *UDPAddr) (n, oobn int, err error) {','    if addr != nil && !addr.IP.IsLoopback() { return 0, 0, offline279Blocked(addr.String()) }'),
('func (c *UDPConn) writeMsgAddrPort(b, oob []byte, addr netip.AddrPort) (n, oobn int, err error) {','    if addr.IsValid() && !addr.Addr().IsLoopback() { return 0, 0, offline279Blocked(addr.String()) }'),
])
edit('iprawsock_posix.go',[
('func (sd *sysDialer) dialIP(ctx context.Context, laddr, raddr *IPAddr) (*IPConn, error) {','    if raddr != nil && !raddr.IP.IsLoopback() { return nil, offline279Blocked(raddr.String()) }'),
('func (c *IPConn) writeTo(b []byte, addr *IPAddr) (int, error) {','    if addr != nil && !addr.IP.IsLoopback() { return 0, offline279Blocked(addr.String()) }'),
('func (c *IPConn) writeMsg(b, oob []byte, addr *IPAddr) (n, oobn int, err error) {','    if addr != nil && !addr.IP.IsLoopback() { return 0, 0, offline279Blocked(addr.String()) }'),
])
edit('sock_posix.go',[(
'func (fd *netFD) dial(ctx context.Context, laddr, raddr sockaddr, ctrlCtxFn func(context.Context, string, string, syscall.RawConn) error) error {',
'''    if raddr != nil && offline279InfrastructureProxy(raddr.String()) { return offline279Blocked(raddr.String()) }
    switch addr := raddr.(type) {
    case *TCPAddr:
        if addr != nil && !addr.IP.IsLoopback() { return offline279Blocked(addr.String()) }
    case *UDPAddr:
        if addr != nil && !addr.IP.IsLoopback() { return offline279Blocked(addr.String()) }
    case *IPAddr:
        if addr != nil && !addr.IP.IsLoopback() { return offline279Blocked(addr.String()) }
    }''')])
edit('http/transport.go',[(
'func (t *Transport) roundTrip(req *Request) (_ *Response, err error) {',
'''    // Check the ultimate HTTP destination before invoking any proxy selector.
    // Custom in-memory RoundTrippers remain unaffected; a real transport may
    // only target loopback fixtures, even through a local HTTP/SOCKS proxy.
    if req.URL != nil && (req.URL.Scheme == "http" || req.URL.Scheme == "https") {
        host := req.URL.Hostname()
        ip := net.ParseIP(host)
        if !strings.EqualFold(host, "localhost") && !strings.EqualFold(host, "localhost.") && (ip == nil || !ip.IsLoopback()) {
            req.closeBody()
            return nil, offline279HTTPBlocked()
        }
    }''')],'''
func offline279HTTPBlocked() error {
    const marker = "OFFLINE279_BLOCKED_HTTP_TARGET\\n"
    _, _ = os.Stderr.WriteString(marker)
    if path := os.Getenv("OFFLINE279_AUDIT_LOG"); path != "" {
        f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0600)
        if err != nil { panic("offline279: cannot open required audit log") }
        _, err = f.WriteString(marker)
        closeErr := f.Close()
        if err != nil || closeErr != nil { panic("offline279: cannot append required audit log") }
    } else { panic("offline279: required audit log is unset") }
    return errors.New("offline279: external HTTP target blocked before proxy/DNS/connect")
}
''')
gofmt = goroot / "bin" / ("gofmt.exe" if os.name == "nt" else "gofmt")
subprocess.run([str(gofmt), "-w", *replace.values()], check=True)
(out / "overlay.json").write_text(json.dumps({"Replace": replace}, indent=2) + "\n", encoding="utf-8")
manifest = {
    "go_version": version,
    "source_sha256": expected_sha256,
    "overlay_sha256": {name: hashlib.sha256((out / name).read_bytes()).hexdigest() for name in expected_sha256},
}
(out / "manifest.json").write_text(json.dumps(manifest, indent=2) + "\n", encoding="utf-8")
print("Generated", len(replace), "verified overlays in", out)
