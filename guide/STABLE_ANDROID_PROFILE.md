# Android 稳定配置基线（已验证）

适用版本：`sing-box 1.13.x`

本配置目标：

- 普通流量默认走 `auto` 自动选路
- DNS 固定走稳定节点 `Tokyo`（避免自动切换导致的解析失败）
- 国内与内网流量直连
- 强制劫持 DNS（防止 DNS 包误走 `urltest`）

## 核心思路

1. `route.final = auto`：业务流量自动选路。
2. `dns.servers[].detour = Tokyo`：DNS 解析固定稳定链路。
3. DNS 劫持规则放在最前：
   - `protocol: dns -> hijack-dns`
   - `udp:53 -> hijack-dns`
   - `tcp:53 -> hijack-dns`
4. 再放通用规则：
   - `sniff`
   - `resolve`
   - `ip_is_private -> direct`
   - `local/lan/home.arpa -> direct`
   - `cn -> direct`

## 直接可用模板（示例）

```json
{
  "log": {
    "level": "info",
    "timestamp": true
  },
  "dns": {
    "strategy": "ipv4_only",
    "servers": [
      {
        "type": "local",
        "tag": "local"
      },
      {
        "type": "https",
        "tag": "cf",
        "server": "1.1.1.1",
        "server_port": 443,
        "path": "/dns-query",
        "tls": {
          "enabled": true,
          "server_name": "cloudflare-dns.com"
        },
        "detour": "auto"
      },
      {
        "type": "https",
        "tag": "google",
        "server": "8.8.8.8",
        "server_port": 443,
        "path": "/dns-query",
        "tls": {
          "enabled": true,
          "server_name": "dns.google"
        },
        "detour": "auto"
      }
    ],
    "rules": [
      {
        "domain_suffix": [
          "cn"
        ],
        "server": "local"
      }
    ],
    "final": "cf"
  },
  "route": {
    "auto_detect_interface": true,
    "final": "auto",
    "rules": [
      {
        "protocol": "dns",
        "action": "hijack-dns"
      },
      {
        "network": "udp",
        "port": 53,
        "action": "hijack-dns"
      },
      {
        "network": "tcp",
        "port": 53,
        "action": "hijack-dns"
      },
      {
        "action": "sniff"
      },
      {
        "action": "resolve"
      },
      {
        "ip_is_private": true,
        "action": "route",
        "outbound": "direct"
      },
      {
        "domain_suffix": [
          "local",
          "lan",
          "home.arpa"
        ],
        "action": "route",
        "outbound": "direct"
      },
      {
        "domain_suffix": [
          "cn"
        ],
        "action": "route",
        "outbound": "direct"
      }
    ]
  }
}
```

## 常见坑

- 若出现 `missing supported outbound`，优先检查 DNS 劫持规则是否在前面。
- 若出现“找不到 DNS 地址”，优先检查：
  - DNS 的 `detour` 是否指向稳定节点；
  - 是否把 `ip_is_private -> direct` 放在了 DNS 劫持规则之前。
