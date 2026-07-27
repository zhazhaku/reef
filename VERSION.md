# Agent Auto Loop CLI — v2.0.0

## 版本信息
- **Version:** 2.0.0
- **Code Name:** Agent Auto Loop
- **Build Date:** 2026-07-27

## 核心功能
- **Auto Loop Orchestrator** — 自动/手动/聊天/Hermes 四模式切换
- **Client Pool** — 客户端进程生命周期管理（生成/监控/销毁）
- **Task Planner** — 串行/并行/混合 DAG 任务规划
- **Healer** — 自动重试（指数退避）+ 故障客户端迁移
- **AutoScaler** — 基于队列深度的动态扩缩容
- **CLI 命令簇** — 8 个子命令 + 别名（`/auto`、`/autotask`、`/autoloop`）

## 文件统计
- 13 个源文件，~5,400 行代码
- 3 个测试文件，35 个测试，全部 PASS
- 6 个已有文件修改
