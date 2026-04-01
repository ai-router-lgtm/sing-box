# Runtime Baseline Release Notes

## Overview

This baseline delivers runtime control capabilities required by ai-vpn control-plane integration, without requiring full config reload.

Runtime endpoints are exposed under `experimental.clash_api.external_controller`:

- `PUT /runtime/policy`
- `PUT /runtime/users`
- `DELETE /runtime/users/{principal}?inbound={tag}`
- `POST /runtime/disconnect`
- `GET /runtime/stats/snapshot`

## Contract Semantics

### revision

`revision` is a monotonic version from control-plane.

- Newer revision is accepted.
- Older revision is rejected with `rejected: "stale_revision"`.

### request_id

`request_id` is used for traceability and idempotency.

- `/runtime/users` returns `idempotent: true` when a duplicate request is ignored.
- `/runtime/policy` echoes `requestId` in response.

### Response fields

Common response semantics:

- `applied`: whether the request is applied.
- `revision`: current applied revision.
- `rejected`: rejection reason, currently `stale_revision`.
- `idempotent`: present for duplicate users requests.

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
