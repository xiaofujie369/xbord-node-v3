# Phase 2 — panel deployed; runtime acceptance remains open

Update: native sing-box REALITY subscription support was subsequently implemented; see `PHASE3_IMPLEMENTATION_REPORT.md`. The subscription restrictions described below record the Phase 2 state and are superseded for explicit native sing-box requests only.

## Implemented

- AnyTLS uses the existing integer TLS mode, standard `tls_settings` and shared `reality_settings`. Legacy object-valued `tls` is normalized on read/write and admin input, without rewriting old rows or adding a database migration.
- The authenticated node API emits mode 2 and maps the existing REALITY settings into `tls_settings`, including destination, per-node keys and short ID. Padding and UUID authentication are preserved.
- Admin save validates REALITY required fields, X25519 key pairing, destination port and short ID. Invalid requests do not overwrite the stored node.
- Certificate provisioning settings are omitted for AnyTLS modes 0 and 2.
- Client-bound nodes remove the REALITY private key before subscription plugins/formatters. Existing standard-TLS encoders receive their legacy object shape. Plaintext and REALITY AnyTLS nodes are excluded from these encoders until their respective client capabilities are implemented; this is not Phase 3 subscription support.
- Admin audit logging recursively removes sensitive fields, including nested REALITY private keys. An actual admin-save regression test checks that the public key remains useful for auditing while the private key is absent.

## Verification

PHP 8.4.16, locked Composer dependencies, PHPUnit 11.5.27:

```sh
APP_ENV=testing CACHE_DRIVER=array php84 vendor/bin/phpunit \
  --bootstrap vendor/autoload.php tests/Feature/Server/AnyTLSRealityTest.php
```

Result: **8 tests, 71 assertions passed**. Tests use an in-memory SQLite database and array cache, including the application's explicitly named Redis cache store. No production database or Redis connection is used.

Coverage includes legacy rows, all three modes, independent VLESS/Trojan/AnyTLS keys, actual authenticated admin save and node config HTTP routes, invalid update persistence, ETag refresh after a short-ID change, client private-key separation and native standard-TLS subscription SNI/UUID output. The standard client formatter is exercised; no REALITY subscription claim is made.

## Live panel evidence and remaining work

On 2026-09-26, the matching backend and admin adapter were deployed with authorization. An independent hidden AnyTLS test node without permission groups was saved as REALITY through the live editor. Reopening the editor retained the mode, generated key pair, short ID, destination and nine padding rows. REALITY advanced settings omit the certificate tab; standard TLS retains it. This does not establish node runtime or traffic acceptance.

The public admin repository contains only compiled assets, and the user has no frontend source. `tools/admin-anytls` supplies a source-maintained adapter pinned to the exact deployed admin commit `fe6dc2952241c6bed807d1ca965e7d5907303265`. It reuses existing components and the original key generator. Three build tests verify rejection of unknown upstream assets, reproducibility, certificate conditions, and unchanged shared crypto/other protocol forms. The local browser harness verified all three modes and preservation of standard TLS SNI and padding.

**Deploy and roll back backend and frontend together.** Canonical AnyTLS responses use integer `tls`; the original editor expects an object. The deployed adapter handles this canonical shape.

Live PHP base: `1f4e4e031dd4680236d449be57aeab7aac15cc31`; PHP 8.3.31. Five PHP files, one new hashed JavaScript asset and the manifest were deployed. No database migration, dependency changes or original asset deletion occurred. Site (125 MB), database (3.6 MB) and deployment configuration (32 KB) archives were created before changes; checksums, archive readability and database dump completion were verified. A code rollback script and original replaced files were retained outside the web root. Full database recovery was not rehearsed against production.

Before deployment, 51 existing standard AnyTLS node wire configurations compared equal after accounting for the new explicit `tls=1`. The configuration fingerprint of 286 existing nodes remained unchanged after deployment. The focused PHPUnit suite also passed against the live-base checkout: 8 tests, 71 assertions. Live model/request probes passed on PHP 8.3.31.

Generated JavaScript SHA-256: `a04f8dd4c68007d123ca3b15fb38b9d3816a7ff907e2c862422bde6b92fe02d9`. The original entry asset remains available for rollback.

After the live UI save, an authenticated request through the production application's HTTP kernel returned `tls=2`, all saved REALITY fields, nine padding rows and no certificate configuration. This was an in-process route check, not an external network or node synchronization test. The actual admin-save audit retained the public key but omitted the private key. Test-node isolation, existing-node fingerprints and Horizon running status passed after saving.

The user also authorized installing a test node on a separate VPS. Standalone VPS traffic tests are independent of the live panel: they cannot prove panel user synchronization, reporting, limits or reload delivery. These gates remain open until an updated panel is deployed and connected.

## Rollback

Revert the panel code changes together with any matching frontend deployment. No database schema rollback is needed. New integer-mode AnyTLS rows need conversion to the old TLS object before using the original backend; REALITY nodes cannot operate on the original backend. Back up affected rows before deployment.
