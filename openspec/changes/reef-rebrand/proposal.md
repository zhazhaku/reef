---
change: reef-rebrand
schema: spec-driven
status: proposed
---

# Proposal: 项目重命名为 Reef

## 概述

将项目从 **picoclaw** (GitHub: `zhazhaku/picoclaw`) 重命名为 **reef** (GitHub: `zhazhaku/reef`)。

## 动机

1. **方向转变**：原始 picoclaw 是个人 AI 助手，而本 fork 的方向是**分布式多智能体编排系统 (Swarm Orchestration)**
2. **命名一致性**：所有 Swarm 组件 (Reef Server/Client/Protocol/DAG Engine) 已经以 "reef" 命名，但项目根名叫 picoclaw 造成认知混乱
3. **独立品牌**：作为独立的项目，需要自己的标识，同时保留上游归属声明

## 变更范围

### 必须变更（硬性）

| 类别 | 当前 | 目标 |
|------|------|------|
| Go Module | `github.com/sipeed/picoclaw` | `github.com/zhazhaku/reef` |
| 二进制名 | `picoclaw` | `reef` |
| CLI 命令 | `picoclaw agent/onboard/server/...` | `reef agent/onboard/server/...` |
| 环境变量 | `PICOCLAW_HOME` | `REEF_HOME` |
| 默认目录 | `~/.picoclaw` | `~/.reef` |
| Docker 基础镜像引用 | `picoclaw` 相关 | 更新为 `reef` |
| 内部 import 路径 | `github.com/sipeed/picoclaw/pkg/...` | `github.com/zhazhaku/reef/pkg/...` |
| 源码中硬编码的 "picoclaw" 字符串 | ~60+ 处 | "reef" |

### 必须保留（软性）

- **上游声明**：所有文件头注释保留 `// PicoClaw - Ultra-lightweight personal AI agent` → 改为 `// Reef - Distributed multi-agent swarm orchestration system (based on PicoClaw)`
- **docs/ 目录**：任何提及 picoclaw 的引用改为 "Reef (based on PicoClaw)"
- **LICENSE / NOTICE**：保留原始许可证，添加派生声明

### 影响统计

| 项 | 数量 |
|----|------|
| import 路径需修改的 .go 文件 | ~470 |
| 二进制名/命令名引用 | ~40 处 |
| PICOCLAW_HOME 引用 | ~33 处 |
| 硬编码 "picoclaw" 字符串 | ~60+ 处 |
| Makefile / Dockerfile | ~15 个文件 |
| README / 文档 | ~30 个 .md 文件 |
| Web 前端引用 | ~30 个 .ts/.tsx 文件 |

## 不变的部分

- `openspec/` 中的所有现有设计文档和 plan — 仅新增 rebrand proposal，不删除历史
- `upstream` remote 指向 sipeed/picoclaw 不变
- 代码逻辑 / API / 核心功能

## 风险

1. **构建中断**：模块路径全局替换需一次性原子完成，否则编译失败
2. **import 遗漏**：469 个文件的 import 是一个不留就会编译失败
3. **前端构建**：package.json / vite config 中的路径引用也需要更新
4. **git history**：git remote rename + go.mod replace 得当，不影响历史

## 建议的执行方式

使用 Go 工具链原生支持 + 脚本批量替换：

```bash
# 1. 修改 go.mod module 行
# 2. 使用 gorename / go mod tidy 验证
# 3. find+sed 批量替换 import 路径
# 4. 环境变量 PICOCLAW_HOME → REEF_HOME
# 5. 二进制名 picoclaw → reef
# 6. README 重写（中英双版）
# 7. go build + go test 全量验证
```
