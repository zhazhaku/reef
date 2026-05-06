# Proposal: Hermes UI — Production-Grade Multi-Agent Dashboard

> change: reef-hermes-ui
> schema: spec-driven
> status: proposed
> created: 2026-05-05

---

## 1. 背景与动机

当前 Reef Web UI 为 v2.0 嵌入式 SPA，提供基本的任务列表、客户端状态和实时 SSE 更新。但缺少：

- **全系统配置能力**：无法通过 UI 修改 Hermes 模式、Client 角色、技能集
- **Client 精细化管理**：无法查看 Client 实时执行内容、会话历史、思考链（reasoning_content）
- **Hermes 专用视图**：Coordinator/Executor/Full 三种模式下应有不同的 UI 焦点
- **操作能力**：无法通过 UI 取消/暂停/恢复任务、重启 Client、切换模式
- **生产级运维**：缺少日志查看、性能图表、告警配置、备份恢复

参考项目：Multica、Evolver、Ruflo、AutoGen Studio、Langflow、CrewAI、MetaGPT

## 2. 目标

打造一个 **生产级 Hermes 运维面板**，使运维人员能够：

1. **全局配置** — 管理所有 Client 的配置、角色、技能、模型参数
2. **实时监控** — 查看所有 Client 的执行状态、思考链、工具调用、Token 消耗
3. **精细操作** — 取消/暂停/恢复任务、重启 Client、切换 Hermes 模式
4. **Hermes 适配** — 根据当前 HermesMode 显示不同的 UI 焦点
5. **生产运维** — 日志、告警、性能、备份

## 3. 范围

### In Scope
- 配置管理页面（Server/Client/Hermes/通知）
- Client 实时监控（思考链、工具调用、推理内容）
- 任务操作（取消/暂停/恢复）
- Hermes 模式切换面板
- 性能指标页面
- 暗色/亮色主题

### Out of Scope
- 移动端 App
- 多租户支持
- SSO/OAuth 集成
- 自定义仪表盘（后续）

---

*Version 0.1 — 待补充完整 design.md*
