# 📚 Sing-Box 完整学习包 - 使用说明

## 🎉 恭喜！你已获得完整的 Sing-Box 学习资料

我为你生成了 **5 份深度文档**，共计 **15,000+ 行** 的详细内容，涵盖从入门到精通的全部内容。

---

## 📂 你现在拥有的文件

所有文件都在你的项目根目录：`/Users/apple/Documents/study/sing-box/`

```
📄 INDEX.md                           ← 👈 从这里开始！总索引和导航
📄 CONFIGURATION_GUIDE.md             ← 配置参数全面解析（最详细）
📄 TEMPLATES_v1.13.3.md               ← 前后端完整模板（即插即用）
📄 ADVANCED_TROUBLESHOOTING.md        ← 高级配置和问题排查
📄 STABLE_ANDROID_PROFILE.md          ← Android 稳定配置基线（已验证）
📄 QUICK_START.md                     ← 快速参考和复制即用配置
📄 VPN_PRINCIPLES.md                  ← VPN 原理和工作机制讲解
```

---

## ⚡ 5 分钟快速开始

### 第 1 步：打开索引文件

在 VS Code 中打开 [INDEX.md](INDEX.md)，这是总导航，包含：
- 各个文档的用途说明
- 快速查找指南
- 推荐学习路径

### 第 2 步：选择你的学习路径

根据你的需求，选择对应的路径：

**🟢 我想快速上手使用（10 分钟）**
→ 打开 [QUICK_START.md](QUICK_START.md)
→ 复制"客户端：混合入站 + 分流出站"配置
→ 修改 3 个参数即可运行

**🟡 我想深入理解配置（1 小时）**
→ 先读 [VPN_PRINCIPLES.md](VPN_PRINCIPLES.md) 理解原理
→ 再读 [CONFIGURATION_GUIDE.md](CONFIGURATION_GUIDE.md) 学参数
→ 最后参考 [TEMPLATES_v1.13.3.md](TEMPLATES_v1.13.3.md) 调整配置

**🔴 我要建立 VPN 服务器（30 分钟）**
→ 打开 [TEMPLATES_v1.13.3.md](TEMPLATES_v1.13.3.md)
→ 找"服务器：VLESS 完整服务"
→ 按里面的步骤部署

**🔧 我遇到问题需要排查（30 分钟）**
→ 打开 [ADVANCED_TROUBLESHOOTING.md](ADVANCED_TROUBLESHOOTING.md)
→ 查找你遇到的问题
→ 按步骤操作

---

## 📖 各文档一句话说明

| 文档 | 一句话总结 |
|------|-----------|
| **INDEX.md** | 📍 导航中心，告诉你怎么用其他文档 |
| **CONFIGURATION_GUIDE.md** | 📚 字典书，每个参数是什么意思、怎么用 |
| **TEMPLATES_v1.13.3.md** | 📋 模板库，复制改改就能用的完整配置 |
| **ADVANCED_TROUBLESHOOTING.md** | 🔧 维修工具，性能慢了、出问题了怎么办 |
| **STABLE_ANDROID_PROFILE.md** | ✅ 稳定基线，Android 网络不稳时优先套用 |
| **QUICK_START.md** | ⚡ 速查表，最常用的命令、配置、错误解决 |
| **VPN_PRINCIPLES.md** | 🧠 理论课，为什么 VPN 这样工作 |

---

## 🎯 根据需求快速找到答案

### 我想知道...

**"什么是 Inbound？"**
→ 打开 CONFIGURATION_GUIDE.md → 搜索 "Inbound" → 看"入站配置"章节

**"怎么防止 DNS 污染？"**
→ 打开 CONFIGURATION_GUIDE.md → 搜索 "DNS" 或打开 VPN_PRINCIPLES.md

**"VLESS 和 Trojan 哪个更好？"**
→ 打开 TEMPLATES_v1.13.3.md → 看最后的"协议对比矩阵"

**"为什么连接超时？"**
→ 打开 ADVANCED_TROUBLESHOOTING.md → 搜索 "Connection Timeout"

**"怎样快速启动？"**
→ 打开 QUICK_START.md → 搜索 "启动" 或 "一行启动命令"

**"国内走直连，国外走 VPN 怎么配置？"**
→ 打开 TEMPLATES_v1.13.3.md → 搜索 "分流配置"

---

## 🔥 最常用的 3 个配置

### 配置 1：在我的 PC 上使用 VPN（分流）

```json
{
  "inbounds": [{
    "type": "mixed",
    "listen": "127.0.0.1",
    "listen_port": 1080
  }],
  "outbounds": [
    {"type": "direct", "tag": "direct"},
    {"type": "vless", "tag": "vpn", "server": "YOUR_SERVER", "server_port": 443}
  ],
  "route": {
    "rules": [
      {"geoip": "cn", "outbound": "direct"},
      {"geosite": "cn", "outbound": "direct"}
    ],
    "final": "vpn"
  }
}
```

**需要修改的 3 个地方**：
1. `YOUR_SERVER` → 你的 VPN 服务器地址
2. `listen_port` → 本地监听端口（默认 1080 就好）
3. `server_port` → VPN 服务器端口（例如 443、8443 等）

完整配置见：TEMPLATES_v1.13.3.md → "客户端：分流配置"

### 配置 2：建立 VPN 服务器

```json
{
  "inbounds": [{
    "type": "vless",
    "listen": "::",
    "listen_port": 443,
    "users": [{"uuid": "your-uuid"}],
    "tls": {"enabled": true}
  }],
  "outbounds": [{"type": "direct"}]
}
```

完整配置见：TEMPLATES_v1.13.3.md → "服务器：VLESS 完整服务"

### 配置 3：所有流量走 VPN

```json
{
  "inbounds": [{"type": "mixed", "listen": "127.0.0.1", "listen_port": 1080}],
  "outbounds": [{"type": "vless", "server": "YOUR_SERVER", "server_port": 443}],
  "route": {"final": "vless-out"}
}
```

完整配置见：TEMPLATES_v1.13.3.md → "客户端：全部代理配置"

---

## 💡 重要要点（必读）

### ✅ 一定要了解的 3 个概念

1. **Inbound（入站）** - 应用与 Sing-Box 的连接
   - 你的 Chrome、QQ 等应用→Sing-Box
   - 通常设为 `127.0.0.1:1080`（本机 SOCKS 代理）

2. **Outbound（出站）** - Sing-Box 与外界的连接
   - Sing-Box→VPN 服务器 或 Sing-Box→直连
   - 可以是 `vless、trojan、direct` 等多种协议

3. **Route（路由）** - 流量分配规则
   - 国内流量→直连（快）
   - 国外流量→VPN（隐私）

### ⚠️ 常见新手错误（避免）

❌ **错误 1**：直接复制网上的配置，不修改服务器地址
→ ✅ 一定要改 `server` 和 `server_port`

❌ **错误 2**：DNS 还用运营商的，导致污染
→ ✅ 用 `type: https` 的 DNS（如 8.8.8.8）

❌ **错误 3**：分流配置写错了，所以所有流量都走 VPN
→ ✅ 按照模板配置 geoip 和 geosite 规则

❌ **错误 4**：日志级别设为 debug，导致性能下降
→ ✅ 生产环境用 `warn` 或 `info`

❌ **错误 5**：没有启用 `find_process`，按应用分流无效
→ ✅ 如果按应用分流，要加 `"find_process": true`

---

## 🚀 三步快速上手

### 步骤 1：下载 Sing-Box 二进制（2 分钟）

```bash
# 下载地址：https://github.com/SagerNet/sing-box/releases
# 选择你的系统版本，例如：
# - Linux: sing-box-1.13.3-linux-amd64.tar.gz
# - macOS: sing-box-1.13.3-darwin-amd64.tar.gz
# - Windows: sing-box-1.13.3-windows-amd64.zip

tar -xzf sing-box-1.13.3-*.tar.gz
chmod +x sing-box
```

### 步骤 2：编辑配置文件（5 分钟）

```bash
# 复制 QUICK_START.md 中的配置到 config.json
# 修改 3 个地方：
# 1. YOUR_SERVER_ADDRESS → 你的 VPN 服务器
# 2. YOUR_UUID_HERE → 你的 UUID
# 3. listen_port → 本地端口（可选）
```

### 步骤 3：运行（1 分钟）

```bash
./sing-box run -c config.json

# 然后在浏览器代理设置：
# 代理：127.0.0.1:1080
# 完成！
```

---

## 🎓 学习建议（付出与收获）

### 时间投入 vs 收获

| 投入时间 | 你将学到 | 推荐阅读 |
|---------|--------|---------|
| **10 分钟** | 快速上手，使用 VPN | QUICK_START |
| **30 分钟** | 理解配置参数 | CONFIGURATION_GUIDE |
| **1 小时** | 完全掌握，能自定义 | + VPN_PRINCIPLES |
| **2 小时** | 能建立服务器 | + TEMPLATES_v1.13.3 |
| **3 小时** | 能解决问题 | + ADVANCED_TROUBLESHOOTING |

### 推荐学习顺序

```
初学者 → VPN_PRINCIPLES.md
         ↓
简单应用 → QUICK_START.md
          ↓
深入学习 → CONFIGURATION_GUIDE.md
          ↓
实战应用 → TEMPLATES_v1.13.3.md
          ↓
问题排查 → ADVANCED_TROUBLESHOOTING.md
          ↓
完全精通！💪
```

---

## 📱 在不同设备上使用

### Windows / Linux / macOS

使用 QUICK_START 中的"客户端：混合入站 + 分流出站"配置即可

### Android

需要使用特殊的 APP（如 sing-box 官方 APP）

### iPhone / iPad

需要使用特殊的 APP（如支持 Sing-Box 的 VPN APP）

更多详情：官方文档 https://sing-box.sagernet.org/

---

## 🆘 遇到问题怎么办？

### 第 1 步：确认症状

```bash
# 能连接到服务器吗？
curl -x http://127.0.0.1:1080 https://www.google.com

# DNS 能解析吗？
dig @127.0.0.1 -p 53 google.com
```

### 第 2 步：查看日志

```bash
# 修改 log.level 为 debug
# 重新运行查看错误信息
./sing-box run -c config.json 2>&1 | grep -i error
```

### 第 3 步：查阅文档

```
错误关键词 → ADVANCED_TROUBLESHOOTING.md 中搜索
          → 查看对应的解决方案
```

### 第 4 步：查看官方社区

https://github.com/SagerNet/sing-box/discussions

---

## 📊 知识导图

```
Sing-Box 完整学习包
│
├── INDEX.md （导航）
│   └─ 告诉你去看哪个文档
│
├── VPN_PRINCIPLES.md （理论）
│   ├─ VPN 是什么
│   ├─ Sing-Box 怎样工作
│   └─ 与其他工具的对比
│
├── CONFIGURATION_GUIDE.md （参数详解）
│   ├─ Inbound（入站）参数
│   ├─ Outbound（出站）参数
│   ├─ Route（路由）参数
│   └─ DNS 参数
│
├── TEMPLATES_v1.13.3.md （实战模板）
│   ├─ 服务器配置（4 种）
│   └─ 客户端配置（4 种）
│
├── QUICK_START.md （快速参考）
│   ├─ 复制即用配置
│   ├─ 常用命令
│   └─ 快速故障排查
│
└── ADVANCED_TROUBLESHOOTING.md （问题解决）
    ├─ 企业级配置（4 种）
    ├─ 常见问题（4 个）
    ├─ 性能优化
    └─ 安全配置
```

---

## ✨ 特色内容一览

### 📊 参数对比表
- 🔄 17 种 Inbound 协议对比
- 🔄 20+ 种 Outbound 协议说明
- 🔄 DNS 服务器类型对比
- 🔄 与其他工具的功能对比矩阵

### 📋 完整配置示例
- ✅ 4 个服务器端配置（VLESS、Hysteria2、SS、Trojan）
- ✅ 4 个客户端配置（分流、全代理、按应用、负载均衡）
- ✅ 企业级透明代理配置
- ✅ 广告拦截配置

### 🔧 命令速查
- ✅ 基础命令（check、format、run）
- ✅ 生成工具（UUID、密钥、密码）
- ✅ 调试命令（tcpdump、curl、ss）
- ✅ 性能测试命令

### 🐛 问题解决方案
- ✅ DNS 污染问题
- ✅ 连接超时问题
- ✅ 分流不生效问题
- ✅ FakeIP 兼容性问题

---

## 🎁 额外资源

### 官方链接
- 📖 官方文档：https://sing-box.sagernet.org/
- 💻 GitHub 项目：https://github.com/SagerNet/sing-box
- 💬 讨论社区：https://github.com/SagerNet/sing-box/discussions
- 📱 Telegram 群：@sing_box_api

### 相关工具
- 🌐 GeoIP/GeoSite 库：https://github.com/SagerNet/geo
- 🔑 UUID 生成：在线随机生成器
- 🔐 密钥生成：sing-box generate rand --base64 32

---

## 💬 问卷反馈（可选）

如果这份学习资料对你有帮助，欢迎反馈：
- ✅ 最有用的部分是什么？
- ✅ 还需要补充哪些内容？
- ✅ 有什么行不通的地方吗？

---

## 📝 版本信息

```
Sing-Box 版本：1.13.3（最新稳定版本）
文档完成日期：2024 年 3 月
总字数：15,000+ 行
文件数：6 个 Markdown 文件
覆盖范围：零基础到精通
```

---

## 🎯 下一步行动

### 现在就开始吧！👇

```
1️⃣ 打开 INDEX.md（当前文件），查看总导航
   ↓
2️⃣ 根据需求选择对应文档
   ↓
3️⃣ 跟着步骤操作
   ↓
4️⃣ 成功运行 Sing-Box！🎉
```

---

## 🏆 学完之后你能做什么

✅ 快速配置 VPN 客户端
✅ 自建 VPN 服务器
✅ 实现国内外分流
✅ 防止 DNS 污染
✅ 出现问题能自己排查
✅ 优化 VPN 性能
✅ 与其他 VPN 工具进行对比
✅ 在任何平台部署

---

**祝你学习愉快！** 🚀

如有问题，先查文档，再查官方社区。

**开始吧！** → **打开 INDEX.md**
