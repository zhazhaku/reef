# Reef — 分布式多智能体编排平台

[![Go Version](https://img.shields.io/badge/Go-1.26%2B-blue)](https://golang.org/dl/)
[![License](https://img.shields.io/badge/License-Apache%202.0-blue.svg)](LICENSE)

> **Reef** 是一个分布式多智能体编排系统，基于 Raft 共识和 WebSocket 通信。

---

## 架构

```
┌──────────────────────────────────────────────┐
│              Reef Server                       │
│  ┌──────────┐  ┌──────────┐ ┌───────────┐   │
│  │ Scheduler│  │ Registry │ │   Hermes   │   │
│  └────┬─────┘  └────┬─────┘ └─────┬─────┘   │
│       └──────────────┼────────────┘          │
│              ┌───────┴───────┐              │
│              │   Raft 共识    │              │
│              └───────────────┘              │
└──────────────────────┬───────────────────────┘
                       │ WebSocket
        ┌──────────────┼──────────────┐
   ┌────┴────┐   ┌────┴────┐   ┌────┴────┐
   │ coder   │   │ tester  │   │ analyst │
   └─────────┘   └─────────┘   └─────────┘
```

## 功能

| 功能 | 状态 |
|------|:---:|
| 多 Agent 角色编排 | ✅ |
| WebSocket 通信 (33 种消息) | ✅ |
| Raft 共识集群 | ✅ |
| Hermes 三层约束 (Coordinator/Executor/Full) | ✅ |
| 任务全生命周期 | ✅ |
| GEP 进化引擎 | ✅ |
| P8 认知架构 (4 层上下文) | ✅ |
| SQLite 持久化队列 | ✅ |
| TLS 支持 | ✅ |
| 多通道通知 (6 种) | ✅ |
| Web UI SPA 仪表盘 (10 页面) | ✅ |
| 22 个后端 API | ✅ |
| 47 个单元+集成测试 | ✅ |
| LLM 调用自动恢复 (3 层) | ✅ |

## 快速开始

```bash
# 构建
CGO_ENABLED=0 GONOSUMCHECK='*' GONOSUMDB='*' go build -o reef-server ./cmd/reef/

# 启动 Server
./reef-server --mode server --config config.json

# 访问 Web UI: http://localhost:8080/ui/
```

## 配置

```json
{
  "store_type": "sqlite",
  "store_path": "/var/reef/data/reef.db",
  "hermes": {"mode": "coordinator", "fallback": true},
  "agents": {"defaults": {
    "llm_retry_max_attempts": 3,
    "scheduled_retry_interval_minutes": 10
  }}
}
```

## Web UI

| 页面 | 路由 | 功能 |
|------|------|------|
| Dashboard | `/` | 统计 + 4 图表 |
| Board | `/board` | Kanban 拖拽 |
| Chatroom | `/chatroom` | 实时聊天 |
| Clients | `/clients` | Agent 监控 |
| Tasks | `/tasks` | 任务管理 |
| Evolution | `/evolution` | Gene 进化 |
| Hermes | `/hermes` | 模式配置 |
| Config | `/config` | 系统配置 |
| Activity | `/activity` | 事件流 |
| Monitoring | `/monitoring` | 实时日志 |

## 技术栈

- **后端**: Go 1.26, Chi, gorilla/websocket, etcd/raft, SQLite
- **前端**: Vanilla JS SPA, Chart.js (205KB)
- **Agent**: PicoClaw (多 LLM Provider)

## 许可证

Apache 2.0
