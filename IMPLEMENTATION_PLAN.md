# AnyTLS + REALITY — Phase 1 implementation plan

## Audit baseline
- Xboard-Node origin/dev: `0a29338e1f102a462363ce3527417029f89bab28` (latest fetched 2026-09-25); no difference from requested baseline.
- anyreality-resi-stack origin/main: `be937d4db2c4e5ab8d6361c5d8ad51ca53f20f38`; both referenced templates inspected. No installer reuse.
- Read-only Xboard API reference: `4f48e61` (latest default branch fetched). No Xboard changes in Phase 1.
- Audited singbox config/tests, service/tests, model conversion/validation, panel types/client/WS, controlplane panel/machine/mailbox, config/standalone and Makefile. No AGENTS.md in node repository.

## 1. Current AnyTLS builder
`internal/kernel/singbox/config.go:720`, complete pre-change implementation:
```go
func buildAnyTLS(base M, nc *model.NodeSpec, users []model.UserSpec, tc kernel.TLSCert) M {
    base["type"] = "anytls"
    userList := make([]M, 0, len(users))
    for _, u := range users {
        userList = append(userList, M{"name": u.UUID, "password": u.UUID})
    }
    base["users"] = userList
    if nc.PaddingScheme != "" {
        base["padding_scheme"] = nc.PaddingScheme
    }
    if tls := buildTLSConfig(nc, tc); tls != nil {
        base["tls"] = tls
    } else {
        nlog.Core().Warn("anytls requires TLS certificate files on disk; configure cert_mode (self, file, http, dns, or content). Sing-box will not start this inbound without tls.")
    }
    return base
}
```
It calls buildTLSConfig without checking nc.TLS. Existing panel AnyTLS omits TLS mode, so TLS=0 plus certificate is a legacy standard-TLS path and must remain compatible. TLS=0 without certificates emits no TLS block; verify the pinned sing-box runtime rejects plaintext AnyTLS before claiming support.

## 2. Shared REALITY builder
`internal/kernel/singbox/config.go:889`: buildRealityConfig is protocol-independent and reused unchanged. It accepts short_id string, []any and []string; dest falls back to server_name, port defaults to 443. Its colon split does not support IPv6 destinations correctly. No parallel builder or extra model fields.

## 3. Runtime validation
`internal/service/service.go:1154,1173,1233`: validateNodeRuntime -> validateTLSRequirements -> validateRealityRequirements. The latter has no protocol whitelist, but AnyTLS currently fails an earlier mandatory-certificate check even with TLS=2. Move AnyTLS into the same certificate exemption branch as Trojan. Existing validation only checks nonempty private_key and server_name OR dest. Add shared destination/short-ID format validation for safe updates, preserving documented server_name fallback and existing VLESS/Trojan behavior. Do not log secret values. Test malformed fields and rejected updates before kernel restart.

## 4. Full configuration path
REST UniProxyController -> Xboard ServerService::buildNodeConfig -> panel.Client.GetConfig JSON decode -> panel.NodeConfig.TLS/TLSSettings -> controlplane.PanelControlPlane.Initial/Poll -> model.NodeSpecFromPanelValidated -> NodeSpecFromPanel (copies TLS and clones TLSSettings) -> NodeSpec -> service runtime validation -> singbox buildInbound -> buildAnyTLS -> buildRealityConfig.
WS config events decode the same NodeConfig -> TranslateWSEvent -> same validated conversion. Machine controlplane and mailbox preserve per-node fields; no global TLS switch. Standalone config already exposes tls and tls_settings and uses NodeSpecFromStandalone.
computeConfigHash JSON-marshals the entire NodeSpec, including TLSSettings. Keep implementation unchanged; add regression coverage for key/short-ID/destination/SNI changes. WS and REST validate before replacing lastConfig/hash and applying changes; reuse these paths.

## 5. Does Xboard need changes?
YES for native AnyTLS REALITY delivery, but only in a later phase. Actual `app/Services/ServerService.php:338` returns server_name, tls_settings from protocol_settings.tls and padding_scheme; it does NOT return integer tls=2. `app/Models/Server.php:290` defines AnyTLS tls as TLS_CONFIGURATION object, unlike VLESS/Trojan integer mode plus reality_settings. Node-side decoding can preserve supplied TLS=2, but current stock panel cannot emit it for AnyTLS. Phase 1 can use existing standalone mode and synthetic panel fixtures; a real panel acceptance gate remains pending. Do not silently mark it passed or modify PHP/UI/database now.

## 6. Minimal change set
- internal/kernel/singbox/config.go: TLS=2 reuse; preserve certificate fallback and padding semantics (check runtime array encoding).
- internal/kernel/singbox/config_test.go: requested AnyTLS REALITY/string/array/standard/no-certificate tests, auth/padding/isolation regression.
- internal/service/service.go: certificate exemption, shared validation only where audit proves necessary.
- internal/service/service_test.go: valid/invalid AnyTLS runtime, config hash, last-known-good update checks.
- Focused model/controlplane tests if needed to prove API propagation.
- IMPLEMENTATION_PLAN.md, IMPLEMENTATION_REPORT.md, docs/ANYTLS_REALITY_TEST.md and examples/anytls-reality-client.json.

## 7. Risks
- Stock Xboard cannot provide the mode: end-to-end production acceptance blocked until later panel work is permitted.
- Go 1.26 and embedded sing-box 1.13.2 build environment required; Windows PATH currently has no Go/make. Use isolated local toolchain or existing WSL without changing production services.
- AnyTLS plaintext may be unsupported. Preserve implicit standard TLS compatibility.
- Existing padding string vs sing-box array decoding needs runtime verification.
- Shared format validation must preserve valid existing configurations and never expose credentials.
- Unit tests do not establish live traffic, online IP, device/rate limits or real Xboard reporting acceptance.

## 8. Test strategy
First add failing focused tests; then implement minimal changes. Run go fmt, go vet ./..., go test ./..., make test (race); Makefile has no lint target. Run existing VLESS/Trojan REALITY regressions and build with production tags. Verify actual generated config through pinned sing-box, start a local server/client if feasible, and document reproducible external integration steps. Test user authentication, panel padding preservation/default omission, per-node isolation, all REALITY hash fields and rejecting bad WS/REST updates. Record actual commands/results and distinguish live/mocked/manual checks.

## 9. Rollback
All changes isolated on codex/anytls-reality. Revert feature/validation commits and return to baseline, then deploy previous binary and known-good node config. Do not delete certificates or keys. No schema migrations. Existing invalid-update rejection keeps active config; integration testing must verify it.

## 10. Expected generated inbound (sanitized)
```json
{"type":"anytls","tag":"anytls-in","listen":"::","listen_port":443,"users":[{"name":"test-user","password":"***"}],"tls":{"enabled":true,"server_name":"example.com","reality":{"enabled":true,"handshake":{"server":"example.com","server_port":443},"private_key":"***","short_id":["0123456789abcdef"]}}}
```

Phase 1 Complete requires every requested acceptance gate. Phase 2/3 must not start before that gate. This plan precedes implementation edits.
