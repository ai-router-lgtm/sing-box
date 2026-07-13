# Runtime 改造当前状态存档

更新时间：2026-07-11 CST
仓库：`/Users/apple/Documents/GitHub/velo/sing-box`
分支：`feat/runtime-control-v2`

## 1) 已完成内容

- 已落地 runtime 控制接口：
  - `PUT /runtime/policy`
  - `PUT /runtime/users`
  - `DELETE /runtime/users/{principal}?inbound={tag}`
  - `POST /runtime/disconnect`
  - `GET /runtime/stats/snapshot`
  - `GET /runtime/status`
  - `GET /runtime/policy/snapshot`
- `POST /runtime/disconnect` 已支持最多 1000 个 selector 的批量请求、单次连接扫描和 request ID 幂等。
- `PUT /runtime/users` 已支持 `replace_managed=true`，权威替换 Runtime 受管用户且保留静态配置用户。
- Runtime status 已提供进程实例 ID、能力列表及用户/策略 count/digest，不返回认证凭据明文。
- 已支持 runtime users 兼容映射：`upsert.name -> principal`
- 已支持 policy 通配优先级：`u:d` > `u:*` > 无策略
- 已补齐测试：
  - runtime 接口行为（revision/idempotent/disconnect scope）
  - wildcard policy 匹配与快照展示
- 已补齐文档与联调模板：
  - `guide/runtime/RUNTIME_BASELINE_RELEASE_NOTES.md`
  - `guide/runtime/verify-runtime-user-control-template.sh`

## 2) 测试结果（本次确认）

已通过：

- `go test ./experimental/clashapi/...`
- `go test ./...`
- `go test ./protocol/vless ./protocol/vmess ./protocol/trojan ./protocol/tuic ./protocol/hysteria2 ./protocol/hysteria ./protocol/shadowsocks`

## 3) Baseline 提交与标签

最近三次核心提交：

1. `d8303967` feat(runtime): add clash runtime control endpoints and runtime user hot updates
2. `b2258b98` test(runtime): cover revision, idempotency, disconnect scope and wildcard policy
3. `a41fdf51` docs(runtime): document revision/request_id semantics and e2e verification template

已创建标签：

- `runtime-baseline`

## 4) 仍待完成（联调环境相关）

- ai-vpn 端到端验收脚本通过记录：
  - `ai-vpn/scripts/verify-runtime-user-control.sh`
- wildcard 联调用例的实机输出留档：
  - `guide/runtime/verify-runtime-user-control-template.sh`

说明：Runtime v2 代码与全仓单元测试已完成，剩余为控制面能力探测、镜像构建和临时节点实机验收。
