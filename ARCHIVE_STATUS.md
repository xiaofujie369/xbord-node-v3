# AnyTLS + REALITY development archive

This repository archives work in progress; it is not a completed production acceptance release.

- `main`: Xboard-Node implementation, tests, reference client JSON and implementation reports. Upstream baseline: `cedar2025/Xboard-Node` commit `0a29338e1f102a462363ce3527417029f89bab28`.
- `codex/anytls-reality-panel`: independent Xboard backend history and Phase 2 tests/report. Upstream baseline: `cedar2025/Xboard` commit `4f48e61a2cbc6db5338872b6bdb45ef954ec1256`. This branch contains a different project; do not merge it into the node source tree.

## Verified

Node formatting, vet, unit/race tests, production-tag build and local native-client integration passed. See `IMPLEMENTATION_REPORT.md` for evidence and limits.

Panel backend tests passed: 8 tests, 71 assertions, including authenticated admin/node APIs, persistence, legacy AnyTLS compatibility, independent REALITY keys, subscription private-key separation and nested audit-log redaction. See `PHASE2_IMPLEMENTATION_REPORT.md` on the panel branch.

## Still pending

- Maintainable admin frontend changes: the available frontend repository has only compiled assets, and no original source was supplied. Do not deploy the backend alone with the old editor.
- Live panel deployment, REALITY node configuration and full panel synchronization/reporting acceptance.
- VPS installation and external-client traffic acceptance. SSH access initially succeeded and source compilation started, but the session disconnected during compilation. Subsequent SSH handshakes timed out. Node startup and successful proxy traffic were not verified; the VPS's final build state is unknown.
- Phase 3 REALITY subscription generation.

No live credentials, test private keys, SSH keys, private node configuration files or VPS binaries are included in this archive. Test fixtures and placeholder client examples are not production credentials.
