# Sing-Box 配置完全指南 (v1.13.3)

## 目录
1. [概述](#概述)
2. [配置结构](#配置结构)
3. [核心功能模块详解](#核心功能模块详解)
4. [VPN中的具体应用](#vpn中的具体应用)
5. [常见协议详解](#常见协议详解)
6. [最佳实践](#最佳实践)

---

## 概述

**sing-box** 是一个通用的代理平台，支持以下核心功能：
- **协议多样** - 支持 Shadowsocks、VLESS、VMess、Trojan、Hysteria2 等 20+ 协议
- **灵活路由** - 基于域名、IP、进程、应用等多维度的流量路由
- **DNS 防污染** - 内置 DNS 解析、分流、FakeIP 等高级功能
- **跨平台** - 支持 Linux、Windows、macOS、Android、iOS 等主流平台

---

## 配置结构

### 根配置框架

```json
{
  "log": {},              // 日志配置
  "dns": {},              // DNS 服务配置
  "ntp": {},              // 时间同步配置
  "certificate": {},      // TLS 证书配置
  "endpoints": [],        // 端点列表（用于路由）
  "inbounds": [],         // 入站配置（接收客户端连接）
  "outbounds": [],        // 出站配置（向远程发起连接）
  "route": {},            // 路由配置（流量分配）
  "services": [],         // 附加服务（如 Clash API）
  "experimental": {}      // 实验功能
}
```

---

## 核心功能模块详解

### 1. 日志配置 (Log)

**作用**：记录 sing-box 运行日志，便于调试和监控

| 参数 | 类型 | 说明 | VPN 应用 |
|------|------|------|---------|
| `level` | string | 日志级别：`trace`/`debug`/`info`/`warn`/`error` | 开发调试时用 `debug`，生产环境用 `warn` |
| `output` | string | 日志文件路径，留空则输出到 stdout | 生产服务器应指定文件路径便于持久化 |
| `timestamp` | bool | 是否显示时间戳 | 服务器模式建议开启便于问题追踪 |

**示例**：
```json
{
  "log": {
    "level": "info",
    "output": "/var/log/sing-box.log",
    "timestamp": true
  }
}
```

---

### 2. 入站配置 (Inbound)

**作用**：定义 sing-box 如何接收来自客户端（如浏览器、app）的流量

#### 2.1 入站类型对比

| 类型 | 协议 | 用途 | 性能 | 加密 |
|------|------|------|------|------|
| `mixed` | HTTP/SOCKS5 | 通用代理，兼容性最好 | ⭐⭐⭐ | ✗ |
| `http` | HTTP | Web 代理 | ⭐⭐⭐⭐ | ✗ |
| `socks` | SOCKS4/5 | 通用代理 | ⭐⭐⭐⭐ | ✗ |
| `shadowsocks` | Shadowsocks | 高效加密代理 | ⭐⭐⭐⭐⭐ | ✓ |
| `vless` | VLESS | 轻量加密代理 | ⭐⭐⭐⭐⭐ | ✓ |
| `vmess` | VMess | 灵活的加密代理 | ⭐⭐⭐⭐ | ✓ |
| `trojan` | Trojan | 伪装 HTTPS 流量 | ⭐⭐⭐⭐⭐ | ✓ |
| `hysteria2` | Hysteria2 | 高速 UDP 协议 | ⭐⭐⭐⭐⭐⭐ | ✓ |
| `tun` | TUN/TAP | 虚拟网卡全流量代理 | ⭐⭐⭐ | ✓ |
| `redirect` | TCP redirect | Linux 透明代理 | ⭐⭐⭐⭐ | ✗ |
| `tproxy` | TP-Proxy | Linux 全流量代理 | ⭐⭐⭐⭐ | ✗ |

#### 2.2 关键参数解析

| 参数 | 类型 | 说明 | VPN 应用 |
|------|------|------|---------|
| `type` | string | **必需**，入站类型 | 服务器选 `vless`/`trojan`/`hysteria2`，客户端选 `mixed`/`socks` |
| `tag` | string | **必需**，入站唯一标识 | 用于路由配置中引用，如 `"local-in"` |
| `listen` | string | 监听地址，`::`=IPv6+IPv4，留空=localhost | VPN 服务器：`"::"` 或 `"0.0.0.0"`；客户端：`"127.0.0.1"` |
| `listen_port` | int | 监听端口 | VPN 服务器用 1024-65535；客户端用 1080 |
| `network` | string | 网络协议 `tcp`/`udp`，留空则都支持 | 大多数协议两种都支持；UDP 用于 DNS/游戏 |
| `tcp_keep_alive` | object | TCP 心跳设置，保持连接活跃 | 长连接场景下建议配置 |
| `udp_timeout` | string | UDP 连接超时时间 | 默认 `5m`，国际线路可调大 |
| `users` | array | 用户认证信息（适用于 SOCKS5） | 客户端代理需要密码时配置 |

#### 2.3 VPN 常见应用场景

**场景 1：VPN 服务器（接收远程客户端）**
```json
{
  "type": "vless",           // 轻量高效
  "tag": "vless-in",
  "listen": "::",            // 监听所有 IPv4 和 IPv6
  "listen_port": 443,        // HTTPS 端口
  "network": "tcp",
  "users": [
    {
      "uuid": "b831381d-6324-4d53-ad4f-8cda48b30811",
      "flow": "xtls-rprx-vision"  // VLESS XTLS 流控
    }
  ],
  "tls": {
    "enabled": true,
    "certificate_path": "/etc/sing-box/cert.pem",
    "key_path": "/etc/sing-box/key.pem"
  }
}
```

**场景 2：VPN 客户端（接收本地应用）**
```json
{
  "type": "mixed",           // HTTP + SOCKS5
  "tag": "local-in",
  "listen": "127.0.0.1",     // 仅本机
  "listen_port": 1080,       // 标准 SOCKS 端口
  "network": "tcp"
}
```

---

### 3. 出站配置 (Outbound)

**作用**：定义 sing-box 如何向外部发起连接（连到目标服务器或 VPN）

#### 3.1 出站类型对比

| 类型 | 用途 | 加密 | 场景 |
|------|------|------|------|
| `direct` | 直连目标 | ✗ | 解锁内容、国内流量 |
| `block` | 阻止连接 | ✗ | 广告拦截、恶意网站黑名单 |
| `vless` | 对接 VLESS 服务器 | ✓ | VPN 客户端模式 |
| `vmess` | 对接 VMess 服务器 | ✓ | 兼容 v2ray |
| `trojan` | 对接 Trojan 服务器 | ✓ | 伪装 HTTPS |
| `shadowsocks` | 对接 SS 服务器 | ✓ | 轻量加密 |
| `hysteria2` | 对接 Hysteria2 服务器 | ✓ | 高速 UDP |
| `wireguard` | 对接 WireGuard 服务器 | ✓ | VPN 层级 |
| `ssh` | 通过 SSH 隧道 | ✓ | 堡垒机模式 |
| `tor` | Tor 网络 | ✓ | 匿名最强 |
| `selector` | 出站选择器 | - | 手动切换不同出站 |
| `urltest` | 自动测速选择 | - | 自动选择最快出站 |

#### 3.2 关键参数解析

| 参数 | 类型 | 说明 | VPN 应用 |
|------|------|------|---------|
| `type` | string | **必需**，出站类型 | VPN 客户端：`vless`/`trojan`/`hysteria2`；国内：`direct` |
| `tag` | string | **必需**，出站唯一标识 | 路由中引用，如 `"vpn-out"`、`"direct-out"` |
| `server` | string | 目标服务器地址 | VPN 服务器 IP 或域名 |
| `server_port` | int | 目标服务器端口 | VPN 服务器端口，通常 443/8443 |
| `network` | string | 网络协议 | 一般留空仅用 tcp，部分协议支持 udp |
| `bind_interface` | string | 绑定网卡名 | 多网卡环境指定出站网卡 |
| `connect_timeout` | string | 连接超时 | 默认 `5s`，跨境链路可调大到 `10s` |
| `detour` | string | 链式出站 | 出站嵌套，如先走代理再走 VPN |

#### 3.3 VPN 常见应用场景

**场景 1：连接到远程 VPN 服务器**
```json
{
  "type": "vless",
  "tag": "vpn-out",
  "server": "vpn.example.com",
  "server_port": 443,
  "uuid": "b831381d-6324-4d53-ad4f-8cda48b30811",
  "greeting": false,
  "tls": {
    "enabled": true,
    "server_name": "vpn.example.com",
    "insecure": false,
    "utls": {
      "enabled": true,
      "fingerprint": "chrome"
    }
  },
  "connect_timeout": "10s"
}
```

**场景 2：直连国内流量**
```json
{
  "type": "direct",
  "tag": "direct-out",
  "bind_interface": "eth0",
  "connect_timeout": "5s"
}
```

**场景 3：自动测速选择最快节点**
```json
{
  "type": "urltest",
  "tag": "auto-select",
  "outbounds": [
    "vpn-server-1",
    "vpn-server-2",
    "vpn-server-3"
  ],
  "url": "https://www.gstatic.com/generate_204",
  "interval": "10m",
  "tolerance": 50
}
```

---

### 4. 路由配置 (Route)

**作用**：定义不同的流量分配规则（哪些请求走 VPN，哪些直连）

#### 4.1 关键参数

| 参数 | 类型 | 说明 | VPN 应用 |
|------|------|------|---------|
| `rules` | array | 路由规则列表，按顺序匹配 | 核心功能，按优先级定义流量走向 |
| `final` | string | 默认出站标签 | 未被任何规则匹配的流量 |
| `auto_detect_interface` | bool | 自动检测网卡 | 多网卡或 VPN 环境建议开启 |
| `default_interface` | string | 强制使用的网卡 | 透明代理时指定物理网卡 |

#### 4.2 路由规则 (Rule) 详解

**规则匹配条件**：

| 匹配条件 | 说明 | VPN 应用 | 示例 |
|---------|------|---------|------|
| `domain` | 完全域名匹配 | 按域名分流 | `["google.com"]` |
| `domain_prefix` | 域名前缀 | 子域名匹配 | `["api", "cdn"]` |
| `domain_regex` | 域名正则 | 复杂匹配 | `"^(mail\|maps)\.google\.com$"` |
| `geosite` | 聚合域名库 | 国家地区分流 | `"cn"`(国内) / `"geolocation-!cn"`(非国内) |
| `ip_is_private` | 内网 IP | 局域网识别 | 常用于排除内网流量 |
| `ip_cidr` | IP 段匹配 | 特定 IP 范围 | `"10.0.0.0/8"` |
| `geoip` | 地理位置库 | 按 IP 地区分流 | `"cn"`(国内)/`"us"`(美国) |
| `port` | 端口匹配 | DNS/特定协议 | `53`(DNS) / `80,443`(HTTP(S)) |
| `process_name` | 进程名 | 按应用分流 | `["chrome", "firefox"]` |
| `protocol` | 协议类型 | 按通信协议 | `"http"` / `"tcp"` / `"udp"` |

**规则动作 (Action)**：

| 动作 | 说明 | VPN 应用 |
|------|------|---------|
| `route` | 路由到指定出站 | `{"outbound": "vpn-out"}` |
| `reject`/`block` | 阻止连接 | 广告过滤、黑名单 |
| `hijack-dns` | DNS 劫持 | 防止 DNS 污染 |
| `return` | 返回解析结果 | DNS 规则中使用 |

#### 4.3 VPN 常见路由配置

**场景 1：一键全部走 VPN（代理所有流量）**
```json
{
  "route": {
    "rules": [
      // 1. DNS 流量劫持交给 sing-box DNS
      {
        "port": 53,
        "action": "hijack-dns"
      },
      // 2. 内网流量直连
      {
        "ip_is_private": true,
        "action": "route",
        "outbound": "direct-out"
      }
    ],
    "final": "vpn-out"  // 默认走 VPN
  }
}
```

**场景 2：分流（某些走 VPN，某些直连）**
```json
{
  "route": {
    "rules": [
      // DNS 劫持
      {
        "port": 53,
        "action": "hijack-dns"
      },
      // 国内网站直连
      {
        "geosite": "cn",
        "action": "route",
        "outbound": "direct-out"
      },
      // 国内 IP 直连
      {
        "geoip": "cn",
        "action": "route",
        "outbound": "direct-out"
      },
      // 某些域名走 VPN
      {
        "domain": ["google.com", "facebook.com", "twitter.com"],
        "action": "route",
        "outbound": "vpn-out"
      },
      // 广告拦截
      {
        "domain_regex": "^ads\\..*",
        "action": "block"
      }
    ],
    "final": "vpn-out",  // 其他默认走 VPN
    "auto_detect_interface": true
  }
}
```

**场景 3：按应用分流**
```json
{
  "route": {
    "rules": [
      {
        "port": 53,
        "action": "hijack-dns"
      },
      // 国内 App 直连
      {
        "process_name": ["WeChat", "QQ", "Alipay"],
        "action": "route",
        "outbound": "direct-out"
      },
      // 其他 App 走 VPN
      {
        "process_name": ["Chrome", "Firefox"],
        "action": "route",
        "outbound": "vpn-out"
      }
    ],
    "final": "direct-out",
    "find_process": true  // **必须启用才能识别进程**
  }
}
```

---

### 5. DNS 配置 (DNS)

**作用**：内置 DNS 服务，解决 DNS 污染、支持 DNS 分流、FakeIP 等

#### 5.1 关键参数

| 参数 | 类型 | 说明 | VPN 应用 |
|------|------|------|---------|
| `servers` | array | DNS 服务器列表 | 配置多个 DNS 服务器实现分流 |
| `rules` | array | DNS 规则 | 按域名或规则选择不同 DNS 服务器 |
| `final` | string | 默认 DNS 服务器标签 | 未被规则匹配的域名查询 |
| `fakeip` | object | FakeIP 配置 | 本地 IP 生成，加快分流速度 |
| `cache_capacity` | int | DNS 缓存容量 | 默认 4096，越大越好（消耗内存） |
| `disable_cache` | bool | 禁用缓存 | 调试时设为 true，生产环保 false |

#### 5.2 DNS 服务器类型

| 类型 | 说明 | 优点 | 缺点 | VPN 应用 |
|------|------|------|------|---------|
| `local` | 系统 DNS | - | 易被污染 | 不推荐 |
| `udp` | 普通 UDP DNS | 快速 | 易被污染/拦截 | 国内不推荐 |
| `tcp` | TCP DNS | 较安全 | 速度稍慢 | 备选方案 |
| `tls` | DoT | 加密安全 | 需要 TLS | **推荐** |
| `https` | DoH | 加密安全 | 需要 HTTPS | **首选** |
| `quic` | DoQ | 超快加密 | 兼容性差 | 试验性 |

#### 5.3 VPN 常见 DNS 配置

**场景 1：配置国际公共 DNS（防污染）**
```json
{
  "dns": {
    "servers": [
      {
        "tag": "cloudflare",
        "type": "https",
        "address": "https://1.1.1.1/dns-query",
        "strategy": "prefer_h2"
      },
      {
        "tag": "google",
        "type": "https",
        "address": "https://8.8.8.8/dns-query"
      },
      {
        "tag": "local",
        "type": "udp",
        "address": "223.5.5.5:53"  // 国内 DNS（快速）
      }
    ],
    "rules": [
      {
        "domain_suffix": ".cn",
        "server": "local"  // 国内域名用国内 DNS
      },
      {
        "domain_regex": "^(google|facebook)",
        "server": "google"  // 特定域名用 Google DNS
      }
    ],
    "final": "cloudflare",  // 默认用 Cloudflare
    "cache_capacity": 8192
  }
}
```

**场景 2：启用 FakeIP（加速 DNS 分流）**
```json
{
  "dns": {
    "servers": [
      {
        "tag": "proxy-dns",
        "type": "https",
        "address": "https://8.8.8.8/dns-query"
      },
      {
        "tag": "local-dns",
        "type": "udp",
        "address": "223.5.5.5:53"
      }
    ],
    "fakeip": {
      "enabled": true,
      "inet4_range": "198.18.0.0/15",
      "inet6_range": "fc00::/18"
    },
    "strategy": "prefer_ipv4"
  }
}
```

---

## VPN 中的具体应用

### 应用 1：基础家庭代理服务器

```json
{
  "log": {
    "level": "warn"
  },
  "dns": {
    "servers": [
      {
        "tag": "remote-dns",
        "type": "https",
        "address": "https://8.8.8.8/dns-query"
      }
    ],
    "final": "remote-dns"
  },
  "inbounds": [
    {
      "type": "mixed",
      "listen": "0.0.0.0",
      "listen_port": 1080,
      "tag": "inbound"
    }
  ],
  "outbounds": [
    {
      "type": "direct",
      "tag": "direct"
    }
  ],
  "route": {
    "final": "direct"
  }
}
```

### 应用 2：VPN 客户端（分流翻墙）

```json
{
  "log": {
    "level": "info"
  },
  "dns": {
    "servers": [
      {
        "tag": "dns-int",
        "type": "https",
        "address": "https://8.8.8.8/dns-query"
      },
      {
        "tag": "dns-cn",
        "type": "udp",
        "address": "223.5.5.5:53"
      }
    ],
    "rules": [
      {
        "domain_suffix": ".cn",
        "server": "dns-cn"
      }
    ],
    "final": "dns-int",
    "strategy": "prefer_ipv4",
    "cache_capacity": 8192
  },
  "inbounds": [
    {
      "type": "mixed",
      "listen": "127.0.0.1",
      "listen_port": 1080
    }
  ],
  "outbounds": [
    {
      "type": "direct",
      "tag": "direct"
    },
    {
      "type": "vless",
      "tag": "vpn",
      "server": "your-vpn-server.com",
      "server_port": 443,
      "uuid": "your-uuid-here",
      "tls": {
        "enabled": true,
        "server_name": "your-vpn-server.com"
      }
    }
  ],
  "route": {
    "rules": [
      {
        "port": 53,
        "action": "hijack-dns"
      },
      {
        "geoip": "cn",
        "action": "route",
        "outbound": "direct"
      },
      {
        "geosite": "cn",
        "action": "route",
        "outbound": "direct"
      }
    ],
    "final": "vpn",
    "auto_detect_interface": true
  }
}
```

---

## 常见协议详解

### Shadowsocks (SS)

**特点**：轻量级，性能快，是公认的"轻量加密传输"标杆

**参数**：
```json
{
  "type": "shadowsocks",
  "method": "2022-blake3-aes-128-gcm",  // 2022 系列最新最安全
  "password": "generated-by-sing-box"
}
```

**安全等级**（推荐顺序）：
1. `2022-blake3-aes-256-gcm` - 最安全
2. `2022-blake3-aes-128-gcm` - 兼容性好
3. `chacha20-ietf-poly1305` - 轻量快速

### VLESS

**特点**：无状态轻量协议，superb 性能，是 V2Ray 生态中性能最高的

**关键参数**：
| 参数 | 说明 | VPN 应用 |
|------|------|---------|
| `uuid` | 用户 ID | 每个用户唯一 |
| `encryption` | 加密类型 | 通常为 `"none"`（下层 TLS 加密） |
| `flow` | 流控算法 | `""`/`"xtls-rprx-vision"`/`"xtls-rprx-splice"` |

### Trojan

**特点**：伪装 TLS，看起来就是正常 HTTPS 流量，不易被识别

```json
{
  "type": "trojan",
  "password": "your-password",
  "network": "tcp",
  "tls": {
    "enabled": true,
    "certificate_path": "/etc/trojan/cert.pem",
    "key_path": "/etc/trojan/key.pem"
  }
}
```

### Hysteria2

**特点**：基于 QUIC，速度超快，带宽利用率最高，特别适合长距离链路

```json
{
  "type": "hysteria2",
  "listen": "0.0.0.0",
  "listen_port": 443,
  "obfs": {
    "type": "salamander",
    "password": "obfs-password"
  },
  "tls": {
    "enabled": true,
    "certificate_path": "/etc/hysteria/cert.pem",
    "key_path": "/etc/hysteria/key.pem"
  },
  "auth": "password",
  "auth_password": "vpn-password"
}
```

---

## 最佳实践

### ✅ DO（推荐做法）

1. **使用 TLS/HTTPS DNS** - 防止 DNS 污染
   ```json
   "type": "https",
   "address": "https://8.8.8.8/dns-query"
   ```

2. **启用 FakeIP** - 加快 DNS 分流效率
3. **配置多 DNS 分流** - 国内用国内 DNS，国外用 DOH
4. **使用 Geosite/Geoip** - 充分利用数据库实现分流
5. **生产环境开启 auto_detect_interface** - 多网卡友好

### ❌ DON'T（避免做法）

1. ❌ 不要用明文 UDP DNS - 易被污染/劫持
2. ❌ 不要在生产环境用 debug 日志 - 性能下降
3. ❌ 不要忘记开启 find_process - 按应用分流无效
4. ❌ 不要配置过多规则 - 性能下降，单个规则用正则表达式
5. ❌ 不要信任不安全的 TLS cert - insecure 仅用于调试

### 验证配置正确性

```bash
# 检查配置语法
sing-box check -c config.json

# 格式化配置文件
sing-box format -w -c config.json

# 合并多个配置文件
sing-box merge output.json -c config.json -D config_directory

# 启用 debug 日志调试
sing-box run -c config.json -D config_directory
```

---

## 相关命令

```bash
# 启动 sing-box
sing-box run -c config.json

# 后台运行
nohup sing-box run -c config.json > /var/log/sing-box.log 2>&1 &

# 查看运行状态
curl http://127.0.0.1:9090/traffic  # 需要启用 Clash API

# 测试连接
curl -x http://127.0.0.1:1080 https://www.google.com
```

---

**最后更新**：2024 年，基于 sing-box v1.13.3
