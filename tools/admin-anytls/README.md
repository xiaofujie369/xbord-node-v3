# AnyTLS admin adapter

The public admin repository contains compiled assets only. This source-maintained adapter is pinned to admin commit `fe6dc2952241c6bed807d1ca965e7d5907303265`, entry `assets/index-BdbgNvrf.js`, SHA-256 `87ca206dbaa4d5660a5697679ec2c3737dcb45659b9e521e5c14d5f4ac6e4100`.

`extension.mjs` reuses the bundled VLESS TLS selector, REALITY fields and key/short-ID generators. It adds the destination field and preserves the original AnyTLS padding, standard TLS and ECH controls. The inherited Chinese SNI label is `伪装站点(dest)`; this field holds the server name, while `Destination / Handshake` holds `host:port`.

This is not recovered upstream frontend source. It depends on the pinned component structure. The builder refuses unknown bytes and missing anchors; review the adapter before any admin upgrade. Use original Git blob bytes or the original deployed file, not a checkout converted to CRLF.

```sh
python tools/admin-anytls/build.py /path/to/index-BdbgNvrf.js /path/to/output
python tools/admin-anytls/test_build.py /path/to/index-BdbgNvrf.js
node --check /path/to/output/index-anytls-reality-<hash>.js
```

Deploy the generated hashed asset and update the existing admin manifest entry together with the matching PHP backend. Retain the original asset and a copy of the old manifest. `preview.py` creates a local-only harness for testing the actual bundled form components; never deploy that harness.

Before deployment, back up the site, database and service configuration, verify archive checksums, and preserve the exact replaced files. Code rollback must restore the PHP files and original manifest together. REALITY test rows require separate handling when reverting to the original backend; do not restore an entire live database merely to undo a page change.

Verified on the live panel: standard TLS editor loads legacy settings; all three modes work in the isolated harness; REALITY generates keys using the original generator, saves an isolated hidden node, and retains fields after reopening. Advanced certificate controls appear only for standard TLS. Backend coverage and deployment evidence are in `PHASE2_IMPLEMENTATION_REPORT.md`.
