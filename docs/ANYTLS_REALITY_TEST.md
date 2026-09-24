# AnyTLS + REALITY integration guide

## Scope

Phase 1 modifies Xboard-Node only. The audited stock Xboard AnyTLS API does not emit integer `tls=2`; this is a prerequisite for later panel integration. Use existing standalone mode or a controlled test API returning this payload. No database or subscription changes are needed for the local test.

Use Go 1.26 and existing go.mod dependencies, including the existing sing-box replacement. REALITY requires `with_utls`. No upstream fork or library patch is introduced.

## Automated local integration

```sh
go test ./...
go vet ./...
make test
go test -tags with_utls ./internal/kernel/singbox -run 'TestAnyTLS.*(RoundTrip|Runtime)' -count=1 -v
```

The tagged test creates temporary X25519 keys, a local TLS 1.3 handshake destination, an embedded node kernel and a native sing-box client. It checks payload forwarding, certificate TLS, user replacement, traffic counters, active IP tracking, rate/device limits and same-port reload. A test-only custom route allows the local echo fixture; production SSRF blocks remain unchanged. No external server or credentials are required. TLS=0 inbound startup is separately tested; native AnyTLS outbound still requires TLS.

## Manual node test

Build with `make build`, or the production tags `with_quic with_utls with_wireguard with_acme with_clash_api`. Generate disposable keys with the matching sing-box CLI (`sing-box generate reality-keypair`). Keep the private key in a private temporary configuration file. Choose a reachable TLS 1.3 handshake target whose certificate matches your test server name.

Existing standalone YAML:

```yaml
kernel:
  type: singbox
cert:
  cert_mode: none
standalone:
  enabled: true
  node:
    protocol: anytls
    server_port: 9443
    tls: 2
    tls_settings:
      private_key: REPLACE_WITH_DISPOSABLE_PRIVATE_KEY
      short_id: "0123456789abcdef"
      dest: TEST_SERVER_NAME:443
      server_name: TEST_SERVER_NAME
  users:
    - id: 1
      uuid: REPLACE_WITH_TEST_USER_UUID
```

Run with `xboard-node -c /path/to/test.yml`. Keep ports distinct from other nodes. Quote all-digit short IDs in YAML so they remain strings.

## Native client

Copy `examples/anytls-reality-client.json` to a private temporary file. Replace NODE_IP, USER_UUID, TEST_SERVER_NAME and REALITY_PUBLIC_KEY. Change server_port to 9443 for the example above. The password is the user's UUID; the public key must match the server's private key. Select one allowed short ID. Never include a private key in client JSON.

```sh
sing-box check -c /path/to/client.json
sing-box run -c /path/to/client.json
curl --proxy http://127.0.0.1:1080 https://example.com/
```

This is native sing-box JSON. No Mihomo profile or share URI is claimed. Placeholders must be filled in before runtime key validation succeeds.

## Modes and compatibility

- TLS=2: REALITY wins even when old certificate material exists.
- TLS=1: certificate TLS; missing certificates/configuration is rejected by service validation.
- TLS=0 without certificates: optional-TLS inbound supported by the pinned runtime. Native sing-box AnyTLS outbound cannot connect plaintext directly; an appropriate TLS offload arrangement is needed.
- TLS=0 with certificates: preserves the existing panel's implicit standard TLS behavior because that panel omits the mode.
- Padding remains panel-supplied; absent padding retains runtime defaults. The runtime accepts existing multiline strings through its Listable type.
- Missing dest may use the existing server_name fallback, default port 443. Malformed targets and short IDs are rejected before WS/poll updates. IPv6 destinations are rejected for sing-box because its existing builder cannot split them correctly.

## Full panel acceptance gate

Once a compatible panel emits tls=2 and tls_settings for AnyTLS, verify both REST polling and WebSocket push on a real test node:

1. Add/remove users; new credentials connect and removed credentials fail.
2. Transfer known bytes in both directions and observe panel traffic reports.
3. Confirm online IP visibility during an active connection.
4. Measure a long transfer beyond the user's configured burst allowance.
5. Connect from distinct source IPs and verify device/global-device limits.
6. Change private_key, short_id, dest and server_name independently; confirm reload and update client fields.
7. Push missing private_key, invalid dest or malformed short_id; the running config and hash must remain intact.
8. Run VLESS REALITY, AnyTLS certificate TLS and AnyTLS REALITY on separate node ports with independent settings.

Do not mark Phase 1 Complete until these gates pass. Standalone mode does not report to Xboard. Existing inbound recreation is nontransactional; early validation cannot guarantee rollback from every bind/resource/runtime error.
