# WebSocket live verification — 2026-09-29

The panel already had a running Workerman service on port 8076 and a BT nginx proxy at `/ws/`. The configured advertised URL used `https://` and omitted the endpoint. Corrected it to the site's `wss://…/ws` endpoint and added an exact `/ws` proxy location while preserving `/ws/`. nginx configuration validation passed before graceful reload.

Backups of nginx extension, service definition, original settings and DeviceStateService.php were retained in a private timestamped deployment directory. No credentials or database dumps are included here.

A second issue became visible after connecting: `array_unique` retained non-contiguous numeric keys in device IP lists. PHP encoded these as JSON objects, which the node could not decode as lists. Reindexing the deduplicated values preserves the original device semantics while emitting arrays. Regression test covers duplicate IPv4 addresses, IPv6, expired entries and JSON list shape: 1 test / 2 assertions passed.

Live evidence: VLESS, standard AnyTLS, AnyTLS REALITY and Hysteria2 nodes all reported WS online. Both machine processes received an explicit `sync.nodes` event in the same second it was published through the panel's NodeSyncService/Redis path. Restarting Workerman to load the fix caused temporary disconnects, then both processes reconnected automatically within approximately 2–4 seconds. Live device snapshots contained only lists; no further device decode warnings appeared in the observed post-restart interval.

REST reporting remains active as fallback. This verifies connection, initial synchronization, machine notifications and reconnect behavior; it does not complete every user-delta, quota, rate/device-limit or extended-outage acceptance case. The earlier reports describe the state at their respective dates.

For another BT deployment, configure a working `wss://panel.example.com/ws` address and matching HTTP/1.1 Upgrade reverse proxy to Workerman. Existing node processes that captured the old URL at startup require a controlled restart to fetch the new handshake. Avoid posting authenticated WS URLs in logs or public reports.
