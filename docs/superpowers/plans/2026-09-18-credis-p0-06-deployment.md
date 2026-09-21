# Credis P0 部署与上线验收 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 提供同机隔离部署、真实 Discourse 升级与板块标签验收、可复现 CI、备份恢复和独立环境拆分演练，在上线门槛未满足时阻止正式结算。

**Architecture:** `/var/discourse` 用 Discourse 官方 Docker，`/opt/credis` 用独立 Compose 项目部署前端/API/Worker/Scheduler/PostgreSQL/Redis；外部反向代理只暴露两个 HTTPS 域名。生产切换用单接收代理、最终增量同步、旧主库降权/停写和受控新一代租约栅栏，不靠 DNS TTL 避免双写。

**Tech Stack:** Docker Compose、Discourse 官方 Docker、PostgreSQL、Redis、Go/Next.js、GitHub Actions、HTTPS reverse proxy。

**Spec:** `docs/superpowers/specs/2026-09-18-credis-p0-design.md` §2.3、§3、§10–17；依赖 01–05 的完整验收证据。

## Global Constraints

- 当前**不读取、导入或修改真实论坛数据**；当前本地 Discourse 用全新测试数据；生产脱敏副本只在后续预发布升级验收使用，需单独授权/数据治理审批。
- 不切生产 DNS，不执行实际生产迁移；真正论坛数据迁移仅写独立清单，部署验收后另起实施。
- Discourse 稳定/ESR 版本与 Credis 镜像固定具体 tag/commit/digest；不运行浮动 `latest`；许可证和上游 SHA 随每次发布记录。
- 同机不得共享 PostgreSQL 实例/用户/目录、Redis、Compose 项目、Docker 网络/卷、Secret、备份/日志/迁移目录；仅通过 HTTPS Webhook 与 REST 通信。
- 升级验收、板块/标签迁移 dry-run、论坛内置积分置零和基线、账本/真实论坛端到端证据全部通过后才允许正式 LDC 结算。

---

## File Structure

- `credis/deploy/compose.yaml`, `credis/deploy/.env.example`, `credis/deploy/proxy.example.conf`：六角色隔离部署及代理示例；`credis/Dockerfile`、`credis/frontend/Dockerfile`：固定发布镜像构建。
- `credis/.github/workflows/ci.yml`：Go/TS/PG/Redis/迁移/flag/安全/镜像验证。
- `discourse-deployment/{single-host-deployment.md,upgrade-acceptance.md,category-tag-inventory.md}`：部署、升级、板块/标签盘点证据。
- `migration/{category-tag-dry-run.md,forum-production-migration.md,credis-split-host-migration.md}`：影响报告、未来论坛迁移清单、拆分演练。
- `credis/internal/lease/{fence.go,fence_test.go}`、`credis/internal/db/migrator/sql/0006_credis_fencing.up.sql`：主库租约/栅栏；`credis/docs/release-gates.md`：证据清单、go/no-go 与回退条件。

### Task 1: 同机隔离模板与恢复演练

**Files:** Create `credis/deploy/{compose.yaml,.env.example,proxy.example.conf}`, `discourse-deployment/single-host-deployment.md`; update `credis/config.example.yaml` with configurable URLs and Secret references.

**Interfaces:** `FORUM_BASE_URL`, `CREDIS_PUBLIC_URL`, `DISCOURSE_WEBHOOK_SECRET_FILE`, `DISCOURSE_API_KEY_FILE`, `POSTGRES_PASSWORD_FILE`, `REDIS_PASSWORD_FILE` 均由环境/Secret 管理；业务仅使用 `https://credis.example.com/webhooks/discourse` 与论坛 REST base URL 配置，不使用跨系统容器名/IP/跨库 SQL。

- [ ] **Step 1: 写失败的拓扑静态检查。** 解析 Compose 模板：六个 service `frontend/api/worker/scheduler/postgres/redis`；仅 proxy-to-app 本地入口暴露，DB/Redis 不映射公网；Credis 网络/volume/项目名与 Discourse 独立；拒绝明文 Secret、共享 `/var/discourse` 挂载、`latest` 镜像。对未创建模板先运行检查确认 RED。

```bash
# 在 credis/ 目录执行，构建镜像 digest 必须来自已验收发布清单
 docker compose -f deploy/compose.yaml config --quiet
 docker compose -f deploy/compose.yaml config | grep -E 'postgres|redis|frontend|api|worker|scheduler'
```

- [ ] **Step 2: 实施隔离部署模板。** Compose 项目名固定 `credis`，`postgres`/`redis` 仅 private network，命名卷只给 Credis；生产服务绑定宿主机回环地址，由宿主机 HTTPS 反向代理分别路由论坛/Credis，不允许跨项目 Docker 网络。配置 `api`/`worker`/`scheduler` 同版本镜像但各自命令，前端同 release 版本；四者需 readiness/health 检查和应用/Schema/upstream SHA 发布清单。对 Secret 文件权限、持久卷、定期备份/恢复、日志脱敏、容量预警写可执行手册。
- [ ] **Step 3: 在新机器/隔离测试 VM 以测试数据执行启动→停服务→PG/Redis 重启→恢复→三桶对账→真实 Webhook；核对论坛仍独立可用。** 从 PG 和 Outbox 恢复队列，Redis 不作为数据备份来源。记录完整命令、日志摘要和用时，不用生产密钥/备份。
- [ ] **Step 4: 提交模板与测试证据。**

```bash
git -C credis add deploy config.example.yaml
git -C credis commit -m 'ops: add isolated same-host deployment template' -m 'Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>'
git add discourse-deployment/single-host-deployment.md credis
git commit -m 'docs: rehearse isolated Discourse and Credis recovery' -m 'Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>'
```

### Task 2: CI 与版本化变更门禁

**Files:** Create `credis/.github/workflows/ci.yml`, `credis/docs/release-gates.md`; modify `credis/internal/db/migrator/migrator_test.go`.

**Interfaces:** 每份 release manifest 包含上游 SHA、Credis SHA、应用镜像摘要、Discourse tag/digest、Schema 版本、回归/Secret/漏洞/许可证结果；失败任一检查不发布。

- [ ] **Step 1: 在隔离分支故意引入失败迁移/错误 flag 测试证明 CI 失败，随后回滚该测试改动。** 检验 Go 格式/vet/test/race、前端 ESLint/TS/build/组件测试、PG/Redis/Asynq、迁移重复运行、账本不变量与 feature flag 关闭；依赖/Secret/漏洞/许可证/镜像扫描工具固定版本或 digest，不依赖远端 `latest`。

```bash
(cd credis && unformatted="$(gofmt -l internal)" && if [ -n "$unformatted" ]; then printf '%s\n' "$unformatted"; exit 1; fi)
(cd credis && go vet ./... && go test ./... -count=1)
(cd credis/frontend && pnpm install --frozen-lockfile && pnpm lint && pnpm exec tsc --noEmit && pnpm build)
```

- [ ] **Step 2: 实施 CI 服务容器/迁移测试。** GitHub Actions `postgres`、`redis` 使用具体镜像摘要，机密扫描不打印发现值；测试 `AutoMigrate` 不更改新表、明确 SQL 迁移可重复、不假设自动 down；破坏性变更固定扩展→双写/回填→切换→清理，回填须可停续、幂等、核对且不修改既有流水金额。每次从全新数据库和上一发布 Schema 两个起点测试。
- [ ] **Step 3: 刻意让某单测失败，验证 CI 阻断并修复；构建固定摘要镜像，记录 SHA/Schema/Go/pnpm/许可证和真实回归结论。** `go test ./... -count=1`、`pnpm lint`、`pnpm exec tsc --noEmit`、`pnpm build`、PG/Redis 集成和镜像构建全绿才能提交。

```bash
git -C credis add .github/workflows/ci.yml docs/release-gates.md internal/db/migrator/migrator_test.go
git -C credis commit -m 'ci: gate Credis releases on ledger and integration checks' -m 'Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>'
```

### Task 3: Discourse 升级验收与板块/标签 dry-run

**Files:** Create `discourse-deployment/{upgrade-acceptance.md,category-tag-inventory.md}`, `migration/category-tag-dry-run.md`; update `discourse-deployment/{version-lock.md,compatibility-matrix.md}`.

**Interfaces:** Inventory columns `forum_instance_id, category_id, parent_category_id, tag_id, tag_group_id, permissions, form_template, notification_policy, scoring_scope, target_id`；dry-run 输出旧→新 ID 映射、主题数、权限变化、Webhook/规则版本/积分影响、失败行和可回退决议。

- [ ] **Step 1: 写可检查的空白验收表与可复算导出命令。** 对全新本地论坛执行全量板块/子板块/标签/标签组分页清单，记录稳定 ID、权限、模板和通知/计分范围；缺页、403 或采集失败判“不完整”，不能以空结果当无板块。比较目标映射，dry-run 只做查询和影响报告，不修改实体。
- [ ] **Step 2: 在后续获授权的预发布环境，用治理审批后的生产脱敏副本做稳定/ESR 升级演练。** 固定目标版本/镜像及 PG 兼容；清理由 Discourse Core 吸收的官方插件 clone 行，检查 `upcoming changes`，回归插件、主题、站点设置、OAuth、Webhook、Gamification、备份恢复；逐项留截图/日志/版本证据。未获脱敏副本授权时只提交验收方案与模板，**本 Task 不标完成、上线门槛不通过**。
- [ ] **Step 3: 生成板块/标签 dry-run 报告、核对主题计数与计分影响，输入给规则版本冻结。** 用测试数据验证导出脚本一致性，用获授权预发布数据时仅生成受控报告，不进入日常开发；权限变更需单独批准。两份门槛材料全部 PASS 再提交。

```bash
git add discourse-deployment/upgrade-acceptance.md discourse-deployment/category-tag-inventory.md discourse-deployment/version-lock.md discourse-deployment/compatibility-matrix.md migration/category-tag-dry-run.md
git commit -m 'docs: record forum upgrade and category dry-run gates' -m 'Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>'
```

### Task 4: 拆分环境租约栅栏与单入口切换演练

**Files:** Create `credis/internal/lease/{fence.go,fence_test.go}`, `credis/internal/db/migrator/sql/0006_credis_fencing.up.sql`, `migration/credis-split-host-migration.md`, `credis/deploy/independent-host.example.yaml`.

**Interfaces:** `Acquire(ctx context.Context, role string, epoch int64) (Lease,error)`；`Lease{Role string, Epoch int64, ExpiresAt time.Time}`；每次 Worker/Scheduler 消费/写账前向**唯一当前可写主库**校验 epoch；旧主库降为只读且撤销旧 API 写权限后才提升新环境，不能让两份被复制的 lease 表分别独立发号。

- [ ] **Step 1: RED 故障演练测试。** 模拟旧/新同用一个 epoch 抢任务只能一个成功；旧租约过期但旧请求尚在途、新主库追平，旧主库必须先禁止写；代理切换前后 Webhook 只进入一个可写 API；回退前新环境有写入时直接切回必须失败。`go test ./internal/lease -v` RED。
- [ ] **Step 2: 实施数据库 epoch/租约和部署脚本。** `consumer_fences(role primary key, epoch bigint not null, owner text, expires_at timestamptz)`，事务内 `SELECT FOR UPDATE` 签发递增 epoch，持租约任务执行前/提交前校验 epoch；新环境只读/API 拒写且 Worker/Scheduler 关闭直到最终增量完成。拆分流程按 Spec §13 的 13 步逐项写：同版本新环境→旧 API 唯一入口→PG 快照+WAL/流复制→只读核对/LSN→代理固定路由旧→旧停写和旧主库降权→最终同步→旧租约撤销/新 epoch→原子代理切换→新角色启动→真实事件核对→成功门槛/回退决策→旧只读窗口。Redis 队列不迁移，任务由 PG 重建。
- [ ] **Step 3: 断网/延迟/租约失效/代理失败/新环境写后回退演练。** 最终事件游标连续、两边流水净额与 Outbox 对等；若新环境未写可回旧但须重新授予更高 epoch，若新环境已写则先停写/反向增量/核对，不能双向回切。`go test ./internal/lease ./internal/outbox ./internal/ledger -count=1` 与独立隔离 VM 测试 GREEN 后提交。

```bash
git -C credis add internal/lease internal/db/migrator/sql/0006_credis_fencing.up.sql deploy/independent-host.example.yaml
git -C credis commit -m 'ops: fence consumers during single-ingress host split' -m 'Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>'
git add migration/credis-split-host-migration.md credis
git commit -m 'docs: rehearse Credis split-host migration' -m 'Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>'
```

### Task 5: 上线证据包和未来论坛迁移清单

**Files:** Create `migration/forum-production-migration.md`; update `credis/docs/release-gates.md`, root `README.md`.

**Interfaces:** Go/no-go 清单逐项关联证据文件、时间、审核人和失败阻断；此 Task 只形成准备材料，不迁移生产论坛。

- [ ] **Step 1: 汇总证据并 RED 验收。** 缺任一项（版本锁/升级/板块 dry-run/置零及切换基线/八项论坛行为/账本并发与乱序/通知/outbox 不明状态/指标成熟度/备份恢复/拆分演练）时发布检查必须返回非零并保持计分 flag=false。
- [ ] **Step 2: 将未来论坛迁移顺序落在独立清单。** 旧论坛 DB、uploads、settings、插件、主题和环境备份 → 新服务器恢复 → 用户/主题/回复/标签/勋章/私信/上传/设置对账 → OAuth/Webhook/Gamification 复核 → 确认 Discourse 用户 ID → 复核 `forum_instance_id + discourse_user_id` → 规则生效时间 → 静默试算 → 验收后结算；生效前不追补。期初余额只经经审计 `opening` 流水；清单不含生产凭证或实际数据。
- [ ] **Step 3: 逐项执行预发布 smoke 和只读生产前检查；条件齐全才签 Go，否则写 No-Go 与阻断点。** 提交文档，不自动推送/部署、改 DNS 或访问生产备份。

```bash
git add migration/forum-production-migration.md README.md credis
git commit -m 'docs: assemble Credis P0 go-no-go evidence' -m 'Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>'
```

## Phase Gate

本计划定义的是交付和验收步骤，不授权生产操作。预发布脱敏副本未获授权、真实 Gamification 写契约不成立或任一上线证据不齐时明确判 **No-Go**；已完成的本地能力仍分别保留并测试。
