# Design: 项目重命名为 Reef

## 1. 新身份

```
项目名:    Reef
二进制:    reef
Module:    github.com/zhazhaku/reef
描述:      Distributed multi-agent swarm orchestration system
上游:      基于 PicoClaw (github.com/sipeed/picoclaw)
```

## 2. 逐层替换策略

### Layer 1: Go Module (根本)

```
旧: module github.com/sipeed/picoclaw
新: module github.com/zhazhaku/reef
```

所有 `import "github.com/sipeed/picoclaw/..."` → `import "github.com/zhazhaku/reef/..."`

### Layer 2: 命令和二进制

```go
// cmd/reef/main.go (原 cmd/picoclaw/main.go)
var rootCmd = &cobra.Command{
    Use:   "reef",      // 原 "picoclaw"
    Short: "Reef - Distributed multi-agent swarm orchestration",
}
```

目录重命名：`cmd/picoclaw/` → `cmd/reef/`

### Layer 3: 环境变量和路径

```
PICOCLAW_HOME        → REEF_HOME
~/.picoclaw/          → ~/.reef/
PICOCLAW_CONFIG_PATH  → REEF_CONFIG_PATH
```

在 `pkg/config/` 中修改常量定义，保持向后兼容读取（先读 REEF_HOME，fallback 读 PICOCLAW_HOME）。

### Layer 4: 文件头注释

```go
// 旧 (每一 .go 文件):
// PicoClaw - Ultra-lightweight personal AI agent

// 新:
// Reef - Distributed multi-agent swarm orchestration system
// Based on PicoClaw (github.com/sipeed/picoclaw)
```

### Layer 5: Web 前端

- `web/frontend/package.json`: project name → "reef"
- `web/frontend/src/components/app-header.tsx`: title → "Reef"
- 所有 import 路径中的 `/api/picoclaw/...` → `/api/reef/...` (如果有的话)

### Layer 6: Docker + Makefile

- Makefile: `BINARY_NAME=reef`
- Dockerfile: 二进制名为 `/app/reef`
- docker-compose: image 标签更新

## 3. README 新结构

```markdown
# 🪸 Reef

**Distributed Multi-Agent Swarm Orchestration System**

> Based on [PicoClaw](https://github.com/sipeed/picoclaw) — the ultra-lightweight personal AI agent.
> Reef extends PicoClaw into a distributed swarm architecture with multi-agent coordination,
> priority scheduling, DAG workflow execution, and cross-channel task routing.

## 💡 What is Reef? (中文)

Reef（珊瑚礁）是一个分布式多智能体编排系统...

## ✨ Features

- 🌊 Swarm Orchestration (Reef Server + Client)
- 📊 Priority-based Scheduling with pluggable strategies
- 🔗 DAG Engine for complex workflow dependencies
- 🔄 Cross-channel task routing (ReplyTo)
- 🖥️ Web UI for swarm monitoring
- 🧠 Hermes role-based agent delegation

## 🏗️ Architecture

[原有架构图，标注 Reef 组件]

## 📦 Installation

```bash
go install github.com/zhazhaku/reef/cmd/reef@latest
```

## 🙏 Credits

Reef is built upon [PicoClaw](https://github.com/sipeed/picoclaw) by [Sipeed](https://github.com/sipeed).
We are grateful for their excellent foundation.
```

## 4. 保持兼容的规则

1. **环境变量兼容**：`REEF_HOME` 优先，fallback `PICOCLAW_HOME`（带 deprecation warning）
2. **配置目录兼容**：`~/.reef/` 优先，fallback `~/.picoclaw/`
3. **Channel 名称**：swarm channel 内部 host 名从 "picoclaw" → "reef"
4. **OAuth originator**：`picoclaw` → `reef`
5. **Matrix bot nick**：`picoclaw` → `reef`

## 5. 不修改的部分

- `pkg/` 核心逻辑文件名不变（server/scheduler/queue/...）
- `go.sum` 校验和不变（go mod tidy 自动更新）
- Git remote `upstream` 指向 sipeed/picoclaw 不变
- 所有 openspec/ 历史文档不加修改
- LICENSE 文件不变，仅添加派生命名声明
