# Tasks: 项目重命名为 Reef

## Phase 1: Go Module 和 Import 路径（核心）

- [ ] **1.1** 修改 `go.mod`：`module github.com/sipeed/picoclaw` → `module github.com/zhazhaku/reef`
- [ ] **1.2** find + sed 全局替换所有 `.go` 文件中的 `github.com/sipeed/picoclaw` → `github.com/zhazhaku/reef` (~470 files)
- [ ] **1.3** find + sed 替换 Makefile / Dockerfile / .goreleaser.yaml 中的 `picoclaw` → `reef`
- [ ] **1.4** `go mod tidy` 更新 go.sum
- [ ] **1.5** `go build ./...` 验证全量编译通过

## Phase 2: 二进制和命令名

- [ ] **2.1** 目录重命名：`cmd/picoclaw/` → `cmd/reef/`
- [ ] **2.2** 修改 `cmd/reef/main.go`：`Use: "reef"`, Short/Description 更新
- [ ] **2.3** 修改 `cmd/reef/main_test.go`：断言更新
- [ ] **2.4** 修改 `cmd/picoclaw-launcher-tui/` 中对 `picoclaw` 二进制的引用 → `reef`
- [ ] **2.5** 修改 Makefile：`BINARY_NAME=reef`

## Phase 3: 环境变量和路径

- [ ] **3.1** `pkg/config/`：`PICOCLAW_HOME` → `REEF_HOME` 常量
- [ ] **3.2** `pkg/config/`：`~/.picoclaw` → `~/.reef` 默认路径
- [ ] **3.3** 其他文件中的 `PICOCLAW_HOME` 字符串替换 (~33处)
- [ ] **3.4** 添加向后兼容 fallback：`REEF_HOME` 不存在时读 `PICOCLAW_HOME`

## Phase 4: 文件头注释

- [ ] **4.1** 批量替换所有 .go 文件头 `PicoClaw - Ultra-lightweight personal AI agent` → `Reef - Distributed multi-agent swarm orchestration system`
- [ ] **4.2** 添加 `Based on PicoClaw (github.com/sipeed/picoclaw)` 归属行

## Phase 5: 硬编码字符串

- [ ] **5.1** OAuth originator: `"picoclaw"` → `"reef"`
- [ ] **5.2** Matrix bot nick/default: `"picoclaw"` → `"reef"`
- [ ] **5.3** Swarm host default: `"picoclaw"` → `"reef"`
- [ ] **5.4** MCP name default: `"picoclaw"` → `"reef"`
- [ ] **5.5** CLI 测试中的字符串 `"picoclaw"` → `"reef"`
- [ ] **5.6** Skills 相关路径中的 `"picoclaw"` → `"reef"`
- [ ] **5.7** 测试文件中的 `"picoclaw"` 字符串引用 (~10处)

## Phase 6: Web 前端

- [ ] **6.1** `web/frontend/package.json`：name → `"reef"`
- [ ] **6.2** `web/frontend/src/components/app-header.tsx`：title → `"Reef"`
- [ ] **6.3** 其他 .ts/.tsx 文件中的 "picoclaw" 字符串替换 (~30处)
- [ ] **6.4** 验证前端构建不报错

## Phase 7: Docker & CI

- [ ] **7.1** Dockerfile 中的二进制路径：`picoclaw` → `reef`
- [ ] **7.2** docker-compose.yml 中的 image 名称
- [ ] **7.3** `.goreleaser.yaml` 中的 builds 配置

## Phase 8: README 重写

- [ ] **8.1** 新建 `README.md`：英文版（含 PicoClaw 致谢）
- [ ] **8.2** 新建 `README_zh.md`：中文版（含 PicoClaw 致谢）
- [ ] **8.3** 两个 README 互链（`[中文](./README_zh.md)` / `[English](./README.md)`）
- [ ] **8.4** 更新安装/构建说明
- [ ] **8.5** 更新 docs/ 中相关引用

## Phase 9: 验证与提交

- [ ] **9.1** `go test ./...` 全量测试通过
- [ ] **9.2** `go vet ./...` 零警告
- [ ] **9.3** `go build -o /dev/null ./cmd/reef/` 二进制构建成功
- [ ] **9.4** Git commit with message: `rebrand: picoclaw → reef`
- [ ] **9.5** 更新 openspec/changes/reef-rebrand 状态为 complete

## Phase 10: 远程仓库

- [ ] **10.1** 确认 `zhazhaku/reef` 是否存在
- [ ] **10.2** 清空/删除远程仓库内容（避免冲突）
- [ ] **10.3** git remote add origin `https://github.com/zhazhaku/reef.git`
- [ ] **10.4** `git push -u origin main --force` 推送

## 任务统计

| Phase | 任务数 | 优先级 |
|-------|--------|--------|
| 1: Go Module | 5 | P0 |
| 2: 二进制名 | 5 | P0 |
| 3: 环境变量 | 4 | P0 |
| 4: 文件头 | 2 | P1 |
| 5: 字符串 | 7 | P1 |
| 6: Web 前端 | 4 | P1 |
| 7: Docker | 3 | P2 |
| 8: README | 5 | P1 |
| 9: 验证提交 | 5 | P0 |
| 10: 远程仓库 | 4 | P0 |
| **合计** | **44** | — |
