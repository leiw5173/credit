# Credis P0 基线与论坛能力 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 在独立 Fork 和全新本地 Discourse 中建立可复现、默认安全的 Credis 基线，并以真实行为验证 P0 事件能力。

**Architecture:** 保持 `linux-do/credit` 的 Go/Gin/GORM、Next.js 和 API/Worker/Scheduler 结构。文档仓库通过 submodule 引用独立 Fork；Discourse 与 Credis 仅经 HTTP(S) 交互，插件是否需要由能力矩阵决定。

**Tech Stack:** Go 1.26、Gin、GORM、PostgreSQL、Redis、Asynq、Next.js 16.1.1、React 19.2.3、Discourse 稳定版/ESR、Docker。

**Spec:** `docs/superpowers/specs/2026-09-18-credis-p0-design.md`（相对于 `开发文件/`；§1–4、§10–12、§15–17）。

## Global Constraints

- 上游固定审阅基线：`linux-do/credit` 默认分支 `master`，2026-09-18 读取 SHA `2d09c890e06e7c12906f1b641e8248646816590a`；**执行前重新核对**，发生漂移时先重新评估差异，不暗中追新版。
- 上游 `LICENSE` 是 Apache-2.0；`go.mod` 是 Go 1.26；前端 Next.js 16.1.1、React 19.2.3；仍需在 Fork 创建时记录实际版本和完整基线结果。
- `credis.example.com` 和 `forum.example.com` 仅为示例域名；所有实际地址由配置提供。
- 不使用生产论坛数据，不自动追补历史，不开发 Discourse Core；未经能力验证不建插件。
- 支付、转账、商户、红包、争议默认关闭，前端/路由/Worker/Scheduler 一起关闭；源码保留。
- 源码尚未接入工作区。所有下面的 `credis/...` 路径依据上述上游 SHA 核实；若 Fork 基线不同，先更新计划中的实际路径和接口再执行。
- 外向的创建 Fork、部署、发送通知及访问真实论坛须在执行时得到明确授权；本计划只定义操作，不执行它们。

---

## File Structure

- 父仓库 `.gitmodules` 和 `credis/`：独立 Fork 引用；`README.md`：运行边界和阶段索引。
- `credis/docs/baseline.md`：固定 SHA/许可证/工具链/测试证据；`credis/docs/upstream-sync.md`：上游同步和差异评审。
- `credis/internal/config/model.go`、`credis/config.example.yaml`：显式 P0 功能门控；`credis/internal/router/router.go`、`credis/internal/task/{worker/worker.go,scheduler/scheduler.go}`：服务端关闭门控。
- `credis/frontend/components/layout/sidebar.tsx`、`credis/frontend/app/(main)/{trade,merchant}/page.tsx`、`credis/frontend/proxy.ts`：入口与直达页面保护；前端公开配置只传非敏感开关。
- `discourse-deployment/{version-lock.md,compatibility-matrix.md,local-development.md}`：镜像版本、兼容矩阵和全新本地启动步骤。
- `credis/docs/forum-capability-matrix.md`：真实事件与撤销能力的逐项证据；`discourse-plugin/` **只在矩阵表明必要时**才创建。

### Task 1: 固定 Fork 与可执行上游基线

**Files:** Create `credis/docs/baseline.md`, `credis/docs/upstream-sync.md`, root `README.md`; modify `.gitmodules` through `git submodule add`.

**Interfaces:** Produces `credis/` with `origin` 指向独立 Fork、`upstream` 指向 `https://github.com/linux-do/credit`；后续计划以它为源码根。

- [ ] **Step 1: 在执行授权后建立独立 Fork 和 submodule。** 在父仓库功能分支执行；若个人账号已占用 `credis` 名称，停止并由仓库负责人确定新仓库，不误用其他远端。首次接入才运行 `git submodule add`；本工作区已经登记 `.gitmodules` 和 gitlink，恢复执行时只核验 URL 和固定提交并执行 `git submodule update --init --recursive`，不可重复 add。

```bash
# 首次接入：由获授权负责人在 GitHub Fork linux-do/credit，记录独立 Fork URL；不依赖 gh
: "${CREDIS_FORK_URL:?set the authorized Fork HTTPS URL}"
# 首次接入使用 git submodule add "$CREDIS_FORK_URL" credis；已登记时仅执行下列检查
git submodule update --init --recursive
test "$(git config -f .gitmodules submodule.credis.url)" = "$CREDIS_FORK_URL"
git -C credis remote get-url origin
# upstream remote 的新增须获本地环境权限；若被拒绝，记为未完成门槛而不绕过
git -C credis remote -v
git -C credis rev-parse HEAD
```

- [ ] **Step 2: 写基线文档并运行原样测试，保留真实失败日志。** 不把待改造的失败记录成 PASS；记录 `git branch -r`、`sha256sum LICENSE`、`go version`、`node --version`、`pnpm --version`、`go test ./...`、`pnpm install --frozen-lockfile && pnpm lint && pnpm build`；API/Worker/Scheduler 要在隔离 PostgreSQL/Redis 上各自启动并检查健康与任务日志。测试数据只能来自全新本地环境。

```bash
git -C credis ls-remote upstream HEAD refs/heads/main refs/heads/master
git -C credis rev-parse HEAD
sha256sum credis/LICENSE
git -C credis status --short
(cd credis && go test ./...)
(cd credis/frontend && pnpm install --frozen-lockfile && pnpm lint && pnpm build)
```

- [ ] **Step 3: 写同步规程并检验约束。** `credis/docs/upstream-sync.md` 必须包含：从 upstream 拉到隔离同步分支 → `git diff` 评审 Schema/余额写入/安全变更 → 全量测试与真实论坛回归 → 人工合并；明确不得自动部署 upstream 默认分支。`README.md` 列明实施计划、目录职责、独立仓库与父仓库提交顺序。
- [ ] **Step 4: 验证 submodule 可复现并分别提交。** `git submodule update --init --recursive` 后父仓库 `git diff --submodule=log` 可显示固定 SHA；先在 Fork 分支提交文档，再在父仓库提交 `.gitmodules`、gitlink 和 README；两个仓库均用规定署名，不推送或发布。

```bash
git -C credis add docs/baseline.md docs/upstream-sync.md
git -C credis commit -m 'docs: capture Credis upstream baseline' -m 'Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>'
git add .gitmodules credis README.md
git commit -m 'docs: register Credis fork submodule' -m 'Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>'
```

### Task 2: 默认关闭非 P0 路径并阻断旧榜单反向写余额

**Files:** Modify `credis/internal/config/model.go`, `credis/config.example.yaml`, `credis/internal/router/router.go`, `credis/internal/task/worker/worker.go`, `credis/internal/task/scheduler/scheduler.go`, `credis/internal/apps/admin/task/routers.go`, `credis/frontend/components/layout/sidebar.tsx`, `credis/frontend/app/(main)/{trade,merchant}/page.tsx`, `credis/frontend/app/(pay)/paying/page.tsx`, `credis/frontend/app/(redenvelope)/redenvelope/[id]/page.tsx`, `credis/frontend/proxy.ts`; create `credis/internal/router/feature_flags_test.go` and `credis/frontend/lib/feature-flags.ts`.

**Interfaces:** Produces `Features.P0 bool`, `Features.LegacyCommerce bool`, `Features.LegacyGamificationImport bool` (all default false); latter must remain false once Credis ledger scoring starts. Export frontend `legacyCommerceEnabled(): boolean` from server-side runtime config; direct routes are forbidden when false.

- [ ] **Step 1: 写默认关闭测试，验证完整入口而非只看导航。** 将 `registerRoutes(r *gin.Engine, flags config.Features)` 从当前 `router.Serve` 的路由注册段抽出供 `httptest` 测试；现有 Redis/session 初始化改为可替换测试依赖。测试直接 `POST /pay/submit.php`、`POST /api/v1/payment/transfer`、`POST /api/v1/redenvelope/create`、`POST /api/v1/merchant/api-keys`，断言 404；相应 Worker mux 和 Scheduler 注册任务清单不得含支付/红包/争议/旧 Gamification 任务。先运行 `go test ./internal/router ./internal/task/...` 确认 RED。

```go
func TestCommerceRoutesAbsentByDefault(t *testing.T) {
    r := gin.New()
    registerRoutes(r, config.Features{})
    for _, path := range []string{"/pay/submit.php", "/api/v1/payment/transfer", "/api/v1/redenvelope/create", "/api/v1/merchant/api-keys"} {
        w := httptest.NewRecorder()
        r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, path, nil))
        if w.Code != http.StatusNotFound { t.Fatalf("%s: %d", path, w.Code) }
    }
}
```

- [ ] **Step 2: 实施后端门控。** 新增配置并在注册处包裹整个相关 route group；`internal/apps/user/tasks.go` 的 `batchUpdateUserScores` 目前把论坛榜单分数写入 `community_balance`、`total_community`、`available_balance`，旧 Scheduler/Worker/管理下发必须在 flag=false 时全部禁用；如果人为开启 `LegacyGamificationImport`，须显式拒绝 `P0=true` 的组合。不可仅关闭前端按钮。`Upload` 和登录等共享能力不得因交易门控一并禁用。

```go
type Features struct {
    P0                       bool `mapstructure:"p0"`
    LegacyCommerce           bool `mapstructure:"legacy_commerce"`
    LegacyGamificationImport bool `mapstructure:"legacy_gamification_import"`
}
func (f Features) Validate() error {
    if f.P0 && f.LegacyGamificationImport { return errors.New("P0 and legacy gamification import are mutually exclusive") }
    return nil
}
```

- [ ] **Step 3: 封闭前端入口、服务端直达页面与文档示例。** 在 sidebar 隐藏交易/商户，`(main)/trade`、`(main)/merchant`、pay/red-envelope 页面直达时调用 `notFound()`；`frontend/lib/feature-flags.ts` 仅读取服务端 `process.env` 且缺省 false；公开前端开关不得承载 Secret。跑 `go test ./internal/router ./internal/task/...` 和 `pnpm lint && pnpm build`，期望 PASS；测试开启 legacy flag 后路由集合恢复，P0 与旧导入并开时启动失败。
- [ ] **Step 4: 在 Fork 分支提交。**

```bash
git -C credis add internal/config/model.go config.example.yaml internal/router internal/task internal/apps/admin/task/routers.go frontend
git -C credis commit -m 'feat: gate legacy commerce and inbound score sync' -m 'Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>'
```

### Task 3: 建立全新 Discourse 环境并核实 P0 事件

**Files:** Create `discourse-deployment/version-lock.md`, `discourse-deployment/compatibility-matrix.md`, `discourse-deployment/local-development.md`, `credis/docs/forum-capability-matrix.md`; conditional `discourse-plugin/plugin.rb` and `discourse-plugin/spec/integration/event_contract_spec.rb` only for confirmed gaps.

**Interfaces:** Produces每种事件的 `source_event_id`、`source_version`、`occurred_at`、关系键字段与撤销能力的证据；供第 03 计划实现适配器。未证实可靠版本/操作终态的事件不得自动计分/投影。

- [ ] **Step 1: 锁版本并定义真实测试矩阵。** 查 Discourse 当时最新受支持稳定/ESR，记确切 tag、git SHA、镜像 digest、Ruby/PostgreSQL/插件兼容性和下载证据；使用官方 Docker、独立 Compose 项目和卷，以新建用户/主题生成数据。`version-lock.md` 同时记录升级/回退条件；绝不使用浮动 `latest`。写完整矩阵行：主题/回复创建删除恢复、点赞撤赞/Reactions、收藏取消、Solved 接受取消、精华标签增删、举报成立改判撤销、勋章授予撤销；每行包含前后 Payload、Webhook/REST 来源、稳定 ID、源版本、权限、分页、取消与恢复证据。
- [ ] **Step 2: 对矩阵逐项做 RED 真实测试。** 通过 Discourse 官方管理员测试账号创建动作，并在 Credis 测试接收器保存脱敏原始 HTTP 请求及时间；无法直接触发的动作记 `unverified`，不得猜测为支持。使用 REST 查询 API 对每个动作复核当前状态；模拟重复、乱序、迟到、权限 403、空页和删除恢复。把可重放的测试脚本写入 `discourse-deployment/local-development.md`，对每行设 PASS/FAIL 与具体证据文件。

```bash
# 以下命令只作用于全新本地测试环境，不包含生产备份
docker compose -p credis-forum-test ps
curl -fsS "${FORUM_BASE_URL}/site.json" >/dev/null
```

- [ ] **Step 3: 若且仅若矩阵确有缺口，补最小插件及 contract spec。** 插件仅提供缺失事件与可靠稳定 ID/版本/时间，不修改 Discourse Core；测试先在真实 Discourse 环境观察缺失事件为 FAIL，再添加事件发布和 `event_contract_spec.rb`，运行 `bundle exec rspec plugins/credis-event-bridge/spec/integration/event_contract_spec.rb` 为 PASS。没有缺口时此步骤不产生插件，矩阵记录 “none needed”。
- [ ] **Step 4: 再跑全部实际事件和禁用插件的对照测试，提交文档和条件性插件。** 验收不能用 mock 替代；矩阵需将不具备可靠 ID/版本的动作标为 `pending_review`，并将仅有当前分值查询而无写入终态的 Gamification API 标为“严格投影阻塞”。父仓库和 Fork 仓库分别提交各自文件。

```bash
git add discourse-deployment credis
# 仅当真实能力矩阵确认需要并已创建插件时，额外执行 git add discourse-plugin
git commit -m 'test: record local Discourse event capabilities' -m 'Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>'
```

## Phase Gate

三角色启动、上游回归、默认关闭测试、真实论坛事件矩阵全部存证后进入第 02/03 计划。旧 Gamification 导入未关闭、事件来源语义不清或版本未锁定时禁止启用 P0 记账。
