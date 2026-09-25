# Phase 2 — backend verified, frontend/deployment pending

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

With the user's authorization, an independent hidden AnyTLS test node was created in their panel, without permission groups. Existing nodes were not edited. The live AnyTLS editor has certificate TLS/SNI/ECH/padding settings but no TLS mode selector or REALITY settings. This node has not been converted into a REALITY node and has not passed panel synchronization acceptance.

The public admin repository contains only compiled assets. The user confirmed they have no frontend source. Existing compiled VLESS/Trojan UI contains a shared key generator; it has not been duplicated or replaced. Maintainable mode selection and reuse of that component remain pending source availability or an agreed frontend replacement approach.

**Do not deploy this backend alone with the old admin editor.** Canonical AnyTLS responses use integer `tls`; the old editor expects an object. Accepting old save payloads preserves API input compatibility but does not make that old editor able to read or safely edit REALITY nodes. Matching frontend work is required before production deployment.

The user also authorized installing a test node on a separate VPS. Standalone VPS traffic tests are independent of the live panel: they cannot prove panel user synchronization, reporting, limits or reload delivery. These gates remain open until an updated panel is deployed and connected.

## Rollback

Revert the panel code changes together with any matching frontend deployment. No database schema rollback is needed. New integer-mode AnyTLS rows need conversion to the old TLS object before using the original backend; REALITY nodes cannot operate on the original backend. Back up affected rows before deployment.
