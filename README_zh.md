# 🪸 Reef（珊瑚礁）

**分布式多智能体集群编排系统**

> 基于 [PicoClaw](https://github.com/sipeed/picoclaw) —— [Sipeed](https://sipeed.com) 出品的超轻量个人 AI 助手。
> Reef 将 PicoClaw 的单智能体架构扩展为分布式集群，支持多智能体协同、
> 优先级调度、DAG 工作流执行和跨频道任务路由。

[English](./README.md)

---

## 💡 什么是 Reef？

Reef（珊瑚礁）是一个用 Go 语言编写的**分布式多智能体集群编排系统**。它将 PicoClaw 的单智能体架构转化为**协同集群**：

- **Reef Server**（Hermes 协调者）接收任务并分发到可用的工作节点
- **Reef Client** 注册自身能力（角色、技能、容量）并执行任务
- **优先级调度**，支持可插拔策略（最少负载、技能匹配、轮询）
- **DAG 引擎**处理复杂的多步骤工作流，含依赖追踪
- **跨频道任务路由**——通过 Telegram/飞书等频道提交的任务，结果会回到原频道
- **Web 控制台**实时监控集群状态

## ✨ 特性

- 🌊 **集群编排**——Server + Client 节点，WebSocket 协议通信
- 🧠 **Hermes 角色委派**——协调者 / 执行者 / 全能 三种能力模型
- 📊 **优先级调度**——可插拔 MatchStrategy，默认最少负载
- 🔗 **DAG 工作流引擎**——子任务、依赖关系、结果聚合
- 🔄 **跨频道 ReplyTo**——任务结果自动路由回源频道
- 🖥️ **Web 仪表盘**——实时统计、任务列表、客户端监控、SSE 事件
- ⚡ **超轻量**——继承 PicoClaw 的 <10MB 内存占用
- 🔌 **持久化存储**——SQLite 任务存储，内存模式作为后备

## 🏗️ 架构

```
┌─────────────────────────────────────────────┐
│                 Reef Server                   │
│  ┌─────────┐  ┌──────────┐  ┌────────────┐  │
│  │ 优先级  │→│  调度器   │→│  注册中心   │  │
│  │  队列   │  │ Scheduler│  │ (Clients)  │  │
│  └─────────┘  └─────┬─────┘  └──────┬─────┘  │
│                     │               │        │
│              ┌──────▼──────┐        │        │
│              │  DAG 引擎   │        │        │
│              └──────┬──────┘        │        │
│                     │               │        │
│              ┌──────▼───────────────▼───┐    │
│              │     WebSocket 协议        │    │
│              └──────┬───────────────────┘    │
└─────────────────────┼────────────────────────┘
                      │
        ┌─────────────┼─────────────┐
        │             │             │
   ┌────▼────┐   ┌────▼────┐   ┌────▼────┐
   │ Client  │   │ Client  │   │ Client  │
   │ 执行者  │   │ 分析师  │   │  全能   │
   │ python  │   │  sql    │   │  all    │
   └─────────┘   └─────────┘   └─────────┘
```

## 📦 快速开始

### 安装

```bash
go install github.com/zhazhaku/reef/cmd/reef@latest
```

### 启动 Server

```bash
reef server
```

### 启动 Client

```bash
reef client --server ws://localhost:8765 --role executor --skills python,bash
```

### Web 控制台

浏览器打开 `http://localhost:8080/reef/overview`

## 📊 Reef Scheduler v2

调度器已完成重大升级：

| 能力 | 说明 |
|------|------|
| 优先级队列 | 按紧急程度排序任务 (1-10) |
| 匹配策略 | 可插拔的客户端选择 (最少负载、技能匹配) |
| DAG 引擎 | 子任务创建、依赖追踪、自动解阻塞 |
| ReplyTo | 跨频道结果路由 |
| 持久化 | SQLite 任务存储 |
| SSE 事件 | 实时 Web UI 更新 |

详见 [reef-scheduler-v2 设计文档](./openspec/changes/reef-scheduler-v2/)

## 📁 项目结构

```
reef/
├── cmd/reef/          # CLI 命令行
├── pkg/
│   ├── reef/          # 集群核心 (Task, Protocol, Bridge)
│   │   ├── server/    # 调度器, DAG, 队列, 注册中心
│   │   └── client/    # 客户端连接器 & 任务执行器
│   ├── agent/         # Hermes 智能体 (协调者/执行者)
│   ├── gateway/       # 频道网关集成
│   └── ...
├── openspec/          # 设计文档 & 规格说明
├── web/
│   ├── backend/       # Go API 服务
│   └── frontend/      # React + TypeScript 界面
└── docker/            # Docker & Compose 配置
```

## 👥 贡献

欢迎贡献！查看 [OpenSpec proposals](./openspec/) 了解当前设计决策。

## 📄 许可

MIT — 基于 PicoClaw。原始 PicoClaw 许可保留。

---

## 🙏 致谢

**Reef** 构建于 **[PicoClaw](https://github.com/sipeed/picoclaw)** 之上——[Sipeed](https://sipeed.com) 出品的超轻量个人 AI 助手。

PicoClaw 提供了坚实的基础：Go 原生智能体架构、多渠道消息系统和工具框架。Reef 将这一基础扩展为分布式集群编排平台。

我们深深感谢 PicoClaw 社区和维护者的卓越工作。
