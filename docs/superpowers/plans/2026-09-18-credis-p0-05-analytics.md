# Credis P0 贡献效果监控 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 独立保存参与/贡献认定与历史，提供可复算的注册→参与→贡献→LDC→再次贡献链路，而不把 LDC 流水误当全部贡献。

**Architecture:** 在论坛原始事件之外建立 `participation_events`、`contributions`、追加式 `contribution_state_history`、用户注册批次与基线快照；聚合仅对完整且观察成熟的批次出数。API 下钻至来源事件、贡献状态、账本流水及快照，初期前端只展示表格和状态，不预设图表。

**Tech Stack:** Go 1.26、PostgreSQL/GORM、Gin、Next.js 16.1.1/React 19.2.3、TypeScript。

**Spec:** `docs/superpowers/specs/2026-09-18-credis-p0-design.md` §8.4、§11、§15.5；依赖 01 用户/事件证据、02 流水、03 论坛事件、04 数据完整性。

## Global Constraints

- 四类排他核心贡献：合格 Demo、有效解答、优质技术内容、技术/产品/Bug 反馈；同一来源/统计窗口只计一次，举报不计入核心贡献/30 天激活/再次贡献。
- 技术/产品/Bug 反馈仅从“确认有效”起纳入；受理、采纳、解决/上线均为同一贡献的状态深化。
- 未满 7/30 天观察窗口、分页不全、权限失败、待审核、任务失败一律显示“观察中/不完整”，不得置零或纳入分母。
- 初期不导入生产数据；上线前基线快照须有分子/分母/排除数、窗口、成熟度、完整性及背景事件证据。

---

## File Structure

- `credis/internal/db/migrator/sql/0005_credis_analytics.up.sql`：用户批次、参与事件、贡献、状态历史、分析事件任务/消费游标、快照运行记录、背景事件、基线与指标快照。
- `credis/internal/contribution/{classify.go,history.go,cohorts.go,metrics.go,consume.go,snapshot_job.go}`：排他类型判定、追加状态、事件消费/用户 API 采集、成熟度、快照调度与指标计算；每个文件独立测试。
- `credis/internal/apps/forum/webhook.go`、`credis/internal/forum/process.go`、`credis/internal/task/{constants.go,worker/worker.go,scheduler/scheduler.go}`：入库即建立持久分析任务、后续状态变更重投、PG 恢复式消费与定时快照；`credis/internal/apps/analytics/routers.go`：仅限管理员的聚合和来源下钻 API；`credis/frontend/app/(main)/admin/contributions/page.tsx`：P0 汇总/明细表与数据质量状态。
- `credis/docs/analytics-definitions.md`：分子、分母、窗口、排他优先级和可重算查询。

### Task 1: 稳定贡献身份、排他分类与追加历史

**Files:** Create `credis/internal/db/migrator/sql/0005_credis_analytics.up.sql`, `credis/internal/contribution/{classify.go,classify_test.go,history.go,history_test.go}`.

**Interfaces:** `Classify(source SourceFacts, threshold ThresholdVersion) (coreType string, valid bool)`；`SourceFacts{SourceID, SourceKind, FeedbackStatus string; DemoQualified, AnswerValid, QualityThresholdMet bool}`，`SourceKind` 取 `demo/answer/feedback/technical_content/reviewable`，`FeedbackStatus` 至少区分 `submitted/under_review/invalid/confirmed/adopted/resolved/reversed`；`ThresholdVersion{Likes, Bookmarks int64}`；`ApplyState(tx *gorm.DB, sourceID string, state string, occurredAt time.Time, ruleVersionID int64) error`。先按**稳定来源类型**排除 reviewable 和未确认/无效反馈，再按合格 Demo → 有效解答 → 确认有效反馈 → 优质技术内容排他分类；未知来源类型 fail closed，不因点赞阈值绕过来源资格。

- [ ] **Step 1: RED 数据测试。** 同一技术反馈确认有效后被采纳/解决仅 1 个贡献；一个来源既 Demo 又精华又达点赞阈值仅 1 个 Demo；Solved 取消后净有效解答移除但历史保留；举报成立可有 ledger earn 但核心贡献=0；未确认为有效的反馈=0，且待审核和已判无效的 Bug/技术/产品反馈即使 `QualityThresholdMet=true`、精华标签或高赞，也不得转为优质技术内容；未知来源类型同样不计。`go test ./internal/contribution -run 'TestClassify|TestApplyState' -v` RED。

```go
func TestExclusiveCoreType(t *testing.T) {
    facts := SourceFacts{SourceID:"post:42", SourceKind:"demo", DemoQualified:true, QualityThresholdMet:true}
    kind, valid := Classify(facts, ThresholdVersion{Likes:100, Bookmarks:250})
    if !valid || kind != "demo" { t.Fatalf("%s %v", kind, valid) }
}

func TestUnverifiedFeedbackCannotBecomeQualityContent(t *testing.T) {
    for _, status := range []string{"submitted", "under_review", "invalid", "reversed"} {
        facts := SourceFacts{SourceID:"bug:42", SourceKind:"feedback", FeedbackStatus:status, QualityThresholdMet:true}
        kind, valid := Classify(facts, ThresholdVersion{Likes:100, Bookmarks:250})
        if valid || kind != "" { t.Fatalf("status=%s classified as %s", status, kind) }
    }
}
```

```go
func Classify(source SourceFacts, threshold ThresholdVersion) (string, bool) {
    switch source.SourceKind {
    case "reviewable":
        return "", false
    case "feedback":
        switch source.FeedbackStatus {
        case "confirmed", "adopted", "resolved":
            return "feedback", true
        default:
            return "", false // 不能落入技术内容的点赞/收藏阈值分支
        }
    case "demo", "answer", "technical_content":
        if source.DemoQualified { return "demo", true }
        if source.AnswerValid { return "answer", true }
        if source.SourceKind == "technical_content" && source.QualityThresholdMet { return "quality_content", true }
        return "", false
    default:
        return "", false
    }
}
```

- [ ] **Step 2: 显式迁移和实现。** `contributions(stable_id,source_event_id,source_id,source_kind,forum_user_id,core_type,direction,current_state,recognition_basis,rule_version_id,recognized_at,updated_at)`；唯一 `(source_id,core_type,window_key)` 且按排他映射保证来源一个核心类型；`contribution_state_history(id,contribution_id,from_state,to_state,occurred_at,recorded_at,evidence_id)` 只 INSERT；`participation_events` 留不可由互动阈值改变的来源类型、当时有效状态、判定证据、撤销/恢复与完整性；`background_events` 记活动/发布/升级/故障区间。`ApplyState` 在同一事务更新当前状态并追加历史，重复状态/事件版本仅幂等返回。
- [ ] **Step 3: `go test ./internal/contribution -v` 与真实 Solved/反馈/举报回归 GREEN 后提交。**

```bash
git -C credis add internal/db/migrator/sql/0005_credis_analytics.up.sql internal/contribution
git -C credis commit -m 'feat: record exclusive contributions and state history' -m 'Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>'
```

### Task 2: 用户批次、成熟度、基线与可复算指标

**Files:** Create `credis/internal/contribution/{cohorts.go,cohorts_test.go,metrics.go,metrics_test.go}`, `credis/docs/analytics-definitions.md`; extend `0005` before release.

**Interfaces:** `Snapshot(ctx context.Context, window Window) (MetricSnapshot, error)`；`Window{From, Until time.Time; Timezone string}`；`MetricSnapshot{EligibleRegistrations, Participated7d, Contributed30d, Credited30d, ContributedAgain30d, Excluded int64; Maturity, Completeness string}`。未成熟/不完整时 API 不返回可误读为 0 的 rate；用 nullable rate + 明确状态。

- [ ] **Step 1: RED 7/30 天样例。** 2026-09-17 注册者在 2026-09-18 查询 7 天参与显示“观察中”且不进入分母；30 天贡献和再次贡献同理；一次分页 403 把整个来源窗口标“不完整”；审核中不计净贡献，不能被“0 分”混入排除数。`go test ./internal/contribution -run 'TestCohort|TestSnapshot' -v`。

```go
func TestSevenDayCohortStillObserving(t *testing.T) {
    registered := time.Date(2026,9,17,0,0,0,0,time.UTC)
    observed := time.Date(2026,9,18,0,0,0,0,time.UTC)
    if CohortMature(registered, observed, 7) { t.Fatal("seven-day window not complete") }
}
```

```go
func CohortMature(registered, observed time.Time, days int) bool {
    if days <= 0 { return false }
    return !observed.Before(registered.AddDate(0, 0, days))
}
```

- [ ] **Step 2: 保存用户注册/来源/TL 与角色时间快照、首次参与/贡献/LDC 时间。** `registration_cohorts` 唯一 `(forum_instance_id,discourse_user_id)`；对每个时间窗口聚合 7 天参与、30 天核心有效贡献、LDC 入账、30 天再次贡献；`metric_snapshots` 存窗口、分子、分母、排除数、规则版本、观察成熟度、权限/分页完整性及来源游标，`snapshot_runs` 保存快照批次和读取水位，快照冻结后不可覆盖；补录生成新版本而非改旧版本。指标按独立事件和贡献状态算，不从账本倒推。
- [ ] **Step 3: 对真实论坛分页中断、重复贡献、历史撤销及跨时区边界回归。** 对每项指标写 `analytics-definitions.md` 的 SQL 复算公式；`go test ./internal/contribution/...` GREEN 后提交。

```bash
git -C credis add internal/contribution internal/db/migrator/sql/0005_credis_analytics.up.sql docs/analytics-definitions.md
git -C credis commit -m 'feat: compute mature and complete contribution cohorts' -m 'Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>'
```

### Task 3: 真实事件消费、用户注册采集与快照调度接线

**Files:** Create `credis/internal/contribution/{consume.go,consume_test.go,snapshot_job.go,snapshot_job_test.go}`; modify `credis/internal/apps/forum/webhook.go`, `credis/internal/forum/process.go`, `credis/internal/task/{constants.go,worker/worker.go,scheduler/scheduler.go}`, `credis/internal/db/migrator/sql/0005_credis_analytics.up.sql`（未发布前）；如 `0005` 已发布且 `0006` 已为 fencing 保留，新增 `0007_credis_analytics_delivery.up.sql`（不得改写 0005 或占用 0006），同时更新总览合同并在空库/升级库验证连续性。

**Interfaces:** `AnalyticsJobKey(forumEventID int64, sourceRevision string) string`、`ConsumeEvent(ctx context.Context, tx *gorm.DB, forumEventID int64, sourceRevision string) error` 处理每条可信事件状态修订（奖励、无奖励、ignored、撤销/恢复）；`SyncUsers(ctx context.Context, forumInstanceID int64, cursor string) (nextCursor string, complete bool, err error)` 从 Discourse API 分页建注册批次；`RunSnapshot(ctx context.Context, window Window) error` 保存冻结快照或数据不完整状态。`analytics_event_jobs(event_id, source_revision)` 唯一，`analytics_cursors` 保存用户分页/论坛事件扫描水位但不作为唯一事实源，`snapshot_runs` 保存时间窗口、上游水位和结果状态。

- [ ] **Step 1: 写失败的接线集成测试。** 不手工插入 `participation_events`/`contributions`：本地真实论坛创建用户、主题、回复、点赞、收藏、合格 Demo、Solved 接受/取消、反馈提交/确认有效、举报成立（不发核心贡献）与勋章；事件接收和后续处理后，由 Worker 自动形成用户注册批次、参与事件、排他贡献及追加历史；重投同一事件版本、清空 Redis/崩溃恢复不得重复计数。分页 403、空页与待审核必须使受影响窗口 `incomplete`；快照 Scheduler 运行后方可通过 API 查询新版本。先在隔离 PG/Redis + 真实 Discourse 中确认 RED；PG 集成断言 `(event_id,source_revision)` 重投一条任务、后续 `v3` 改判生成第二条且用户/贡献最终净状态只算一次。

```go
func TestAnalyticsJobKeyIncludesProcessingRevision(t *testing.T) {
    if AnalyticsJobKey(42, "v2") == AnalyticsJobKey(42, "v3") {
        t.Fatal("manual redecision lost after initial processing")
    }
}
```

```go
func AnalyticsJobKey(forumEventID int64, sourceRevision string) string {
    return strconv.FormatInt(forumEventID, 10) + ":" + sourceRevision
}
```

- [ ] **Step 2: 接入持久事件消费。** 可信 Webhook 落库同事务写 `analytics_event_jobs`；03 的 `ProcessEvent` 每次状态决议/人工改判同事务追加新的 `source_revision` 任务（稳定源版本+处理结论版本），无奖励事件仍进入分析而不是从 ledger 倒推。Worker 按 PG 行租约领取 pending/retry job，读取 `forum_events` 原文和状态历史，先幂等 upsert 稳定参与事件，再 `Classify`/`ApplyState`；缺字段置 `pending_review`/不完整，不默认为零。分析结果、消费结论、游标一笔 PG 事务提交；Scheduler 按 PG pending 表/游标补扫，Redis 仅加速。用户注册独立定期调用 Discourse 分页 API，按 `(forum_instance_id,discourse_user_id)` upsert，记录注册时间/来源/TL/角色事件快照与完整性。

```sql
CREATE TABLE analytics_event_jobs (
 event_id bigint NOT NULL REFERENCES forum_events(id),
 source_revision text NOT NULL,
 status text NOT NULL DEFAULT 'pending',
 attempts integer NOT NULL DEFAULT 0,
 lease_until timestamptz,
 PRIMARY KEY (event_id, source_revision)
);
CREATE TABLE analytics_cursors (
 source_key text PRIMARY KEY, cursor_value text NOT NULL, updated_at timestamptz NOT NULL DEFAULT now()
);
```

- [ ] **Step 3: 注册定时快照与完整性门禁。** `task.SnapshotContributionMetricsTask` 进 `maintenance` 队列；Scheduler 按北京时间窗口调度 `RunSnapshot`，将 DB 事件游标与用户 API 分页完整性冻结进 `snapshot_runs`。未成熟/权限失败/分页不完整时保存“观察中/不完整”而非数值 0；迟到事件补录生成新快照版本。重复调度和 Redis 丢失从 PG pending run 恢复，不能覆盖已冻结版本。
- [ ] **Step 4: 验证 GREEN 并提交。** `go test ./internal/contribution ./internal/apps/forum ./internal/forum ./internal/task/... -count=1` 加 PG/Redis 故障注入；真实论坛逐条触发后在管理 API 和数据库查看参与、反馈待审核/无效高赞仍排除、确认有效后纳入、Solved 撤销、举报排除与新快照，**不允许直接向分析表手工插数**。记录未完成观察窗口和数据源中断证据后提交。

```bash
git -C credis add internal/contribution internal/apps/forum/webhook.go internal/forum/process.go internal/task internal/db/migrator/sql/0005_credis_analytics.up.sql
git -C credis commit -m 'feat: consume forum analytics events and schedule snapshots' -m 'Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>'
```

### Task 4: 管理员下钻与最小效果页面

**Files:** Create `credis/internal/apps/analytics/{routers.go,routers_test.go}`, `credis/frontend/app/(main)/admin/contributions/page.tsx`, `credis/frontend/components/common/admin/contributions.tsx`; modify `credis/internal/router/router.go`, `credis/frontend/components/layout/sidebar.tsx`.

**Interfaces:** `GET /api/v1/admin/contributions/snapshots?from=...&until=...` 返回窗口/状态与可为空的指标；`GET /api/v1/admin/contributions/:stable_id/trace` 返回原始事件 ID、完整性、排他认定、状态历史、ledger IDs 和快照版本（敏感 payload 不返回）。均需后台管理员身份而非论坛信任等级。

- [ ] **Step 1: RED API/组件测试。** 非管理员 403，已授权者能追溯到来源与状态历史，`incomplete` 快照不渲染 0% 或除以零；举报可见“治理参与”但核心统计 excluded；空库显示“尚无完整数据”，不显示零成果。
- [ ] **Step 2: 实施带分页的汇总和追踪接口及只读表格。** 返回稳定 ID、时间窗口、分子/分母/排除、完整性、版本、来源游标；页面以“观察中/数据不完整/已完成”文字区分状态，支持按来源 ID 跳转明细。不给管理端直接改分/改贡献接口。
- [ ] **Step 3: 执行 `go test ./internal/apps/analytics ./internal/contribution/...`、`pnpm lint && pnpm build`，再用真实论坛种子数据逐行对账；GREEN 后提交。**

```bash
git -C credis add internal/apps/analytics internal/router/router.go frontend/app frontend/components docs/analytics-definitions.md
git -C credis commit -m 'feat: expose auditable contribution cohorts' -m 'Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>'
```

## Phase Gate

上线前冻结并人工签核基线快照；任何指标下钻必须能经来源事件、状态历史、账本、快照重算。缺失外部数据时保留不完整状态而不是填零。
