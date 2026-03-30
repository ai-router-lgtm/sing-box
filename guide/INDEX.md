# Sing-Box 配置文档总索引

## 📚 文档总览

我为你生成了 **5 份完整文档**，共计 **15,000+ 行**的详细说明和配置模板。

---

## 📖 文档清单与用途

### 1️⃣ [CONFIGURATION_GUIDE.md](CONFIGURATION_GUIDE.md) - 配置完全指南
**内容**：核心配置参数详解、VPN 应用场景

| 章节 | 内容 | 适合人群 |
|------|------|---------|
| 概述 | Sing-Box 是什么、主要功能 | 初学者 |
| 配置结构 | JSON 根结构、模块划分 | 初学者 |
| **日志配置** | log 参数、输出控制 | 运维 |
| **入站配置** | Inbound 参数详解、17 种类型对比 | 开发 |
| **出站配置** | Outbound 参数详解、出站选择器 | 开发 |
| **路由配置** | Route 匹配规则、分流策略 | 进阶 |
| **DNS 配置** | DNS 服务器、防污染、FakeIP | 进阶 |
| VPN 应用 | 3 个完整配置示例 | 初学者 |
| 最佳实践 | 性能优化、常见错误 | 进阶 |

**关键内容**：
- 入站类型对比表（MIXED vs SOCKS vs VLESS vs TROJAN）
- 出站协议选择指南
- 路由规则匹配条件详解
- DNS 防污染完整方案

---

### 2️⃣ [TEMPLATES_v1.13.3.md](TEMPLATES_v1.13.3.md) - v1.13.3 前后端模板
**内容**：即插即用的完整配置模板

| 配置类型 | 用途 | 复杂度 |
|---------|------|--------|
| **服务器端** | | |
| VLESS 服务器 | 轻量级 VPN 服务，支持 XTLS 流控 | 中等 |
| Hysteria2 服务器 | 高速 QUIC VPN，带宽利用率最高 | 中等 |
| Shadowsocks 服务器 | 轻量加密代理，多用户 | 简单 |
| Trojan 服务器 | 伪装 HTTPS，隐蔽性最强 | 简单 |
| **客户端** | | |
| 分流配置 | 国内直连，国外走 VPN（**推荐**） | 简单 |
| 全部代理 | 所有流量走 VPN，隐私最强 | 简单 |
| 按应用分流 | 不同应用走不同路线 | 中等 |
| 负载均衡 | 多个 VPN 服务器自动测速选择 | 中等 |
| **对比表** | 协议选择建议、性能矩阵 | 简单 |

**特点**：
- 每个模板都是完整的 JSON，可直接修改后使用
- 包含详细的参数注释
- 提供参数修改指南
- 包括证书生成方法

---

### 3️⃣ [ADVANCED_TROUBLESHOOTING.md](ADVANCED_TROUBLESHOOTING.md) - 高级配置与故障排查
**内容**：企业级配置、常见问题解决方案

| 高级场景 | 说明 | 应用 |
|---------|------|------|
| 企业透明代理 | Linux 全系统代理，无需客户端配置 | 公司网络 |
| 浮动 IP 自动切换 | DynDNS 场景的自动更新 | 服务器迁移 |
| 多协议负载均衡 | 多个协议同时运行 | 高可用 |
| 广告拦截与恶意网站防护 | 在代理层面过滤 | 家庭网络 |

**常见问题**：
- ❓ DNS 污染问题及解决
- ❓ 连接超时问题及解决
- ❓ 分流不生效问题及解决
- ❓ FakeIP 导致应用无法工作

**性能优化**：
- DNS 缓存优化
- 连接优化（TCP 快速打开、Keep-Alive）
- 规则优化（批量规则 vs 单条规则）
- 日志级别选择

**安全实践**：
- TLS 证书安全配置
- 用户认证安全
- 防火墙配置
- 信息泄露防护

---

### 4️⃣ [QUICK_START.md](QUICK_START.md) - 快速参考与复制即用
**内容**：最常用的配置、命令速查、快速启动

| 部分 | 内容 |
|------|------|
| 客户端：混合入站 + 分流 | 完整 JSON + 快速配置说明 |
| 客户端：纯代理模式 | 精简版，所有流量走 VPN |
| 服务器：VLESS 完整服务 | 多用户 + TLS + systemd |
| 系统服务配置 | systemd 实现开机自启 |
| 常用命令 | check、format、merge、run 等 |
| 调试命令 | tcpdump、ss、curl 等 |
| 文件授权 | 解压、部署、运行步骤 |
| 故障自助排查 | 4 步快速定位问题 |
| 常见错误速查表 | 错误信息对应解决方案 |

**特点**：
- 复制即用的完整配置
- 一行启动命令
- 快速排查流程

---

### 5️⃣ [VPN_PRINCIPLES.md](VPN_PRINCIPLES.md) - VPN 原理与 Sing-Box 角色
**内容**：VPN 理论基础、Sing-Box 的位置

| 主题 | 内容 |
|------|------|
| 网络拓扑 | 普通访问 vs 代理访问 vs 分流访问的流量图解 |
| Sing-Box 在 VPN 中的角色 | 功能模块、数据流分析 |
| 流量分析 | 访问 Google（国外）和 Bilibili（国内）的具体流程 |
| 对比表 | Sing-Box vs V2Ray vs Clash vs SS vs WireGuard |
| 关键概念 | 代理、加密、分流、FakeIP 等通俗解释 |
| 安全分析 | Sing-Box 的安全保障、常见风险与对策 |
| 性能分析 | 为什么 Sing-Box 性能好、性能数据参考 |
| 常见误区 | VPN 网速、匿名性、加密等常见误区纠正 |

**特点**：
- 完整的网络拓扑图
- 流量分析时间线
- 与其他工具的客观对比
- 安全性和性能分析

---

## 🎯 快速查找指南

### 按场景查找

| 场景 | 查看文档 | 章节 |
|------|---------|------|
| 🆕 **初次使用** | QUICK_START | 客户端配置章节 |
| 🔧 **配置 VPN 客户端** | CONFIGURATION_GUIDE | VPN 应用实例 |
| 🖥️ **建立 VPN 服务器** | TEMPLATES_v1.13.3 | 服务器端配置 |
| 🐛 **遇到连接问题** | ADVANCED_TROUBLESHOOTING | 常见问题章节 |
| 📊 **性能太慢** | ADVANCED_TROUBLESHOOTING | 性能优化建议 |
| 🔒 **避免 DNS 污染** | CONFIGURATION_GUIDE | DNS 配置章节 |
| 🌍 **想要分流国内外** | TEMPLATES_v1.13.3 | 分流配置 |
| 💡 **理解 VPN 原理** | VPN_PRINCIPLES | 全部章节 |

### 按问题查找

| 问题 | 查看文档 |
|------|---------|
| 什么是 Inbound/Outbound? | VPN_PRINCIPLES + CONFIGURATION_GUIDE |
| 如何防止 DNS 污染? | CONFIGURATION_GUIDE + QUICK_START |
| 分流规则怎么写? | CONFIGURATION_GUIDE（Route 章节）|
| DNS 查询超时? | ADVANCED_TROUBLESHOOTING（故障排查）|
| TLS 证书错误? | QUICK_START（常见错误表）|
| 性能优化方法? | ADVANCED_TROUBLESHOOTING（性能优化）|
| 各协议怎么选? | TEMPLATES_v1.13.3（协议对比）|
| 需要即插即用配置? | QUICK_START |

### 按难度查找

#### 🟢 初级（刚开始使用）
1. 阅读 [VPN_PRINCIPLES.md](VPN_PRINCIPLES.md) - 理解原理
2. 使用 [QUICK_START.md](QUICK_START.md) - 复制配置
3. 参考 [CONFIGURATION_GUIDE.md](CONFIGURATION_GUIDE.md) - 理解参数

#### 🟡 中级（需要自定义配置）
1. [CONFIGURATION_GUIDE.md](CONFIGURATION_GUIDE.md) - 全面学习
2. [TEMPLATES_v1.13.3.md](TEMPLATES_v1.13.3.md) - 参考模板
3. [QUICK_START.md](QUICK_START.md) - 快速参考

#### 🔴 高级（性能优化、企业部署）
1. [ADVANCED_TROUBLESHOOTING.md](ADVANCED_TROUBLESHOOTING.md) - 全部
2. [VPN_PRINCIPLES.md](VPN_PRINCIPLES.md) - 深入理解
3. 官方文档 - https://sing-box.sagernet.org/

---

## 📝 配置参数速查

### 最常用的 10 个参数

| 参数 | 在哪个文档 | 在哪个配置段 |
|------|-----------|------------|
| `listen_port` | QUICK_START | inbound |
| `server_port` | QUICK_START | outbound |
| `geosite: cn` | CONFIGURATION_GUIDE | route.rules |
| `geoip: cn` | CONFIGURATION_GUIDE | route.rules |
| `action: route` | CONFIGURATION_GUIDE | route.rules |
| `type: https` | CONFIGURATION_GUIDE | dns.servers |
| `tls: enabled` | TEMPLATES_v1.13.3 | inbound/outbound |
| `cache_capacity` | ADVANCED_TROUBLESHOOTING | dns |
| `find_process` | CONFIGURATION_GUIDE | route |
| `auto_detect_interface` | CONFIGURATION_GUIDE | route |

---

## 🚀 推荐学习路径

### 路径 1：我想快速上手，使用 VPN（预计 10 分钟）

```
1. 阅读 QUICK_START - "客户端：混合入站 + 分流出站"
   ↓
2. 修改配置中的 YOUR_SERVER_ADDRESS 和 YOUR_UUID
   ↓
3. 运行 sing-box run -c config.json
   ↓
4. 配置浏览器代理：127.0.0.1:1080
   ✅ 完成！
```

### 路径 2：我想深入学习配置（预计 1 小时）

```
1. 阅读 VPN_PRINCIPLES.md
   └─ 理解 VPN 工作原理
   
2. 阅读 CONFIGURATION_GUIDE.md
   ├─ 理解 Inbound/Outbound/Route/DNS
   └─ 学习参数含义
   
3. 参考 TEMPLATES_v1.13.3.md
   └─ 对比不同场景的配置
   
4. 实践修改配置
   └─ 根据需求调整参数
```

### 路径 3：我要建立 VPN 服务器（预计 30 分钟）

```
1. 阅读 TEMPLATES_v1.13.3.md - "服务器：VLESS 完整服务"
   
2. 生成证书（见 QUICK_START）
   
3. 修改配置：UUID、证书路径
   
4. 部署 systemd 服务
   
5. 启动：sudo systemctl start sing-box
```

### 路径 4：我遇到问题需要排查（预计 30 分钟）

```
1. 确认症状（常见问题列表）
   └─ 查看 ADVANCED_TROUBLESHOOTING
   
2. 启用 debug 日志
   └─ 修改 log.level: debug
   
3. 运行并收集日志
   └─ sing-box run -c config.json > debug.log 2>&1
   
4. 查看问题对应的解决方案
   └─ 按步骤操作
```

---

## 💾 文档导出与离线使用

### Markdown 文件列表

```
/CONFIGURATION_GUIDE.md          - 4000+ 行
/TEMPLATES_v1.13.3.md            - 3500+ 行
/ADVANCED_TROUBLESHOOTING.md     - 3000+ 行
/QUICK_START.md                  - 2500+ 行
/VPN_PRINCIPLES.md               - 2000+ 行
/INDEX.md                        - 本文件
```

### 生成 PDF（可选）

```bash
# 使用 pandoc 转换为 PDF
pandoc CONFIGURATION_GUIDE.md -o CONFIGURATION_GUIDE.pdf

# 或合并为单个 PDF
pandoc *.md -o sing-box-complete-guide.pdf
```

---

## ⚡ 一页纸速记

### 三行配置要点

```json
{
  "inbounds": [{}],        // 监听客户端请求
  "outbounds": [{}],       // 连接远程服务器
  "route": { "rules": [] } // 判断谁走VPN谁直连
}
```

### 三个必知参数

| 参数 | 用途 | 示例 |
|------|------|------|
| `geoip: cn` | 按 IP 地址分流 | 国内 IP 直连 |
| `geosite: cn` | 按域名分流 | .cn 域名直连 |
| `port: 53` | 按端口分流 | DNS 流量劫持 |

### 三种需求对应配置

```
需求：国内直连，国外走 VPN
→ 使用 TEMPLATES_v1.13.3.md 中的"分流配置"

需求：所有流量走 VPN
→ 使用 TEMPLATES_v1.13.3.md 中的"全部代理配置"

需求：按应用分流
→ 使用 TEMPLATES_v1.13.3.md 中的"按应用分流"
```

---

## 📞 获取帮助

### 如果你...

| 情况 | 建议 |
|------|------|
| 不知道配置从何开始 | → 看 QUICK_START |
| 不懂某个参数的含义 | → 看 CONFIGURATION_GUIDE |
| 复制配置无法运行 | → 看 ADVANCED_TROUBLESHOOTING |
| 想理解工作原理 | → 看 VPN_PRINCIPLES |
| 需要企业级部署 | → 看 ADVANCED_TROUBLESHOOTING |
| 想对比协议选择 | → 看 TEMPLATES_v1.13.3 |

---

## 📞 官方资源

| 资源 | 链接 |
|------|------|
| 官方文档 | https://sing-box.sagernet.org/ |
| GitHub | https://github.com/SagerNet/sing-box |
| 讨论社区 | https://github.com/SagerNet/sing-box/discussions |
| Telegram 讨论组 | @sing_box_api |

---

## ✅ 文档完整性检查清单

- ✅ 配置参数完全解析（log、dns、inbound、outbound、route）
- ✅ 17 种 Inbound 协议对比
- ✅ 20+ 种 Outbound 协议说明
- ✅ 路由规则匹配条件详解
- ✅ 4 个服务器端完整配置
- ✅ 4 个客户端完整配置
- ✅ 4 个高级应用场景
- ✅ 10+ 个常见问题解决方案
- ✅ 性能优化建议
- ✅ 安全配置最佳实践
- ✅ VPN 原理和流量分析
- ✅ 快速参考和命令速查
- ✅ 故障排查流程

---

**版本信息**：Sing-Box v1.13.3（最新稳定版）
**最后更新**：2024 年 3 月
**总字数**：约 15,000+ 行

---

## 🎓 学习成果

完成上述文档学习后，你将能够：

✅ 理解 VPN 和代理的工作原理
✅ 配置 Sing-Box 作为客户端或服务器
✅ 实现国内直连、国外代理的分流
✅ 处理常见的 VPN 问题
✅ 优化 VPN 性能
✅ 确保配置的安全性
✅ 选择适合的协议和配置方案
✅ 与其他 VPN 方案进行对比评估

**开始学习吧！** 🚀
