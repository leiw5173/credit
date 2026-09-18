# Credis 上游基线

采集日期：2026-09-18  
采集环境：隔离 Git worktree，仅使用全新本地测试数据；未访问生产系统。

## 仓库固定

| 项目 | 记录 |
|---|---|
| `origin` | `https://github.com/leiw5173/credit.git` |
| 约定 `upstream` | `https://github.com/linux-do/credit.git` |
| 分支 | `master` |
| 固定 SHA | `2d09c890e06e7c12906f1b641e8248646816590a` |
| LICENSE | Apache-2.0 |
| LICENSE SHA-256 | `605edfddc7c4228f53ed2ffba4eb6585f3256d9a69beeffeb677add391721d14` |
| Go | `go.mod` 声明 `go 1.26` |
| 前端 | Next.js 16.1.1、React 19.2.3 |

`upstream` URL 已核实，但当前执行环境拒绝修改 Git remote 配置；因此本次未在本地写入该 remote。同步前必须由获授权环境按 `upstream-sync.md` 配置并复核。

## 原始证据

### `git branch -r`

```text
origin/HEAD -> origin/master
origin/master
```

### `sha256sum LICENSE`

```text
605edfddc7c4228f53ed2ffba4eb6585f3256d9a69beeffeb677add391721d14 *credis/LICENSE
```

### 工具链

```text
go version: 未运行；当前 PATH 无 go
node --version: v26.8.1（C:\Users\wanglei\AppData\Local\Programs\nodejs\node.exe）
pnpm --version: 未运行；当前 PATH 无 pnpm
docker version: 未运行；当前 PATH 无 docker
```

## 基线测试结果

| 检查 | 结果 | 说明 |
|---|---|---|
| `go test ./...` | NOT RUN | Go 1.26 不可用；不记为 PASS |
| `pnpm install --frozen-lockfile` | NOT RUN | pnpm 不可用；不记为 PASS |
| `pnpm lint` | NOT RUN | 依赖未安装且 pnpm 不可用 |
| `pnpm build` | NOT RUN | 依赖未安装且 pnpm 不可用 |
| API 健康检查 | NOT RUN | Docker、隔离 PostgreSQL/Redis 不可用 |
| Worker 任务日志 | NOT RUN | Docker、隔离 PostgreSQL/Redis 不可用 |
| Scheduler 任务日志 | NOT RUN | Docker、隔离 PostgreSQL/Redis 不可用 |

以上未运行项是 Task 1 的未满足验收门槛。提供 Go 1.26、pnpm、Docker、隔离 PostgreSQL/Redis 后，须原样补跑并附完整日志；不得以此文档替代测试。

## 可复现检查

```bash
git submodule update --init --recursive
git -C credis rev-parse HEAD
sha256sum credis/LICENSE
git -C credis status --short
```

预期 SHA 为 `2d09c890e06e7c12906f1b641e8248646816590a`，工作树无改动（基线文档提交后）。
