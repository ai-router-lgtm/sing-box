# Sing-Box 高级配置与故障排查

## 📋 目录
1. [高级配置场景](#高级配置场景)
2. [常见问题与解决](#常见问题与解决)
3. [性能优化建议](#性能优化建议)
4. [安全配置最佳实践](#安全配置最佳实践)

---

## 高级配置场景

### 场景 1：企业网络透明代理（Linux）

**需求**：所有流量自动代理，无需客户端配置

```json
{
  "log": {
    "level": "warn"
  },
  "dns": {
    "servers": [
      {
        "tag": "proxy",
        "type": "https",
        "address": "https://8.8.8.8/dns-query"
      }
    ],
    "final": "proxy"
  },
  "inbounds": [
    {
      "type": "redirect",
      "tag": "redirect-tcp",
      "listen": "0.0.0.0",
      "listen_port": 1080,
      "network": "tcp"
    },
    {
      "type": "tproxy",
      "tag": "tproxy-udp",
      "listen": "0.0.0.0",
      "listen_port": 1080,
      "network": "udp"
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
      "uuid": "your-uuid"
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
      }
    ],
    "final": "vpn",
    "auto_detect_interface": true
  }
}
```

**启用透明代理的 iptables 规则**：
```bash
# TCP 重定向
iptables -t nat -A PREROUTING -p tcp --dport 1:65535 -j REDIRECT --to-ports 1080

# UDP 重定向
ip rule add fwmark 1 table 100
ip route add local 0/0 dev lo table 100
iptables -t mangle -A PREROUTING -p udp --dport 1:65535 -j MARK --set-mark 1
iptables -t mangle -A OUTPUT -p udp -j MARK --set-mark 1
```

---

### 场景 2：浮动 IP 自动切换

**需求**：服务器 IP 变更时自动更新（DynDNS 场景）

```json
{
  "log": {
    "level": "info"
  },
  "dns": {
    "servers": [
      {
        "tag": "resolver",
        "type": "https",
        "address": "https://8.8.8.8/dns-query"
      }
    ],
    "final": "resolver"
  },
  "inbounds": [
    {
      "type": "vless",
      "tag": "vless-in",
      "listen": "::",
      "listen_port": 443,
      "users": [
        {
          "uuid": "your-uuid"
        }
      ],
      "tls": {
        "enabled": true,
        "certificate_path": "/etc/sing-box/cert.pem",
        "key_path": "/etc/sing-box/key.pem"
      }
    }
  ],
  "outbounds": [
    {
      "type": "direct",
      "tag": "direct",
      "bind_interface": "eth0",
      "bind_to_address": "auto"
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

---

### 场景 3：多协议负载均衡与故障转移

**需求**：多个协议同时运行，自动选择最快

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
      "type": "vless",
      "tag": "vless-in",
      "listen": "::",
      "listen_port": 443,
      "users": [
        {"uuid": "uuid-1"}
      ],
      "tls": {
        "enabled": true,
        "certificate_path": "/etc/sing-box/cert.pem",
        "key_path": "/etc/sing-box/key.pem"
      }
    },
    {
      "type": "hysteria2",
      "tag": "hysteria2-in",
      "listen": "::",
      "listen_port": 443,
      "obfs": {
        "type": "salamander",
        "password": "obfs-pwd"
      },
      "auth": {
        "type": "password",
        "password": "main-pwd"
      },
      "tls": {
        "enabled": true,
        "certificate_path": "/etc/sing-box/cert.pem",
        "key_path": "/etc/sing-box/key.pem"
      }
    },
    {
      "type": "trojan",
      "tag": "trojan-in",
      "listen": "::",
      "listen_port": 443,
      "password": ["trojan-pwd"],
      "tls": {
        "enabled": true,
        "certificate_path": "/etc/sing-box/cert.pem",
        "key_path": "/etc/sing-box/key.pem"
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
        "port": 53,
        "action": "hijack-dns"
      }
    ],
    "final": "direct"
  }
}
```

---

### 场景 4：广告拦截与恶意网站防护

**需求**：在代理层面过滤广告和恶意网站

```json
{
  "log": {
    "level": "info"
  },
  "dns": {
    "servers": [
      {
        "tag": "proxy",
        "type": "https",
        "address": "https://8.8.8.8/dns-query"
      }
    ],
    "rules": [
      {
        "domain_regex": [
          "^ads\\\\.",
          "^tracker\\\\.",
          "^analytics\\\\.",
          "^metrics\\\\.",
          "doubleclick\\\\.net"
        ],
        "action": "block"
      }
    ],
    "final": "proxy",
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
      "type": "block",
      "tag": "block"
    }
  ],
  "route": {
    "rules": [
      {
        "port": 53,
        "action": "hijack-dns"
      },
      {
        "domain_regex": [
          "ad\\\\.google\\\\.com",
          "doubleclick\\\\.net"
        ],
        "action": "route",
        "outbound": "block"
      },
      {
        "geosite": "malware",
        "action": "route",
        "outbound": "block"
      }
    ],
    "final": "direct"
  }
}
```

---

## 常见问题与解决

### ❓ 问题 1：DNS 污染导致无法访问特定网站

**症状**：
- 某些国外网站无法打开
- DNS 解析返回错误 IP（如 127.0.0.1）
- `nslookup` 返回与其他工具不同的结果

**原因**：
- ISP 对特定域名 DNS 进行了污染
- 本地 DNS 缓存中毒

**解决方案**：

```json
{
  "dns": {
    "servers": [
      {
        "tag": "clean-dns",
        "type": "https",
        "address": "https://8.8.8.8/dns-query",
        "client_subnet": "1.1.1.1/24",
        "strategy": "prefer_ipv4"
      }
    ],
    "final": "clean-dns",
    "disable_cache": false,
    "reverse_mapping": true
  },
  "route": {
    "rules": [
      {
        "port": 53,
        "action": "hijack-dns"
      }
    ]
  }
}
```

**排查步骤**：
```bash
# 1. 检查本地 DNS 解析
nslookup google.com 127.0.0.1:1080

# 2. 查看 DNS 日志
sing-box run -c config.json 2>&1 | grep dns

# 3. 用 dig 测试
dig @8.8.8.8 google.com

# 4. 清空 DNS 缓存
# 在配置文件中设置 "disable_cache": false 并重启
```

---

### ❓ 问题 2：连接超时（Connection Timeout）

**症状**：
- `curl: (7) Failed to connect`
- 定期断开连接
- 某些时间段特别慢

**原因**：
- 服务器响应慢
- ISP 限流或丢包
- 连接超时设置过短

**解决方案**：

```json
{
  "outbounds": [
    {
      "type": "vless",
      "tag": "vpn",
      "server": "vpn.example.com",
      "server_port": 443,
      "uuid": "your-uuid",
      "connect_timeout": "10s",  // 增加超时时间
      "tls": {
        "enabled": true
      },
      "tcp_keep_alive": {
        "enabled": true,
        "idle_timeout": "300s"
      }
    }
  ]
}
```

**排查步骤**：
```bash
# 1. 测试 TCP 连接
nc -zv vpn.example.com 443

# 2. 测试 TLS 握手
openssl s_client -connect vpn.example.com:443

# 3. 检查网络延迟
ping -c 4 vpn.example.com
mtr vpn.example.com

# 4. 查看连接状态
ss -tnap | grep sing-box
```

---

### ❓ 问题 3：分流不生效

**症状**：
- 所有流量都走 VPN（分流规则未应用）
- 设置优先级错误
- 规则写法不正确

**原因**：
- 规则顺序不对（应该从严格到宽松）
- 规则类型写错（`domain` vs `domain_suffix`）
- 未开启必要的选项（如 `find_process`）

**解决方案**：

```json
{
  "route": {
    "rules": [
      // 1. 首先处理 DNS
      {
        "port": 53,
        "action": "hijack-dns"
      },
      // 2. 然后是最严格的规则（直连内网）
      {
        "ip_is_private": true,
        "action": "route",
        "outbound": "direct"
      },
      // 3. 再处理地理位置（国内走直连）
      {
        "geoip": "cn",
        "action": "route",
        "outbound": "direct"
      },
      {
        "geosite": "cn",
        "action": "route",
        "outbound": "direct"
      },
      // 4. 最后是包含规则（特定站点走 VPN）
      {
        "domain": ["google.com", "github.com"],
        "action": "route",
        "outbound": "vpn"
      }
    ],
    "final": "vpn",  // 默认走 VPN
    "find_process": true,  // 需要启用才能按进程分流
    "auto_detect_interface": true
  }
}
```

**调试技巧**：
```bash
# 启用 debug 日志查看规则匹配
"log": {
  "level": "debug"
}

# 使用 curl 测试带 User-Agent 的请求
curl -x socks5h://127.0.0.1:1080 -A "Mozilla/5.0" https://google.com

# 查看实时流量
sing-box run -c config.json 2>&1 | grep -E "route|rule"
```

---

### ❓ 问题 4：FakeIP 导致某些应用无法工作

**症状**：
- 某些应用连接失败
- DNS 解析正常但访问超时
- 命令行工具（如 SSH）无法使用

**原因**：
- FakeIP 返回的地址与应用期望的真实 IP 冲突
- 应用需要真实 IP 地址

**解决方案**：

```json
{
  "dns": {
    "servers": [
      {
        "tag": "remote",
        "type": "https",
        "address": "https://8.8.8.8/dns-query"
      }
    ],
    "fakeip": {
      "enabled": true,
      "inet4_range": "198.18.0.0/15",  // 保留范围
      "inet6_range": "fc00::/18"
    },
    "rules": [
      // SSH 等协议不使用 FakeIP，用真实 IP
      {
        "protocol": "tcp",
        "port": [22, 3389],
        "server": "direct-dns"
      },
      // 数据库连接用真实 IP
      {
        "port": [3306, 5432, 27017],
        "server": "direct-dns"
      }
    ]
  }
}
```

---

## 性能优化建议

### 1. DNS 缓存优化

```json
{
  "dns": {
    "cache_capacity": 16384,      // 增大缓存容量（消耗更多内存）
    "disable_expire": false,       // 保持缓存过期机制
    "independent_cache": false,    // 关闭独立缓存节省内存
    "disable_cache": false         // 启用缓存
  }
}
```

**影响**：
| 参数 | 增大影响 | 典型值 |
|------|---------|--------|
| cache_capacity | 内存 ↑ 速度 ↑ | 4096-16384 |
| disable_cache | true=速度↓但准确 | false |

---

### 2. 连接优化

```json
{
  "inbounds": [
    {
      "type": "mixed",
      "tcp_fast_open": true,        // 启用 TCP 快速打开
      "tcp_keep_alive": {
        "enabled": true,
        "idle_timeout": "300s"
      },
      "udp_timeout": "5m"           // UDP 超时时间
    }
  ],
  "outbounds": [
    {
      "type": "vless",
      "connect_timeout": "5s",
      "tcp_fast_open": true
    }
  ]
}
```

---

### 3. 规则优化

```json
{
  "route": {
    "rules": [
      // ❌ 低效：多个单独的规则
      {"domain": ["google.com"], "outbound": "vpn"},
      {"domain": ["facebook.com"], "outbound": "vpn"},
      {"domain": ["twitter.com"], "outbound": "vpn"},

      // ✅ 高效：合并为数组
      {
        "domain": ["google.com", "facebook.com", "twitter.com"],
        "outbound": "vpn"
      },

      // ✅ 最高效：使用 geosite 库
      {
        "geosite": "geolocation-!cn",
        "outbound": "vpn"
      }
    ]
  }
}
```

---

### 4. 日志级别选择

```json
{
  "log": {
    // 生产环境（内存占用最少）
    "level": "warn",           // 仅显示警告和错误

    // 调试环境
    "level": "debug",          // 显示调试信息

    // 排查问题
    "level": "trace"           // 显示所有细节（性能下降）
  }
}
```

---

## 安全配置最佳实践

### 1. TLS 证书安全

```json
{
  "inbounds": [
    {
      "type": "vless",
      "tls": {
        "enabled": true,
        "certificate_path": "/etc/sing-box/cert.pem",
        "key_path": "/etc/sing-box/key.pem",
        "min_version": "1.2",           // 仅支持 TLS 1.2+
        "max_version": "1.3",           // 最高使用 TLS 1.3
        "cipher_suites": [              // 强加密套件
          "TLS_CHACHA20_POLY1305_SHA256",
          "TLS_AES_256_GCM_SHA384",
          "TLS_AES_128_CCM_8_SHA256"
        ],
        "curve_preferences": [          // 椭圆曲线
          "x25519",
          "secp256r1",
          "secp384r1"
        ]
      }
    }
  ]
}
```

---

### 2. 用户认证安全

```json
{
  "inbounds": [
    {
      "type": "shadowsocks",
      "method": "2022-blake3-aes-256-gcm",  // 最新的 2022 方法
      "password": "generated-randomly",      // 用 sing-box 生成
      "users": [
        {
          "name": "user1",
          "password": "unique-strong-password-32-chars"
        }
      ]
    }
  ]
}
```

**生成安全密钥**：
```bash
# 生成 32 字节的随机密钥
sing-box generate rand --base64 32

# 生成 UUID
sing-box generate uuid

# 生成密码
openssl rand -base64 32
```

---

### 3. 防止信息泄露

```json
{
  "log": {
    //❌ 不要在生产环境输出日志到 stdout
    "output": "/var/log/sing-box/access.log",  // ✅ 输出到文件
    "level": "warn",                            // ✅ 日志级别最低

    "timestamp": true                           // ✅ 便于审计
  },
  "inbounds": [
    {
      "type": "vless",
      "users": [
        {
          "uuid": "b831381d-6324-4d53-ad4f-8cda48b30811"
          // ❌ 不要在配置中硬编码密码或关键信息
        }
      ]
    }
  ]
}
```

---

### 4. 防火墙配置

```bash
# 仅允许特定 IP 连接
iptables -A INPUT -p tcp --dport 443 -s 允许的IP -j ACCEPT
iptables -A INPUT -p tcp --dport 443 -j DROP

# 限制连接速率（防止 DDoS）
iptables -A INPUT -p tcp --dport 443 -m limit --limit 25/minute --limit-burst 100 -j ACCEPT
iptables -A INPUT -p tcp --dport 443 -j DROP

# 启用 SYN Cookies（防止 SYN 洪泛）
sysctl -w net.ipv4.tcp_syncookies=1
```

---

## 🚀 快速参考

### 最小化配置（快速测试）

```json
{
  "inbounds": [{
    "type": "mixed",
    "listen": "127.0.0.1",
    "listen_port": 1080
  }],
  "outbounds": [{
    "type": "direct",
    "tag": "direct"
  }],
  "route": {
    "final": "direct"
  }
}
```

### 完整安全配置

```json
{
  "log": {
    "level": "warn",
    "output": "/var/log/sing-box.log"
  },
  "dns": {
    "servers": [
      {
        "tag": "default",
        "type": "https",
        "address": "https://8.8.8.8/dns-query"
      }
    ],
    "final": "default",
    "cache_capacity": 8192
  },
  "inbounds": [{
    "type": "mixed",
    "listen": "127.0.0.1",
    "listen_port": 1080
  }],
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
      "uuid": "uuid-here",
      "tls": {"enabled": true}
    }
  ],
  "route": {
    "rules": [
      {"port": 53, "action": "hijack-dns"},
      {"geoip": "cn", "outbound": "direct"}
    ],
    "final": "vpn",
    "auto_detect_interface": true
  }
}
```

---

**最后更新**：2024 年 3 月，基于 sing-box v1.13.3
