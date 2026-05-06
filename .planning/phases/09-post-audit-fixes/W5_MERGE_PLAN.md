# W5: 代码库统一 — 合并方案与执行报告

> Phase 09 — 审计修复 Wave 5
> 状态: 🟡 进行中（import 路径已统一，目录合并待执行）

---

## 确认清单

| 问题 | 原状 | 现在 | 状态 |
|------|------|------|:---:|
| 主仓库 `go.mod` | `github.com/sipeed/reef` | `github.com/zhazhaku/reef` | ✅ |
| Picoclaw `go.mod` | `github.com/zhazhaku/reef` | 已一致 | ✅ |
| 主仓库 Go import | 38 文件 `sipeed/reef` | 全部 `zhazhaku/reef` | ✅ |
| 配置路径 | `~/.picoclaw/` | `~/.reef/` | ✅ |
| 环境变量 | `PICOCLAW_HOME` | `REEF_HOME` | ✅ |
| CLI 命令 | `picoclaw agent` | `reef agent` | ✅ |
| Agent 身份 | `picoclaw 🦞` | `reef 🪸` | ✅ |
| 临时目录 | `picoclaw_media/` | `reef_media/` | ✅ |

---

## 合并后目标架构

```
reef/                                    ← github.com/zhazhaku/reef
├── go.mod
├── cmd/reef/                            ← 来自 picoclaw (cobra CLI)
├── pkg/
│   ├── reef/                            ← 合并后唯一 pkg/reef/
│   │   ├── protocol.go                  ← 取 picoclaw 版本（reply_to 支持）
│   │   ├── task.go                      ← 取 picoclaw 版本
│   │   ├── dag.go                       ← 保留（仅在主仓库）
│   │   ├── cnp_messages.go              ← 保留（仅在主仓库）
│   │   ├── client_info.go              ← 保留
│   │   ├── error_classifier.go          ← W3 新增
│   │   ├── client/                      ← 取 picoclaw 版本（增强）
│   │   ├── server/                      ← 取 picoclaw 版本（34 文件 vs 7）
│   │   ├── evolution/                   ← 取 picoclaw 完整 GEP 管道
│   │   ├── raft/                        ← 保留主仓库（唯一独有）
│   │   └── role/                        ← 取 picoclaw 版本
│   ├── agent/                           ← P8 认知架构（picoclaw）
│   ├── memory/                          ← 记忆系统（picoclaw）
│   ├── skills/                          ← 技能系统（picoclaw）
│   ├── channels/                        ← 频道+swarm（picoclaw）
│   ├── gateway/                         ← 网关集成（picoclaw）
│   ├── config/                          ← 配置（picoclaw）
│   ├── providers/                       ← LLM 提供商（picoclaw）
│   └── tools/                           ← 工具系统（picoclaw）
```

---

## 删除清单（主仓库冗余文件）

| 路径 | 原因 |
|------|------|
| `pkg/reef/protocol.go` | picoclaw 版本更全 |
| `pkg/reef/task.go` | picoclaw 版本一致 |
| `pkg/reef/client/` 全部 | picoclaw 版本增强 |
| `pkg/reef/server/` 全部 | picoclaw 34 文件 vs 7 |
| `pkg/reef/evolution/` 除 server/claim_board | picoclaw 完整 GEP 管道 |
| `pkg/channels/swarm/swarm.go` | picoclaw 版本更新 |
| `pkg/bus/bus.go` | picoclaw 一致 |

---

## 保留清单（主仓库独有文件 → 并入）

| 路径 | 说明 |
|------|------|
| `pkg/reef/dag.go` | DAG 节点定义 |
| `pkg/reef/cnp_messages.go` | 16 认知消息类型 |
| `pkg/reef/client_info.go` | ClientInfo 结构体 |
| `pkg/reef/error_classifier.go` | W3 IsRetryable |
| `pkg/reef/raft/` 全部 (6 文件) | BoltStore + RaftNode + Transport + Pool + FSM |

---

## 已执行

### Step 1: 导入路径统一 ✅

```
38 files: sed s|github.com/sipeed/reef|github.com/zhazhaku/reef|g
go.mod: module github.com/zhazhaku/reef

测试结果:
  pkg/reef            ✓
  pkg/reef/server     ✓
  pkg/reef/evolution  ✓
  pkg/reef/role       ✓
```

### Step 2: picoclaw → reef 重命名 ✅

```
160 files: .picoclaw→.reef, PICOCLAW_HOME→REEF_HOME, 🦞→🪸
CLI, identity, env vars, temp dirs 全部统一
```

---

## 待执行

### Step 3: 文件级合并

```bash
# 将 picoclaw 增强文件复制到主仓库
cp -r picoclaw/pkg/reef/server/* pkg/reef/server/
cp -r picoclaw/pkg/reef/evolution/* pkg/reef/evolution/
cp -r picoclaw/pkg/reef/client/* pkg/reef/client/
cp -r picoclaw/pkg/reef/role/* pkg/reef/role/
cp picoclaw/pkg/reef/protocol.go pkg/reef/protocol.go
cp picoclaw/pkg/reef/task.go pkg/reef/task.go

# 删除冗余
rm pkg/reef/server/ (主仓库旧 server/ 被覆盖)
```

### Step 4: 验证

```bash
go build ./pkg/reef/...
go test ./pkg/reef/...
go vet ./pkg/reef/...
```

### Step 5: Raft 依赖修复

```bash
go get go.etcd.io/raft/v3
go get go.etcd.io/bbolt
go get github.com/gogo/protobuf/proto
```

---

## 预估

| Step | 状态 | 耗时 |
|------|:---:|:----:|
| 1. 导入路径统一 | ✅ 已完成 | 30 min |
| 2. picoclaw→reef | ✅ 已完成 | 30 min |
| 3. 文件级合并 | 🟡 待执行 | 1 hr |
| 4. 编译验证 | 🟡 待执行 | 30 min |
| 5. Raft 依赖修复 | 🟡 待执行 | 30 min |

---

*报告生成时间 2026-05-03 | 待审核*
