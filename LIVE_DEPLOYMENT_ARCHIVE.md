# Live deployment archive — 2026-09-27

This branch is a source snapshot of the deployed panel worktree based on upstream 1f4e4e031dd4680236d449be57aeab7aac15cc31, with application changes through 019677c. It differs from the older codex/anytls-reality-panel archive because that archive used a different upstream baseline. Do not interpret unrelated baseline differences as intended feature removals, or blindly apply this whole snapshot onto a newer production panel.

Includes the backend AnyTLS modes, pinned compiled-admin adapter, private-key redaction, native sing-box REALITY export, integer ports, standard TLS ALPN preservation and native client version handling. Original phase reports describe evidence at their respective dates.

## Additional live verification

On Debian 13 amd64, the custom node at 417cd4f successfully served standard AnyTLS TCP 443, AnyTLS REALITY TCP 8443 and Hysteria2 TLS + Salamander UDP 2443. Each passed proxy egress and 64 KiB HTTPS download checks, and the panel received online/traffic reports. REALITY was tested using the user's updated SNI and matching handshake destination. The user also confirmed standard AnyTLS in Android FlClash.

Actual test subscriptions returned HTTP 200 for FlClash 0.8.98 and native sing-box. FlClash exports standard AnyTLS and Hysteria2, while AnyTLS REALITY remains excluded by design. Fixing root-owned application logs resolved an HTTP 500; carrying the FlClash version in the test URL resolved Hysteria2 filtering. These were deployment/artifact corrections, not additional application patches. Run future PHP maintenance as the site user to avoid root-owned log files.

WebSocket has an unresolved malformed URL error; REST polling currently carries synchronization and traffic reporting. Full reconnect, reconciliation, rate/device limit and reload acceptance remain pending. No all-device/all-network guarantee is implied. Legacy DNS template deprecation warnings remain.

## Installation and restoration

See main branch docs/DEPLOYMENT_ZH.md for the custom node installation. For panel integration, follow PHASE2_IMPLEMENTATION_REPORT.md, PHASE3_IMPLEMENTATION_REPORT.md and tools/admin-anytls/README.md after a verified site/database/configuration backup. The admin adapter is version-pinned and requires the exact original asset; it is not a general frontend replacement.

Only source, adapter and documentation are archived. Live credentials, subscription URLs, database backups, certificates and deployed binary are excluded. Restore PHP files and manifest together for code rollback; preserve later user changes and avoid a blanket database restore.
