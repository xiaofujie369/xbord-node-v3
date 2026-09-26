# Phase 3 — native sing-box subscription

Implemented and deployed on 2026-09-26 from panel base `4303c7a`. The user explicitly requested skipping VPS connection acceptance and proceeding with subscriptions. Runtime connectivity, traffic reporting and limits remain unverified live gates; they are not marked passed.

## Behavior

An explicit native sing-box request (`flag=sing-box`, or a recognized sing-box user agent) includes AnyTLS REALITY nodes. Known client versions below 1.12.0 are excluded. An explicit format without a version exports the configuration without claiming the consuming client's capabilities. Hiddify/SFM aliases, Mihomo/Clash and generic URI exports still exclude these nodes. Plaintext AnyTLS remains excluded.

Use the existing subscription URL with `&flag=sing-box` when it already has a query string, or `?flag=sing-box` otherwise. Replace an existing `flag` parameter instead of duplicating it. Normal availability, permission-group, visibility and node filtering continue to apply. The isolated test node remains hidden without permission groups.

The REALITY outbound uses an explicit allowlist: host, integer port, existing user UUID password, SNI, public key, short ID and Chrome uTLS. It never copies the server-side settings wholesale: private key, handshake destination and certificate configuration are absent. Standard AnyTLS subscription construction remains unchanged.

References checked: [AnyTLS outbound](https://sing-box.sagernet.org/configuration/outbound/anytls/) and [shared TLS](https://sing-box.sagernet.org/configuration/shared/tls/). No new dependency or kernel changes were made.

## Verification

- Focused PHP suite: **8 tests, 87 assertions passed** on PHP 8.4.16. Covers the earlier panel/admin/node gates plus native REALITY fields, no private key, preservation of standard TLS, low-version exclusion and unsupported format exclusions.
- PHP 8.3.31 staged and deployed probes: subscription controller renders the hidden test-node fixture with a synthetic user password, matching the saved public key and excluding the private key. This does not alter node visibility or grant a real user access.
- All **51 existing standard AnyTLS** outbound configurations compared identical before and after.
- All **286 existing node configurations** retained their pre-Phase-2 fingerprint.
- The emitted REALITY outbound, wrapped in a minimal client config, passed `sing-box check` with the existing local test binary. Its embedded module is the unchanged project replacement `github.com/cedar2025/sing-box v1.14.0-alpha.2.0.20260316103356-2e665cb7e295`, built with uTLS. This checks configuration construction only, not a remote connection or every deployed custom subscription template.

## Deployment and rollback

Only `app/Protocols/SingBox.php` and `app/Http/Controllers/V1/Client/ClientController.php` changed in production. Their exact prior contents were preserved and checksummed in a new private Phase 3 backup directory, with `rollback-code.sh`. Original hashes were checked before atomic replacement; PHP-FPM was reloaded and Horizon instructed to restart. No database writes, frontend changes or dependency updates were part of this deployment. The earlier full site/database backup remains available.

To undo only this phase, run the Phase 3 code rollback script. This restores the previous subscription filter while retaining the Phase 2 editor. Do not use a full database restore for this code-only change.
