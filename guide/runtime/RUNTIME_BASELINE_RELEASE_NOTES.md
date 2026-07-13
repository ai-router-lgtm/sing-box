# Runtime Baseline Release Notes

## Overview

This baseline delivers runtime control capabilities required by ai-vpn control-plane integration, without requiring full config reload.

Runtime endpoints are exposed under `experimental.clash_api.external_controller`:

- `PUT /runtime/policy`
- `PUT /runtime/users`
- `DELETE /runtime/users/{principal}?inbound={tag}`
- `POST /runtime/disconnect`
- `GET /runtime/stats/snapshot`
- `GET /runtime/status`
- `GET /runtime/policy/snapshot`

## Contract Semantics

### revision

`revision` is a monotonic version from control-plane.

- Newer revision is accepted.
- Older revision is rejected with `rejected: "stale_revision"`.

### request_id

`request_id` is used for traceability and idempotency.

- `/runtime/users` returns `idempotent: true` when a duplicate request is ignored.
- `/runtime/policy` echoes `requestId` in response.
- `/runtime/disconnect` caches non-empty request IDs so retries do not repeat the connection scan.

### runtime process identity and digest

`GET /runtime/status` returns a process-scoped `runtime_instance_id`, capability list, and deterministic user/policy digests. A process restart always changes the instance ID. Credentials participate in the user digest but are never returned by the status endpoint.

### managed user replacement

`PUT /runtime/users` accepts `replace_managed: true` for authoritative recovery. Replacement only removes users tracked as Runtime-managed in the current process. Static configuration users are preserved and restored after a same-principal Runtime override is removed.

### batch disconnect

`POST /runtime/disconnect` accepts `principals[]` and `user_ids[]`, deduplicates at most 1000 selectors, and evaluates them in one connection-map scan. Existing single-selector fields remain supported.

### Response fields

Common response semantics:

- `applied`: whether the request is applied.
- `revision`: current applied revision.
- `rejected`: rejection reason, currently `stale_revision`.
- `idempotent`: present for duplicate users requests.

### runtime stats snapshot

`GET /runtime/stats/snapshot` returns:

- `upload_total` / `download_total`: global cumulative traffic totals.
- `principal_stats[].upload` / `principal_stats[].download`: cumulative traffic totals per principal.
- `principal_stats[].active`: current active connection count per principal.

The `principal_stats` traffic fields are cumulative values and are no longer computed as the sum of currently active connections.

## Compatibility

### runtime users upsert alias

`upsert` accepts both:

- `principal` (preferred)
- `name` (compatibility alias mapped to `principal`)

This avoids dropping historical control-plane payloads still using `name`.

## Wildcard Policy Matching

Policy resolution priority:

1. exact principal: `user_id:device_id`
2. user wildcard: `user_id:*`
3. no policy

This priority is applied consistently in:

- connection admission (`max_connections`)
- dynamic TCP/UDP rate limiting (`up_bps` / `down_bps`)
- per-principal runtime snapshot policy display

## Verification

- Unit/integration tests: `experimental/clashapi/...`
- Protocol compile checks: VLESS, VMess, Trojan, TUIC, Hysteria2, Hysteria, Shadowsocks
- E2E template: `guide/runtime/verify-runtime-user-control-template.sh`
