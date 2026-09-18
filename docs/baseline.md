# Credis 上游基线

采集日期：2026-09-18  
采集环境：隔离 Git worktree，仅使用全新本地测试数据；未访问生产系统。

## 仓库固定

| 项目 | 记录 |
|---|---|
| `origin` | `https://github.com/leiw5173/credit.git` |
| 约定 `upstream` | `https://github.com/linux-do/credit.git` |
| 分支 | `master` |
| 上游源码基线 SHA | `2d09c890e06e7c12906f1b641e8248646816590a` |
| 首次本地文档提交 | `7705ec22d18b7f7c4ad55592cce7c373089dcb7f`（后续修订以父仓库 gitlink 为准） |
| LICENSE | Apache-2.0 |
| LICENSE SHA-256 | `605edfddc7c4228f53ed2ffba4eb6585f3256d9a69beeffeb677add391721d14` |
| Go | `go.mod` 声明 `go 1.26` |
| 前端 | Next.js 16.1.1、React 19.2.3 |

`upstream` 已按明确授权配置为 `https://github.com/linux-do/credit.git`。本次仅新增 remote 配置，没有抓取、合并或推送；同步时仍必须按 `upstream-sync.md` 复核 URL 和差异。

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
git rev-parse HEAD:credis
git -C credis rev-parse HEAD
git -C credis merge-base --is-ancestor 2d09c890e06e7c12906f1b641e8248646816590a HEAD
sha256sum credis/LICENSE
git -C credis status --short
```

两条 `rev-parse` 输出必须相同（父仓库 gitlink = submodule HEAD）。`2d09c890e06e7c12906f1b641e8248646816590a` 是上游源码基线，必须是当前 HEAD 的祖先，而不是文档提交后的 HEAD。远端 Fork 尚未提供父仓库指向的本地文档提交，全新克隆验证仍为 **BLOCKED**；本地已有对象的 `submodule update` 成功不能充当远端可复现证据。
