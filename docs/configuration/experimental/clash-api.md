!!! quote "Changes in sing-box 1.10.0"

    :material-plus: [access_control_allow_origin](#access_control_allow_origin)  
    :material-plus: [access_control_allow_private_network](#access_control_allow_private_network)

!!! quote "Changes in sing-box 1.8.0"

    :material-delete-alert: [store_mode](#store_mode)  
    :material-delete-alert: [store_selected](#store_selected)  
    :material-delete-alert: [store_fakeip](#store_fakeip)  
    :material-delete-alert: [cache_file](#cache_file)  
    :material-delete-alert: [cache_id](#cache_id)

### Structure

=== "Structure"

    ```json
    {
      "external_controller": "127.0.0.1:9090",
      "external_ui": "",
      "external_ui_download_url": "",
      "external_ui_download_detour": "",
      "secret": "",
      "default_mode": "",
      "access_control_allow_origin": [],
      "access_control_allow_private_network": false,
      
      // Deprecated
      
      "store_mode": false,
      "store_selected": false,
      "store_fakeip": false,
      "cache_file": "",
      "cache_id": ""
    }
    ```

=== "Example (online)"

    !!! question "Since sing-box 1.10.0"

    ```json
    {
      "external_controller": "127.0.0.1:9090",
      "access_control_allow_origin": [
        "http://127.0.0.1",
        "http://yacd.haishan.me"
      ],
      "access_control_allow_private_network": true
    }
    ```

=== "Example (download)"

    !!! question "Since sing-box 1.10.0"

    ```json
    {
      "external_controller": "0.0.0.0:9090",
      "external_ui": "dashboard"
      // "external_ui_download_detour": "direct"
    }
    ```

!!! note ""

    You can ignore the JSON Array [] tag when the content is only one item

### Fields

#### external_controller

RESTful web API listening address. Clash API will be disabled if empty.

#### external_ui

A relative path to the configuration directory or an absolute path to a
directory in which you put some static web resource. sing-box will then
serve it at `http://{{external-controller}}/ui`.

#### external_ui_download_url

ZIP download URL for the external UI, will be used if the specified `external_ui` directory is empty.

`https://github.com/MetaCubeX/Yacd-meta/archive/gh-pages.zip` will be used if empty.

#### external_ui_download_detour

The tag of the outbound to download the external UI.

Default outbound will be used if empty.

#### secret

Secret for the RESTful API (optional)
Authenticate by spedifying HTTP header `Authorization: Bearer ${secret}`
ALWAYS set a secret if RESTful API is listening on 0.0.0.0

#### default_mode

Default mode in clash, `Rule` will be used if empty.

This setting has no direct effect, but can be used in routing and DNS rules via the `clash_mode` rule item.

#### access_control_allow_origin

!!! question "Since sing-box 1.10.0"

CORS allowed origins, `*` will be used if empty.

To access the Clash API on a private network from a public website, you must explicitly specify it in `access_control_allow_origin` instead of using `*`.

#### access_control_allow_private_network

!!! question "Since sing-box 1.10.0"

Allow access from private network.

To access the Clash API on a private network from a public website, `access_control_allow_private_network` must be enabled.

#### store_mode

!!! failure "Deprecated in sing-box 1.8.0"

    `store_mode` is deprecated in Clash API and enabled by default if `cache_file.enabled`.

Store Clash mode in cache file.

#### store_selected

!!! failure "Deprecated in sing-box 1.8.0"

    `store_selected` is deprecated in Clash API and enabled by default if `cache_file.enabled`.

!!! note ""

    The tag must be set for target outbounds.

Store selected outbound for the `Selector` outbound in cache file.

#### store_fakeip

!!! failure "Deprecated in sing-box 1.8.0"

    `store_selected` is deprecated in Clash API and migrated to `cache_file.store_fakeip`.

Store fakeip in cache file.

#### cache_file

!!! failure "Deprecated in sing-box 1.8.0"

    `cache_file` is deprecated in Clash API and migrated to `cache_file.enabled` and `cache_file.path`.

Cache file path, `cache.db` will be used if empty.

#### cache_id

!!! failure "Deprecated in sing-box 1.8.0"

    `cache_id` is deprecated in Clash API and migrated to `cache_file.cache_id`.

Identifier in cache file.

If not empty, configuration specified data will use a separate store keyed by it.

### Runtime Control API

When `experimental.clash_api.external_controller` is enabled, sing-box also exposes runtime control endpoints under `/runtime`.

These endpoints are designed for control-plane and node-agent integrations that need real-time user/policy updates without full process reload.

#### PUT `/runtime/policy`

Apply principal-based policy revision.

Request body:

```json
{
  "revision": 1001,
  "request_id": "policy-req-001",
  "replace": false,
  "policies": [
    {
      "principal": "user_id:device_id",
      "max_connections": 3,
      "up_bps": 1048576,
      "down_bps": 2097152
    }
  ]
}
```

Response fields:

- `applied`: whether this revision is accepted.
- `revision`: current policy revision.
- `requestId`: echoed request id.
- `rejected`: `stale_revision` when revision is older than current revision.

#### POST `/runtime/disconnect`

Disconnect by principal or by user id.

Request body examples:

```json
{
  "principal": "user_id:device_id"
}
```

```json
{
  "user_id": "user_id"
}
```

`user_id` without `device_id` disconnects both `user_id` and `user_id:*`.

#### GET `/runtime/stats/snapshot`

Return traffic totals and principal snapshots:

- `upload_total`
- `download_total`
- `principal_stats[]` (active connections and traffic per principal)

#### PUT `/runtime/users`

Upsert/delete runtime users on specific inbounds.

Request body:

```json
{
  "revision": 2001,
  "request_id": "req-001",
  "operations": [
    {
      "inbound": "vless-in",
      "upsert": [
        {
          "principal": "user_id:device_id",
          "uuid": "00000000-0000-0000-0000-000000000000",
          "enabled": true
        }
      ],
      "delete": [
        "old_user:old_device"
      ]
    }
  ]
}
```

Supported runtime user inbounds include VLESS, VMess, Trojan, TUIC and Hysteria2.

`upsert` accepts `principal` and also accepts `name` as a compatibility alias of `principal`.

Response fields:

- `applied`: whether operation is applied.
- `revision`: current users revision.
- `rejected`: `stale_revision` when revision is stale.
- `idempotent`: `true` when duplicated `request_id` is ignored.

#### DELETE `/runtime/users/{principal}?inbound={tag}`

Delete one runtime user from a specific inbound.
