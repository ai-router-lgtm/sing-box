# Sing-Box v1.13.3 前后端完整模板

## 📋 目录
1. [服务器端 (Server Configurations)](#服务器端-server-configurations)
2. [客户端 (Client Configurations)](#客户端-client-configurations)
3. [协议对比与选择](#协议对比与选择)

---

## 服务器端 (Server Configurations)

### ① VLESS 服务器完整配置

**特点**：轻量级、高性能、支持 TLS 和 XTLS，是最推荐的协议

<details>
<summary>📋 server-vless-full.json</summary>

```json
{
  "log": {
    "level": "warn",
    "timestamp": true,
    "output": "/var/log/sing-box/vless.log"
  },
  "dns": {
    "servers": [
      {
        "tag": "remote-dns",
        "type": "https",
        "address": "https://8.8.8.8/dns-query",
        "strategy": "prefer_h2"
      }
    ],
    "final": "remote-dns",
    "cache_capacity": 8192,
    "disable_expire": false
  },
  "inbounds": [
    {
      "type": "vless",
      "tag": "vless-in",
      "listen": "::",
      "listen_port": 443,
      "network": "tcp",
      "users": [
        {
          "uuid": "b831381d-6324-4d53-ad4f-8cda48b30811",
          "flow": "xtls-rprx-vision"
        },
        {
          "uuid": "c942492e-7435-5e64-be5g-9deb59c41922",
          "flow": "xtls-rprx-vision"
        }
      ],
      "tls": {
        "enabled": true,
        "certificate_path": "/etc/sing-box/cert.pem",
        "key_path": "/etc/sing-box/key.pem",
        "min_version": "1.2",
        "max_version": "1.3",
        "cipher_suites": [
          "TLS_CHACHA20_POLY1305_SHA256",
          "TLS_AES_256_GCM_SHA384",
          "TLS_AES_128_GCM_SHA256"
        ]
      },
      "tcp_keep_alive": {
        "enabled": true,
        "idle_timeout": "300s",
        "probe_interval": "30s"
      }
    }
  ],
  "outbounds": [
    {
      "type": "direct",
      "tag": "direct"
    },
    {
      "type": "block",
      "tag": "block"
    }
  ],
  "route": {
    "rules": [
      {
        "outbound": "dns-out",
        "port": 53
      },
      {
        "outbound": "block",
        "domain_regex": [
          "^ads\\\\..*",
          "^tracker\\\\..*"
        ]
      }
    ],
    "final": "direct",
    "auto_detect_interface": true,
    "override_android_vpn": true
  }
}
```

</details>

### ② Hysteria2 服务器完整配置

**特点**：基于 QUIC，超高速，带宽利用率最高，最适合长距离链路

<details>
<summary>📋 server-hysteria2-full.json</summary>

```json
{
  "log": {
    "level": "warn",
    "timestamp": true,
    "output": "/var/log/sing-box/hysteria2.log"
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
      "type": "hysteria2",
      "tag": "hysteria2-in",
      "listen": "::",
      "listen_port": 443,
      "network": "tcp,udp",
      "up": "100 Mbps",
      "down": "100 Mbps",
      "obfs": {
        "type": "salamander",
        "password": "your-obfs-password-here"
      },
      "masquerade": "https://www.bing.com",
      "auth": {
        "type": "password",
        "password": "your-first-user-password"
      },
      "users": [
        {
          "name": "user1",
          "password": "user1-password-here"
        },
        {
          "name": "user2",
          "password": "user2-password-here"
        }
      ],
      "tls": {
        "enabled": true,
        "certificate_path": "/etc/sing-box/cert.pem",
        "key_path": "/etc/sing-box/key.pem",
        "alpn": ["h3"]
      },
      "udp_timeout": "300s"
    }
  ],
  "outbounds": [
    {
      "type": "direct",
      "tag": "direct"
    }
  ],
  "route": {
    "rules": [
      {
        "port": 53,
        "action": "hijack-dns"
      }
    ],
    "final": "direct",
    "auto_detect_interface": true
  }
}
```

</details>

### ③ Shadowsocks 服务器完整配置

**特点**：轻量、稳定、兼容性强，支持多用户

<details>
<summary>📋 server-shadowsocks-full.json</summary>

```json
{
  "log": {
    "level": "info"
  },
  "dns": {
    "servers": [
      {
        "tag": "remote",
        "type": "https",
        "address": "https://8.8.8.8/dns-query"
      }
    ],
    "final": "remote"
  },
  "inbounds": [
    {
      "type": "shadowsocks",
      "tag": "ss-in",
      "listen": "::",
      "listen_port": 8080,
      "network": "tcp,udp",
      "method": "2022-blake3-aes-256-gcm",
      "password": "8JCsPssfgS8tiRwiMlhARg==",
      "users": [
        {
          "name": "user1",
          "password": "PCD2Z4o12bKUoFa3cC97Hw=="
        },
        {
          "name": "user2",
          "password": "QDE3a4_2cLVgpSmdEd98Ix=="
        }
      ],
      "multiplex": {
        "enabled": true,
        "padding": true,
        "max_connections": 8
      }
    }
  ],
  "outbounds": [
    {
      "type": "direct",
      "tag": "direct"
    }
  ],
  "route": {
    "rules": [
      {
        "protocol": "dns",
        "action": "hijack-dns"
      }
    ],
    "final": "direct"
  }
}
```

</details>

### ④ Trojan 服务器完整配置

**特点**：伪装为 HTTPS，难以被识别和阻止

<details>
<summary>📋 server-trojan-full.json</summary>

```json
{
  "log": {
    "level": "info"
  },
  "dns": {
    "servers": [
      {
        "tag": "remote",
        "type": "https",
        "address": "https://8.8.8.8/dns-query"
      }
    ],
    "final": "remote"
  },
  "inbounds": [
    {
      "type": "trojan",
      "tag": "trojan-in",
      "listen": "::",
      "listen_port": 443,
      "network": "tcp",
      "password": [
        "your-password-1",
        "your-password-2"
      ],
      "transport": {
        "type": "http",
        "host": [
          "www.example.com",
          "www.anothersite.com"
        ]
      },
      "tls": {
        "enabled": true,
        "certificate_path": "/etc/sing-box/cert.pem",
        "key_path": "/etc/sing-box/key.pem",
        "alpn": ["http/1.1"]
      }
    }
  ],
  "outbounds": [
    {
      "type": "direct",
      "tag": "direct"
    }
  ],
  "route": {
    "rules": [],
    "final": "direct"
  }
}
```

</details>

---

## 客户端 (Client Configurations)

### ① VPN 客户端：分流配置

**场景**：某些站点通过 VPN，国内站点直连（最常用）

<details>
<summary>📋 client-split-tunnel.json</summary>

```json
{
  "log": {
    "level": "info"
  },
  "dns": {
    "servers": [
      {
        "tag": "proxy-dns",
        "type": "https",
        "address": "https://8.8.8.8/dns-query",
        "strategy": "prefer_h2",
        "detour": "vpn-out"
      },
      {
        "tag": "direct-dns",
        "type": "udp",
        "address": "223.5.5.5:53",
        "strategy": "prefer_ipv4"
      }
    ],
    "rules": [
      {
        "domain_suffix": [".cn"],
        "server": "direct-dns"
      },
      {
        "geoip": "cn",
        "server": "direct-dns"
      },
      {
        "domain_suffix": [
          ".google.com",
          ".github.com",
          ".youtube.com"
        ],
        "server": "proxy-dns"
      }
    ],
    "final": "proxy-dns",
    "strategy": "prefer_ipv4",
    "disable_cache": false,
    "cache_capacity": 8192
  },
  "inbounds": [
    {
      "type": "mixed",
      "tag": "mixed-in",
      "listen": "127.0.0.1",
      "listen_port": 1080,
      "network": "tcp",
      "tcp_fast_open": true
    },
    {
      "type": "tun",
      "tag": "tun-in",
      "address": [
        "172.19.0.1/30",
        "fd00::1/126"
      ],
      "auto_route": true,
      "auto_route_exclude_intranets": true,
      "strict_route": false,
      "stack": "mixed",
      "platform": {
        "http_proxy": {
          "enabled": true,
          "listen": "127.0.0.1",
          "listen_port": 1081
        }
      }
    }
  ],
  "outbounds": [
    {
      "type": "direct",
      "tag": "direct-out"
    },
    {
      "type": "vless",
      "tag": "vpn-out",
      "server": "your-vpn-server.com",
      "server_port": 443,
      "uuid": "b831381d-6324-4d53-ad4f-8cda48b30811",
      "flow": "xtls-rprx-vision",
      "network": "tcp",
      "tls": {
        "enabled": true,
        "server_name": "your-vpn-server.com",
        "utls": {
          "enabled": true,
          "fingerprint": "chrome"
        }
      },
      "packet_encoding": "xudp",
      "connect_timeout": "10s"
    }
  ],
  "route": {
    "rules": [
      {
        "port": 53,
        "action": "hijack-dns"
      },
      {
        "inbound": "mixed-in",
        "action": "route",
        "outbound": "vpn-out"
      },
      {
        "geoip": "cn",
        "action": "route",
        "outbound": "direct-out"
      },
      {
        "geosite": "cn",
        "action": "route",
        "outbound": "direct-out"
      },
      {
        "network": "udp",
        "port": 443,
        "action": "route",
        "outbound": "vpn-out"
      }
    ],
    "final": "vpn-out",
    "auto_detect_interface": true,
    "default_interface": "en0"
  }
}
```

</details>

### ② VPN 客户端：全部代理配置

**场景**：所有流量都通过 VPN（安全隐私模式）

<details>
<summary>📋 client-full-proxy.json</summary>

```json
{
  "log": {
    "level": "warn"
  },
  "dns": {
    "servers": [
      {
        "tag": "proxy-dns",
        "type": "https",
        "address": "https://8.8.8.8/dns-query",
        "detour": "vpn-out"
      }
    ],
    "final": "proxy-dns",
    "strategy": "prefer_ipv4",
    "cache_capacity": 8192
  },
  "inbounds": [
    {
      "type": "mixed",
      "tag": "local-in",
      "listen": "127.0.0.1",
      "listen_port": 1080,
      "tcp_fast_open": true
    }
  ],
  "outbounds": [
    {
      "type": "vless",
      "tag": "vpn",
      "server": "vpn.example.com",
      "server_port": 443,
      "uuid": "your-uuid",
      "tls": {
        "enabled": true,
        "server_name": "vpn.example.com"
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
        "ip_is_private": true,
        "action": "route",
        "outbound": "direct-out"
      }
    ],
    "final": "vpn",
    "auto_detect_interface": true
  }
}
```

</details>

### ③ VPN 客户端：按应用分流

**场景**：不同应用走不同路线（某些 App 直连国内，某些 App 走 VPN）

<details>
<summary>📋 client-app-split.json</summary>

```json
{
  "log": {
    "level": "info"
  },
  "dns": {
    "servers": [
      {
        "tag": "proxy-dns",
        "type": "https",
        "address": "https://8.8.8.8/dns-query",
        "detour": "vpn-out"
      },
      {
        "tag": "direct-dns",
        "type": "udp",
        "address": "223.5.5.5:53"
      }
    ],
    "rules": [
      {
        "domain": [".cn"],
        "server": "direct-dns"
      }
    ],
    "final": "proxy-dns",
    "cache_capacity": 8192
  },
  "inbounds": [
    {
      "type": "mixed",
      "tag": "local-in",
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
      "server": "vpn.example.com",
      "server_port": 443,
      "uuid": "your-uuid",
      "tls": {
        "enabled": true,
        "server_name": "vpn.example.com"
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
        "process_name": [
          "WeChat",
          "QQ",
          "Alipay"
        ],
        "action": "route",
        "outbound": "direct"
      },
      {
        "process_name": [
          "Chrome",
          "Firefox",
          "Safari"
        ],
        "action": "route",
        "outbound": "vpn"
      },
      {
        "geoip": "cn",
        "action": "route",
        "outbound": "direct"
      }
    ],
    "final": "vpn",
    "find_process": true,
    "auto_detect_interface": true
  }
}
```

</details>

### ④ VPN 客户端：自动测速负载均衡

**场景**：多个 VPN 服务器，自动选择最快的

<details>
<summary>📋 client-lb-urltest.json</summary>

```json
{
  "log": {
    "level": "info"
  },
  "dns": {
    "servers": [
      {
        "tag": "proxy-dns",
        "type": "https",
        "address": "https://8.8.8.8/dns-query",
        "detour": "vpn-lb"
      }
    ],
    "final": "proxy-dns"
  },
  "inbounds": [
    {
      "type": "mixed",
      "tag": "local-in",
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
      "tag": "vpn-server-1",
      "server": "server1.example.com",
      "server_port": 443,
      "uuid": "uuid-1",
      "tls": {
        "enabled": true,
        "server_name": "server1.example.com"
      }
    },
    {
      "type": "vless",
      "tag": "vpn-server-2",
      "server": "server2.example.com",
      "server_port": 443,
      "uuid": "uuid-2",
      "tls": {
        "enabled": true,
        "server_name": "server2.example.com"
      }
    },
    {
      "type": "vless",
      "tag": "vpn-server-3",
      "server": "server3.example.com",
      "server_port": 443,
      "uuid": "uuid-3",
      "tls": {
        "enabled": true,
        "server_name": "server3.example.com"
      }
    },
    {
      "type": "urltest",
      "tag": "vpn-lb",
      "outbounds": [
        "vpn-server-1",
        "vpn-server-2",
        "vpn-server-3"
      ],
      "url": "https://www.gstatic.com/generate_204",
      "interval": "10m",
      "tolerance": 50,
      "idle_timeout": "30m"
    }
  ],
  "route": {
    "rules": [
      {
        "port": 53,
        "action": "hijack-dns"
      },
      {
        "geosite": "cn",
        "action": "route",
        "outbound": "direct"
      }
    ],
    "final": "vpn-lb",
    "auto_detect_interface": true
  }
}
```

</details>

---

## 协议对比与选择

### 📊 性能对比矩阵

| 协议 | 性能 | 安全性 | 隐蔽性 | 兼容性 | 推荐度 | 最佳场景 |
|------|------|--------|--------|--------|--------|---------|
| **VLESS** | ⭐⭐⭐⭐⭐ | ⭐⭐⭐⭐ | ⭐⭐⭐ | ⭐⭐⭐⭐ | 🥇 | 速度优先，通用场景 |
| **Hysteria2** | ⭐⭐⭐⭐⭐ | ⭐⭐⭐⭐ | ⭐⭐⭐ | ⭐⭐ | 🥇 | 高延迟/丢包环境 |
| **Trojan** | ⭐⭐⭐⭐ | ⭐⭐⭐⭐ | ⭐⭐⭐⭐⭐ | ⭐⭐⭐⭐ | 🥈 | 隐蔽性优先 |
| **Shadowsocks** | ⭐⭐⭐⭐ | ⭐⭐⭐ | ⭐⭐⭐ | ⭐⭐⭐⭐⭐ | 🥈 | 轻量快速，兼容性 |
| **VMess** | ⭐⭐⭐ | ⭐⭐⭐ | ⭐⭐ | ⭐⭐⭐ | 🥉 | V2Ray 生态兼容 |

### 🎯 选择建议

**我想要最快速度？**
→ 使用 **VLESS** 或 **Hysteria2**
- VLESS：通用、稳定、速度快
- Hysteria2：高延迟/丢包环境特别优

**我想要最隐蔽？**
→ 使用 **Trojan** 或 **ShadowTLS**
- 伪装为 HTTPS，难以被识别

**我想要最稳定可靠？**
→ 使用 **Shadowsocks** 或 **VLESS**
- 都是经过时间验证的稳定协议

**我想要最容易部署？**
→ 使用 **Shadowsocks**
- 配置简单，部署快速

---

## 🔧 常用命令速查

```bash
# 启动 sing-box
sing-box run -c config.json

# 检查配置语法
sing-box check -c config.json

# 格式化配置文件
sing-box format -w -c config.json

# 合并多个配置文件
sing-box merge output.json -c config.json -D config_directory

# 生成密钥
sing-box generate rand --base64 16   # SS 密钥（16 bytes）
sing-box generate rand --base64 32   # SS 密钥（32 bytes）

# 生成 UUID（VLESS）
sing-box generate uuid
```

---

**最后更新**：2024 年 3 月，以 sing-box v1.13.3 为基准
