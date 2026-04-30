# Spec: Reef Rebrand

## 身份定义

- **项目名称**：Reef
- **二进制名称**：`reef`
- **Go Module**：`github.com/zhazhaku/reef`
- **描述**：Distributed multi-agent swarm orchestration system

## 环境变量

| 旧 | 新 | 兼容 |
|----|-----|------|
| `PICOCLAW_HOME` | `REEF_HOME` | fallback PICOCLAW_HOME |
| `~/.picoclaw/` | `~/.reef/` | fallback ~/.picoclaw/ |

## 文件头注释

```go
// Reef - Distributed multi-agent swarm orchestration system
// Based on PicoClaw (github.com/sipeed/picoclaw)
```

## Web 前端

- `package.json`: `"name": "reef"`
- App header title: `"Reef"`
- 所有 "picoclaw" 引用更新为 "reef"

## Docker

- 二进制路径: `/app/reef`
- Image: `zhazhaku/reef:latest`

## CLI

```
$ reef agent    # 启动 Agent
$ reef server   # 启动 Reef Server (Hermes Coordinator)
$ reef onboard  # 初始化配置
```

## README

- 中英双语
- 明确标注 "Based on PicoClaw"
- 保留上游链接和致谢

## 不变更

- `go.sum` 中的依赖版本
- 原始 LICENSE
- `upstream` git remote
- `openspec/` 历史文档
