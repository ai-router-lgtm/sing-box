# Sing-Box 配置文件快速参考 & 复制即用

## 目录
1. [客户端：混合入站 + 分流出站](#客户端混合入站--分流出站)
2. [客户端：纯代理模式](#客户端纯代理模式)
3. [服务器：VLESS 完整服务](#服务器vless-完整服务)
4. [单文件配置对照表](#单文件配置对照表)

---

## 客户端：混合入站 + 分流出站

**适用场景**：本地 PC/Mac 使用 VPN，实现国内走直连、国外走代理

### config.json

```json
{
  "log": {
    "level": "info",
    "timestamp": true,
    "output": "./"
  },
  "dns": {
    "servers": [
      {
        "tag": "proxy",
        "type": "https",
        "address": "https://8.8.8.8/dns-query",
        "strategy": "prefer_h2",
        "detour": "vless-out"
      },
      {
        "tag": "local",
        "type": "udp",
        "address": "223.5.5.5:53"
      }
    ],
    "rules": [
      {
        "domain_suffix": ".cn",
        "server": "local"
      },
      {
        "geoip": "cn",
        "server": "local"
      }
    ],
    "final": "proxy",
    "strategy": "prefer_ipv4",
    "cache_capacity": 8192,
    "disable_expire": false
  },
  "inbounds": [
    {
      "type": "mixed",
      "tag": "mixed-in",
      "listen": "127.0.0.1",
      "listen_port": 1080,
      "network": "tcp",
      "tcp_fast_open": true,
      "udp_timeout": "5m"
    }
  ],
  "outbounds": [
    {
      "type": "direct",
      "tag": "direct-out",
      "bind_interface": "auto",
      "bind_to_address": "auto"
    },
    {
      "type": "vless",
      "tag": "vless-out",
      "server": "YOUR_SERVER_ADDRESS",
      "server_port": 443,
      "uuid": "YOUR_UUID_HERE",
      "encryption": "none",
      "network": "tcp",
      "flow": "xtls-rprx-vision",
      "tls": {
        "enabled": true,
        "server_name": "YOUR_SERVER_ADDRESS",
        "insecure": false,
        "utls": {
          "enabled": true,
          "fingerprint": "chrome"
        }
      },
      "connect_timeout": "10s",
      "tcp_fast_open": true,
      "packet_encoding": "xudp"
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
        "domain_suffix": [
          ".google.com",
          ".github.com",
          ".youtube.com",
          ".facebook.com",
          ".twitter.com"
        ],
        "action": "route",
        "outbound": "vless-out"
      }
    ],
    "final": "vless-out",
    "auto_detect_interface": true,
    "default_interface": "auto"
  }
}
```

### 快速配置说明

| 字段 | 修改内容 |
|------|---------|
| `YOUR_SERVER_ADDRESS` | 改成你的 VPN 服务器地址或域名 |
| `YOUR_UUID_HERE` | 改成分配给你的 UUID（从服务器获取） |
| `listen_port` | 本地监听端口（默认 1080） |
| `server_port` | VPN 服务器端口，通常 443、8443 或自定义端口 |

### 使用方法

```bash
# 1. 下载 sing-box 二进制
# 从 https://github.com/SagerNet/sing-box/releases 下载对应系统版本

# 2. 检查配置
./sing-box check -c config.json

# 3. 运行
./sing-box run -c config.json

# 4. 配置客户端代理
# 代理地址：127.0.0.1:1080 (Socks5 或 HTTP)
# 在浏览器/应用中设置代理即可
```

---

## 客户端：纯代理模式

**适用场景**：所有流量都走 VPN（最小化配置）

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
        "address": "https://8.8.8.8/dns-query",
        "detour": "vless-out"
      }
    ],
    "final": "remote-dns"
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
      "type": "vless",
      "tag": "vless-out",
      "server": "YOUR_VPN_SERVER",
      "server_port": 443,
      "uuid": "YOUR_UUID",
      "tls": {
        "enabled": true,
        "server_name": "YOUR_VPN_SERVER"
      }
    }
  ],
  "route": {
    "rules": [
      {
        "port": 53,
        "action": "hijack-dns"
      }
    ],
    "final": "vless-out"
  }
}
```

---

## 服务器：VLESS 完整服务

**适用场景**：自建 VPN 服务器，提供给多个客户端访问

### config.json

```json
{
  "log": {
    "level": "warn",
    "timestamp": true,
    "output": "/var/log/sing-box/server.log"
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
  "certificate": [
    {
      "certificate_path": "/path/to/cert.pem",
      "key_path": "/path/to/key.pem"
    }
  ],
  "inbounds": [
    {
      "type": "vless",
      "tag": "vless-in",
      "listen": "::",
      "listen_port": 443,
      "network": "tcp",
      "users": [
        {
          "name": "user1",
          "uuid": "b831381d-6324-4d53-ad4f-8cda48b30811",
          "flow": "xtls-rprx-vision"
        },
        {
          "name": "user2",
          "uuid": "c942492e-7435-5e64-be5f-9deb59c41922",
          "flow": "xtls-rprx-vision"
        }
      ],
      "tls": {
        "enabled": true,
        "certificate_path": "/path/to/cert.pem",
        "key_path": "/path/to/key.pem",
        "min_version": "1.2",
        "max_version": "1.3"
      },
      "tcp_keep_alive": {
        "enabled": true
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
    "final": "direct",
    "auto_detect_interface": true
  }
}
```

### 证书配置

```bash
# 生成自签名证书（测试用）
openssl req -x509 -newkey rsa:2048 -keyout key.pem -out cert.pem -days 365 -nodes

# 使用 Let's Encrypt 证书（生产环）
# 1. 安装 certbot
sudo apt install certbot

# 2. 申请证书
sudo certbot certonly --standalone -d example.com

# 3. 复制到 sing-box 目录
sudo cp /etc/letsencrypt/live/example.com/fullchain.pem /etc/sing-box/cert.pem
sudo cp /etc/letsencrypt/live/example.com/privkey.pem /etc/sing-box/key.pem

# 4. 设置文件权限
sudo chmod 644 /etc/sing-box/cert.pem
sudo chmod 600 /etc/sing-box/key.pem
sudo chown sing-box:sing-box /etc/sing-box/*.pem
```

### 系统服务配置

**创建 systemd 服务** (`/etc/systemd/system/sing-box.service`):

```ini
[Unit]
Description=sing-box service
Documentation=https://sing-box.sagernet.org/
After=system-online.target nss-lookup.target

[Service]
CapabilityBoundingSet=CAP_NET_ADMIN CAP_NET_RAW CAP_SYS_RESOURCE
AmbientCapabilities=CAP_NET_ADMIN CAP_NET_RAW CAP_SYS_RESOURCE
ExecStart=/usr/local/bin/sing-box run -c /etc/sing-box/config.json
ExecReload=/bin/kill -HUP $MAINPID
Restart=on-failure
RestartSec=10s
LimitNOFILE=infinity

[Install]
WantedBy=multi-user.target
```

```bash
# 启动服务
sudo systemctl start sing-box

# 设为开机启动
sudo systemctl enable sing-box

# 查看日志
sudo journalctl -u sing-box -f

# 重启服务
sudo systemctl restart sing-box
```

---

## 单文件配置对照表

### 配置对应关系速查

```
┌────────────────────────────────┐
│   sing-box config.json         │
├────────────────────────────────┤
│ log:        日志配置            │
│ dns:        DNS 服务            │
│ inbounds:   入站（监听）        │
│   ├─ type: 协议类型            │
│   ├─ listen: 监听地址          │
│   ├─ listen_port: 监听端口     │
│   └─ users: 用户认证           │
│ outbounds:  出站（连接）        │
│   ├─ type: 协议类型            │
│   ├─ server: 服务器地址        │
│   ├─ server_port: 服务器端口   │
│   └─ tls: TLS 配置             │
│ route:      流量路由            │
│   ├─ rules: 路由规则           │
│   └─ final: 默认出站           │
└────────────────────────────────┘
```

### 常用参数一览

**Inbound 参数**：

```json
{
  "type": "mixed",              // mixed | http | socks | vless | trojan
  "tag": "local-in",            // 唯一标识
  "listen": "127.0.0.1",        // 监听地址
  "listen_port": 1080,          // 监听端口
  "network": "tcp",             // tcp | udp | (留空=两者)
  "users": []                   // 用户认证（仅限部分协议）
}
```

**Outbound 参数**：

```json
{
  "type": "vless",              // vless|trojan|direct|block
  "tag": "vpn-out",             // 唯一标识
  "server": "vpn.example.com",  // 远程服务器地址
  "server_port": 443,           // 远程服务器端口
  "uuid": "...",                // 用户 ID（VLESS/Trojan）
  "tls": {
    "enabled": true,
    "server_name": "vpn.example.com"
  }
}
```

**Route 参数**：

```json
{
  "rules": [
    {
      "domain": ["google.com"],           // 完全匹配域名
      "geosite": "cn",                    // 地理位置库
      "geoip": "cn",                      // IP 地理位置
      "port": 53,                         // 端口
      "process_name": ["chrome"],         // 进程名
      "action": "route",                  // 路由动作
      "outbound": "vpn-out"               // 目标出站
    }
  ],
  "final": "direct-out"                   // 默认出站
}
```

---

## 常用命令速查

### 基础命令

```bash
# 检查配置文件语法
sing-box check -c config.json -C config_dir/

# 格式化配置文件
sing-box format -w -c config.json

# 合并多个配置文件
sing-box merge output.json -c config.json -C config_dir/

# 生成工具
sing-box generate uuid               # 生成 UUID
sing-box generate rand --base64 32   # 生成密钥

# 运行 sing-box
sing-box run -c config.json

# 以守护进程运行
nohup sing-box run -c config.json > sing-box.log 2>&1 &

# 关闭 sing-box
pkill -f "sing-box run"
```

### 调试命令

```bash
# 启用 debug 日志
sing-box -D config_dir/ run -c config.json 2>&1 | tee debug.log

# 只显示错误
sing-box run -c config.json 2>&1 | grep -E "error|ERROR|FATAL"

# 监测连接
ss -tnap | grep sing-box

# 测试本地代理
curl -x http://127.0.0.1:1080 https://google.com

# 测试 SOCKS5
curl -x socks5h://127.0.0.1:1080 https://google.com

# 查看 DNS 请求
tcpdump -i any -n port 53
```

### 性能测试

```bash
# 测试网络延迟
ping -c 4 YOUR_SERVER_ADDRESS
mtr YOUR_SERVER_ADDRESS

# 测试代理速度
curl -x socks5h://127.0.0.1:1080 -o /dev/null -s -w "%{time_total}\n" https://www.google.com

# 测试 DNS 解析速度
time dig @127.0.0.1 -p 53 google.com
```

---

## 文件授权与运行

```bash
# 解压下载的文件
tar -xzf sing-box-*-linux-*.tar.gz

# 赋予执行权限
chmod +x sing-box

# 移到系统路径（可选）
sudo mv sing-box /usr/local/bin/

# 创建配置目录
sudo mkdir -p /etc/sing-box

# 创建日志目录
sudo mkdir -p /var/log/sing-box
sudo chown -R $USER:$USER /var/log/sing-box
```

---

## 故障自助排查

### 步骤 1：验证配置

```bash
# 检查配置是否有语法错误
./sing-box check -c config.json

# 应该输出：[info] Configuration check successful
```

### 步骤 2：启用 debug 日志

修改 config.json 中的日志级别：

```json
"log": {
  "level": "debug"  // 改为 debug
}
```

然后运行并观察日志：

```bash
./sing-box run -c config.json 2>&1 | tee debug.log
```

### 步骤 3：测试代理

```bash
# 在另一个终端测试
curl -x http://127.0.0.1:1080 https://www.google.com -v

# 或用 socks5
curl -x socks5h://127.0.0.1:1080 https://www.google.com -v
```

### 步骤 4：检查日志中的错误

```bash
# 查找错误信息
grep -i error debug.log

# 查找连接问题
grep -i "connection\|dial" debug.log

# 查找 DNS 问题
grep -i "dns" debug.log
```

---

## 一行启动命令

```bash
# 最小化模式（测试）
./sing-box run -c config.json

# 带日志输出（调试）
./sing-box run -c config.json 2>&1 | tee app.log

# 后台运行（生产）
nohup ./sing-box run -c config.json > /var/log/sing-box.log 2>&1 &

# 指定配置目录
./sing-box run -D /etc/sing-box -c config.json
```

---

## ⚠️ 常见错误速查表

| 错误信息 | 原因 | 解决方案 |
|----------|------|---------|
| `:address: connection refused` | 服务器未启动或地址错误 | 检查 server 和 server_port |
| `certificate verify failed` | TLS 证书问题 | 设置 `"insecure": true` 或更新证书 |
| `port already in use` | 端口被占用 | 改用其他端口或 `lsof -i:1080` 查找占用进程 |
| `dial: connection timeout` | 连接超时 | 增加 `"connect_timeout": "10s"` |
| `dns: i/o timeout` | DNS 查询超时 | 检查 DNS 服务器是否可访问 |

---

**特别提示**：
- 👉 第一次使用？推荐用**客户端：混合入站 + 分流出站**的配置
- 👉 需要自建服务？参考**服务器：VLESS 完整服务**部分
- 👉 遇到问题？查看**常见错误速查表**或**故障自助排查**

---

**最后更新**：2024 年 3 月
