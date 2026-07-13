!!! quote "sing-box 1.10.0 中的更改"

    :material-plus: [access_control_allow_origin](#access_control_allow_origin)  
    :material-plus: [access_control_allow_private_network](#access_control_allow_private_network)

!!! quote "sing-box 1.8.0 中的更改"

    :material-delete-alert: [store_mode](#store_mode)  
    :material-delete-alert: [store_selected](#store_selected)  
    :material-delete-alert: [store_fakeip](#store_fakeip)  
    :material-delete-alert: [cache_file](#cache_file)  
    :material-delete-alert: [cache_id](#cache_id)

### 结构

=== "结构"

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

=== "示例 (在线)"

    !!! question "自 sing-box 1.10.0 起"

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

=== "示例 (下载)"

    !!! question "自 sing-box 1.10.0 起"

    ```json
    {
      "external_controller": "0.0.0.0:9090",
      "external_ui": "dashboard"
      // "external_ui_download_detour": "direct"
    }
    ```

!!! note ""

    当内容只有一项时，可以忽略 JSON 数组 [] 标签

### Fields

#### external_controller

RESTful web API 监听地址。如果为空，则禁用 Clash API。

#### external_ui

到静态网页资源目录的相对路径或绝对路径。sing-box 会在 `http://{{external-controller}}/ui` 下提供它。

#### external_ui_download_url

静态网页资源的 ZIP 下载 URL，如果指定的 `external_ui` 目录为空，将使用。

默认使用 `https://github.com/MetaCubeX/Yacd-meta/archive/gh-pages.zip`。

#### external_ui_download_detour

用于下载静态网页资源的出站的标签。

如果为空，将使用默认出站。

#### secret

RESTful API 的密钥（可选）
通过指定 HTTP 标头 `Authorization: Bearer ${secret}` 进行身份验证
如果 RESTful API 正在监听 0.0.0.0，请始终设置一个密钥。

#### default_mode

Clash 中的默认模式，默认使用 `Rule`。

此设置没有直接影响，但可以通过 `clash_mode` 规则项在路由和 DNS 规则中使用。

#### access_control_allow_origin

!!! question "自 sing-box 1.10.0 起"

允许的 CORS 来源，默认使用 `*`。

要从公共网站访问私有网络上的 Clash API，必须在 `access_control_allow_origin` 中明确指定它而不是使用 `*`。

#### access_control_allow_private_network

!!! question "自 sing-box 1.10.0 起"

允许从私有网络访问。

要从公共网站访问私有网络上的 Clash API，必须启用 `access_control_allow_private_network`。

#### store_mode

!!! failure "已在 sing-box 1.8.0 废弃"

    `store_mode` 已在 Clash API 中废弃，且默认启用当 `cache_file.enabled`。

将 Clash 模式存储在缓存文件中。

#### store_selected

!!! failure "已在 sing-box 1.8.0 废弃"

    `store_selected` 已在 Clash API 中废弃，且默认启用当 `cache_file.enabled`。

!!! note ""

    必须为目标出站设置标签。

将 `Selector` 中出站的选定的目标出站存储在缓存文件中。

#### store_fakeip

!!! failure "已在 sing-box 1.8.0 废弃"

    `store_selected` 已在 Clash API 中废弃，且已迁移到 `cache_file.store_fakeip`。

将 fakeip 存储在缓存文件中。

#### cache_file

!!! failure "已在 sing-box 1.8.0 废弃"
 
    `cache_file` 已在 Clash API 中废弃，且已迁移到 `cache_file.enabled` 和 `cache_file.path`。

缓存文件路径，默认使用`cache.db`。

#### cache_id

!!! failure "已在 sing-box 1.8.0 废弃"
 
    `cache_id` 已在 Clash API 中废弃，且已迁移到 `cache_file.cache_id`。

缓存 ID。

如果不为空，配置特定的数据将使用由其键控的单独存储。

### Runtime 控制接口

当启用 `experimental.clash_api.external_controller` 后，sing-box 还会在 `/runtime` 暴露运行时控制接口。

这些接口用于控制面或 node-agent 场景，以便在不 reload 进程的情况下实时更新用户与策略。

#### GET `/runtime/status`

返回当前 Runtime 进程身份和收敛状态：

- `runtime_instance_id`：每次 sing-box 进程启动都会生成新的 UUID。
- `version` 与 `started_at`。
- `capabilities`：当前支持的 Runtime 控制能力。
- 用户和策略的 `revision`、`count` 与 SHA-256 `digest`。
- `user_state`：每个受管入站的用户数量和摘要。

响应不会返回用户 UUID 或密码明文。摘要会覆盖认证字段，使控制面无需读取明文凭据即可核验状态。

摘要输入使用 UTF-8 紧凑 JSON 并计算 SHA-256。受管用户按 inbound、principal 排序，字段为 `inbound`、`principal`、可选的 `uuid/password/flow/alter_id` 和 `enabled`；策略按 principal 排序。`experimental/clashapi/runtime_test.go` 保存了兼容 fixture，控制面可用它校验编码实现。

#### PUT `/runtime/policy`

应用基于 principal 的策略版本。

请求体：

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

响应字段：

- `applied`：该版本是否被接受。
- `revision`：当前策略版本号。
- `requestId`：回显请求 ID。
- `rejected`：当版本过旧时为 `stale_revision`。

#### GET `/runtime/policy/snapshot`

返回排序后的当前策略快照及 revision、数量和摘要。该接口应只由本机控制器或经过认证的 node-agent 代理访问。

#### POST `/runtime/disconnect`

按 principal 或用户维度断开连接。

请求体示例：

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

仅传 `user_id`（不带 `device_id`）时，会同时断开 `user_id` 与 `user_id:*`。

批量请求最多接受 1000 个去重后的 selector，并且只扫描一次连接表：

```json
{
  "request_id": "drain-node-001",
  "principals": ["user-1:device-1"],
  "user_ids": ["user-2", "user-3"]
}
```

重复使用非空 `request_id` 时会返回缓存结果和 `idempotent: true`，不会再次扫描连接。

#### GET `/runtime/stats/snapshot`

返回总流量和 principal 维度快照：

- `upload_total`
- `download_total`
- `principal_stats[]`（每个 principal 的活动连接与累计流量）

其中：

- `upload_total` / `download_total` 为全局累计值。
- `principal_stats[].upload` / `principal_stats[].download` 为该 principal 的累计值，单调不减。
- `principal_stats[].active` 为当前活跃连接数，实时变化。

#### PUT `/runtime/users`

对指定入站执行运行时用户增删改。

请求体：

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

当前支持运行时用户更新的入站包括 VLESS、VMess、Trojan、TUIC、Hysteria2。

`upsert` 除 `principal` 外，也支持 `name` 作为 `principal` 的兼容别名。

控制面进行权威恢复时可设置 `replace_managed: true`。此时每个 operation 必须提供该入站完整的受管用户集合；此前通过 Runtime API 写入但本次缺失的用户会被删除，静态 sing-box 配置用户会被保留。即使受管用户临时覆盖了同名静态 principal，移除覆盖时也会恢复原静态用户。

响应字段：

- `applied`：是否已应用。
- `revision`：当前用户版本号。
- `rejected`：当版本过旧时为 `stale_revision`。
- `idempotent`：重复 `request_id` 被忽略时为 `true`。

#### DELETE `/runtime/users/{principal}?inbound={tag}`

从指定入站删除一个运行时用户。
