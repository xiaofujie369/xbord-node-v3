# AnyTLS + REALITY implementation report

## Status

Node-side implementation and local integration pass. **Phase 1 is NOT marked Complete**: real Xboard user-sync/reporting acceptance remains pending, and the audited stock panel cannot emit AnyTLS `tls=2`. No Phase 2/3 changes were made.

## Baselines and scope

- Xboard-Node: `0a29338e1f102a462363ce3527417029f89bab28`, latest origin/dev when cloned on 2026-09-25 (Asia/Shanghai).
- Reference templates: `be937d4db2c4e5ab8d6361c5d8ad51ca53f20f38`, matching the requested audit baseline.
- Read-only Xboard API audit: `4f48e61`; AnyTLS mapping in `app/Services/ServerService.php:338` does not include integer tls mode. The model uses an object-valued protocol_settings.tls. Merely supplying REALITY keys there cannot select the node's REALITY builder.
- Branch: `codex/anytls-reality`.
- Effective embedded runtime: existing go.mod replacement `github.com/cedar2025/sing-box v1.14.0-alpha.2.0.20260316103356-2e665cb7e295`, not the nominal require version v1.13.2. Dependencies were not modified.

## Changed files and functions

| File | Change |
| --- | --- |
| `internal/kernel/singbox/config.go` | `buildAnyTLS` calls existing `buildRealityConfig` for TLS=2. Existing UUID name/password, padding and certificate behavior retained. Incorrect mandatory-TLS warning removed. |
| `internal/service/service.go` | `validateTLSRequirements` requires certificates for AnyTLS TLS=1, permits REALITY and optional-TLS inbound. Shared `validateRealityRequirements` rejects malformed destinations/ports, unsupported sing-box IPv6 targets and malformed short IDs before update. |
| `internal/kernel/singbox/config_test.go` | REALITY/string/array, certificate/legacy/no-TLS, authentication, default padding, per-node independence and sanitized generated-config coverage. |
| `internal/kernel/singbox/anytls_integration_test.go` | Real embedded server/native client tests behind `with_utls`; disposable local handshake target and keys. |
| `internal/service/service_test.go` | Shared protocol validation matrix, TLS modes, all four REALITY hash fields, rejected WS/poll update preserves active config/hash and avoids kernel calls. |
| `internal/controlplane/panel_test.go` | Synthetic REST initial/poll and WS mapping preserve AnyTLS mode/settings; separate maps avoid cross-node key mutation. |
| `IMPLEMENTATION_PLAN.md` | Audit and plan written before production edits, with dependency/runtime findings appended. |
| `docs/ANYTLS_REALITY_TEST.md` | Reproducible local/manual integration and remaining panel gate. |
| `examples/anytls-reality-client.json` | Native client template with placeholders and public-key-only REALITY configuration. |
| `IMPLEMENTATION_REPORT.md` | This evidence and limitations record. |

Production diff is two files: 62 inserted lines, 5 removed. `buildRealityConfig`, model/config schema, hash implementation, synchronizers, routes, limiters, tracker and upstream libraries remain unchanged.

## Tests and execution evidence

Environment: WSL2 Alpine 3.22, Linux amd64, Go 1.26.0 selected using GOTOOLCHAIN=auto. Tests use the repository's exact dependency replacements. Formatting-only changes to unrelated upstream files from `go fmt ./...` were restored.

| Command/check | Result |
| --- | --- |
| `go fmt ./...` and targeted final gofmt | PASS; changed Go files formatted |
| `go vet ./...` | PASS |
| `go test ./...` | PASS |
| `make test` (`go test -v -race -count=1 ./internal/...`) | PASS |
| `make lint` | Not applicable: no lint target exists |
| `go test -tags 'with_quic with_utls with_wireguard with_acme with_clash_api' ./...` | PASS, including local integration |
| `go build -tags 'with_quic with_utls with_wireguard with_acme with_clash_api' -o /tmp/anytls-xboard-node ./cmd/xboard-node` | PASS |
| `go test -tags with_utls ./internal/kernel/singbox -run 'TestAnyTLS.*(RoundTrip\|Runtime)' -count=1 -v` | PASS: REALITY 5.95s, standard TLS 5.81s, no-TLS startup |
| Additional tagged integration with `-race` | PASS, package result 12.875s |
| `git diff --check` | PASS |

The initial pre-feature AnyTLS tests failed with `missing REALITY` and `missing TLS`, proving the builder gap. A concurrently running initial service compile was invalidated by source edits; that initial service result is not counted as a valid red-test result. Subsequent complete test runs passed.

The real local round-trip tests assert:

- A native sing-box client forwards an exact 26 KiB payload through the generated AnyTLS inbound, in both REALITY and certificate TLS modes.
- Actual per-user upload/download counters increase, and the active source IP is visible while the request is alive.
- Replacing users allows the new password and rejects the old one.
- A 32 KiB/s limiter with its burst exhausted delays the transfer; the burst is large enough for the existing zero-copy counter path.
- A fresh global device entry consumes the device slot; a different source IP is rejected after prior streams close. Clearing global devices restores access.
- Reload rebuilds the inbound on the same port and the client reconnects successfully.
- TLS=0 without certificates starts successfully in the pinned server runtime.

Existing VLESS REALITY, VLESS short-ID-array and Trojan REALITY builder tests pass. Existing panel traffic-push, user/limiter/tracker tests pass. The REST/WS fixture is synthetic and does not prove stock PHP delivery.

Actual standalone binary smoke log (temporary key/config removed; no private key logged):

```text
initial snapshot ready protocol=anytls port=39443 users=1
[anytls:39443] started, 1 users
received terminated, shutting down...
stopped
```

## Actual generated inbound

Captured by `TestBuildInbound_AnyTLS_Reality_GeneratedConfig`; only password/private_key are masked before logging:

```json
{
  "listen": "::",
  "listen_port": 443,
  "tag": "anytls-in",
  "tls": {
    "enabled": true,
    "reality": {
      "enabled": true,
      "handshake": {
        "server": "example.com",
        "server_port": 443
      },
      "private_key": "***",
      "short_id": ["0123456789abcdef"]
    },
    "server_name": "example.com"
  },
  "type": "anytls",
  "users": [{"name": "test-user", "password": "***"}]
}
```

## Acceptance matrix

| Requirement | Evidence/status |
| --- | --- |
| AnyTLS basic / standard TLS / REALITY | PASS unit and real local runtime |
| short ID string / array | PASS, including array on runtime reload |
| Shared REALITY validation | PASS, AnyTLS/VLESS/Trojan |
| VLESS / Trojan regressions | PASS |
| User sync | PASS local kernel replacement and synthetic REST/WS mapping; real panel PENDING |
| Traffic report | PASS local counters and existing mocked API tests; real panel PENDING |
| Speed / device limits | PASS local connection tests; deployed panel settings PENDING |
| Online IP | PASS during a live local request; panel display PENDING |
| Config reload / hash / invalid-update rejection | PASS local runtime and service tests; real panel push PENDING |
| Independent nodes | PASS builder isolation; real multi-node deployment PENDING |

## Known limitations and compatibility decisions

1. Native panel mode delivery needs later Xboard changes. No real panel credentials/environment were supplied. No real panel acceptance is claimed, and Phase 2 remains unstarted per the requested gate.
2. Preserve legacy TLS=0 plus certificate behavior because current AnyTLS panel responses omit the integer mode. Explicit TLS=1 works; TLS=0 without certificates starts a plaintext server inbound. Native sing-box AnyTLS outbound still requires TLS, so this is not a claim of a plaintext native-client pairing.
3. Preserve existing `server_name` destination fallback rather than requiring dest unconditionally. Sing-box's existing first-colon builder cannot express IPv6 correctly; reject it before reload and leave the shared builder unchanged.
4. Early validation handles missing settings/key, malformed destination and malformed short IDs. It does not fully validate cryptographic key bytes or target reachability. Existing inbound recreation is not transactional; bind failures or other unvalidated runtime failures can still affect the active listener. A broader rollback redesign was not added to this feature.
5. Tests do not certify Mihomo, subscriptions or share URIs. The client example has placeholders and requires a matching runtime with REALITY/uTLS support.
6. Limit testing covers the existing normal burst configuration. It does not establish correct behavior for arbitrarily small bursts below a zero-copy chunk size; no limiter rewrite was made.

## Rollback and commits

Local commits are separated into audit (`997e8bc`), tests (`aa579e6`), feature/shared validation (`fc4d1dd`), and local integration evidence (`69b3739`), followed by documentation. No remote push or PR was created.

To roll back a deployment, restore the previous binary and known-good node config; revert the feature commit in the development branch if needed. No schema migration, key replacement, or database rollback is required. Keep private keys outside version control.

## Numbered task acceptance audit

Rechecked against the original task document after implementation. PASS below is scoped to the evidence stated; it does not promote mocked or local checks to deployed panel acceptance.

| Task sections | Current evidence and disposition |
| --- | --- |
| 0–4 | PASS: exact node/reference commits inspected; existing builders retained; both reference templates read. |
| 5 | PASS locally: REALITY and certificate client round trips, optional-TLS server startup. Legacy omitted-mode certificate behavior is explicitly preserved. |
| 6–8 | PASS audit: implementation plan predates code; model, panel, controlplane and service paths inspected. Real stock AnyTLS API cannot emit mode 2. |
| 9–14 | PASS: existing builder reuse, UUID authentication, unchanged panel padding, string/array short IDs and original fields; generated JSON captured. |
| 15–18 | PASS: all four requested named AnyTLS builder tests exist and pass, plus short-ID-array test. |
| 19–21 | PASS: shared runtime validation accepts certificate-free REALITY; rejects missing settings/key/target, malformed destination and short IDs. Existing server_name fallback retained. |
| 22–24 | PASS: native client example and integration guide supplied; no subscription implementation. |
| 25 | INCOMPLETE: local checks and binary startup pass, but real Xboard user sync/reporting/limits/online-IP acceptance is missing. |
| 26–30 | GATED: read-only mapping audit done; native Xboard model/API/admin UI/key-generator work is Phase 2 and has not begun. |
| 31–33 | GATED: Phase 3 subscription generation has not begun. Native JSON test client exists; no unverified Mihomo or URI support claimed. |
| 34–36 | PASS: no upstream dependency/library edit, copied installer, second production synchronizer or external runtime layer. |
| 37–38 | PASS configuration isolation tests; existing per-node architecture retained. Concurrent deployed multi-node acceptance remains unverified. |
| 39 | PASS: complete NodeSpec hash retained; tests cover all four REALITY settings. |
| 40 | PASS for specified missing-key/malformed-target/short-ID rejection; invalid WS/poll update preserves config/hash before kernel call. Existing general nontransactional reload limitation remains disclosed. |
| 41 | PASS change review: new validation messages do not include secret values; generated report masks private key/password; client has no private key. |
| 42–44 | PASS: existing VLESS/Trojan REALITY regression tests and standard AnyTLS unit/real client tests. |
| 45–47 | PASS: small separate commits, two production files, no architecture refactor. |
| 48 | PASS: IMPLEMENTATION_PLAN.md created and committed before production edits, covering all ten requested topics. |
| 49 | PASS: formatting, vet, full tests, make test, production-tag tests/build; no make lint target exists. |
| 50–51 | PASS: report includes base, files/functions, tests, real generated masked config, regression results and limitations. |
| 52 | INCOMPLETE: the full PASS checklist includes real panel gates, so Phase 1 Complete is intentionally not claimed. |
| 53–54 | GATED: later Xboard native configuration and subscription tasks await Phase 1 acceptance. |
| 55 | DEFERRED as explicitly optional Phase 4 work. |
| 56–57 | PASS: cloned latest node first, inspected reference, planned before editing, reused existing architecture and builders. |

Blocking prerequisite: a compatible isolated Xboard test environment must deliver AnyTLS TLS=2 to finish the real-panel gates. The audited stock API lacks that behavior. Changing it now would enter Phase 2 before sections 25/52 permit it. The pending environment question therefore concerns a concrete missing prerequisite, not an additional approval requirement inferred from a skill. No further production-code change can by itself establish the missing panel acceptance evidence.
