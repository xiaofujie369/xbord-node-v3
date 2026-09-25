# AnyTLS REALITY — Phase 2

The user explicitly authorized entering Phase 2 before the original Phase 1 real-panel gate, to resolve the API dependency. Baseline: `4f48e61a2cbc6db5338872b6bdb45ef954ec1256`; branch `codex/anytls-reality-panel`. The previously read-only Xboard checkout is now the Phase 2 development checkout.

## Audit findings

- `Server::PROTOCOL_CONFIGURATIONS` defines AnyTLS `tls` as an object. VLESS/Trojan use integer `tls`, `tls_settings` and shared `REALITY_CONFIGURATION` under `reality_settings`.
- `getProtocolSettingsAttribute` and `setProtocolSettingsAttribute` discard fields absent from the schema. Adding API output alone cannot retain REALITY keys.
- `ServerSave` validates AnyTLS tls as an array; reuse standard TLS/REALITY rules and normalize legacy object-valued input before validation.
- `ServerService::buildNodeConfig` is the shared node-config producer. AnyTLS currently emits no tls integer. Match VLESS/Trojan: TLS=2 maps reality_settings to wire tls_settings.
- Existing REALITY schema has server_name/server_port but no dest. Add dest to the shared configuration/rules, retaining fallback fields.
- `ServerService::getAvailableServers` feeds client subscriptions. `NodeResource` exposes only status metadata, but client-bound protocol settings must remove the private key before formatters/plugins consume them.
- Existing AnyTLS subscription encoders expect the legacy tls object. Preserve their standard-TLS input shape through a client compatibility projection; REALITY subscription generation is a later phase, so do not emit a misleading ordinary-TLS profile for mode 2.
- Admin assets are a submodule pointing to `cedar2025/xboard-admin-dist`; fetched commit `ef5f43d` contains only minified build output, no application source. A source location has been requested. Do not hand-edit the minified bundle as a substitute for maintainable UI work.
- Existing frontend REALITY key generation must be located and reused once source is available. No AnyTLS-specific generator.

## Implementation and verification

1. Normalize legacy AnyTLS tls objects to mode 1 plus tls_settings in model accessors and admin validation, without database migration or rewriting existing rows.
2. Add canonical integer mode (0/1/2), tls_settings and shared reality_settings to AnyTLS. Validate mode-specific fields, key pair and destination/short ID for REALITY.
3. Extend shared node API mapping; preserve padding, user model and sync pipelines.
4. Strip server-only secret fields from client settings and test client/status endpoints. Keep standard AnyTLS subscriptions compatible; defer REALITY profiles until Phase 3.
5. Add focused PHP model/request/API regression tests, run them using the existing locked Laravel/PHP dependencies and an isolated SQLite test database.
6. Implement frontend mode selection and conditional shared settings using original frontend source when supplied. Verify in browser.
7. Use the user's panel for read-only version/config inspection after they log in. Test-node mutations depend on their confirmation that independent test nodes are allowed. No live deployment is implied by login access.

## Acceptance and rollback

Phase 2 requires model persistence, admin validation/UI, node config delivery, private-key separation and preserved standard TLS. Record each as completed or pending with direct evidence. Reuse the Phase 1 node for actual traffic/sync/report verification. Roll back by reverting panel code and restoring the prior frontend build; no schema migration is planned. Existing legacy rows remain readable. New REALITY rows require the updated node and panel.
