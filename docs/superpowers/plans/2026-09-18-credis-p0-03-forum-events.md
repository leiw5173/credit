# Credis P0 论坛事件与规则 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 从真实 Discourse 接收可信行为、按发生时生效规则计分，并在重复、乱序、撤销、恢复和额度竞争下收敛到相同净结果。

**Architecture:** 入口只做签名验证/落库，Worker 从 PostgreSQL 原始事件领取处理。版本化 `behavior_relations`、确定性 `quota_claims` 与 `quota_usages` 在数据库锁内重算；通过第 02 计划的 `ledger.Post` 写入净差额与 Outbox。

**Tech Stack:** Go 1.26、Gin、GORM、PostgreSQL、Asynq、真实 Discourse REST/Webhook。

**Spec:** `docs/superpowers/specs/2026-09-18-credis-p0-design.md` §4.2、§5.2、§5.5–5.8、§7、§9–11；依赖 01 的真实事件矩阵和 02 的 `ledger.Post`。

## Global Constraints

- 唯一账户关联：`forum_instance_id + discourse_user_id`；用户名/邮箱/头像仅快照；不能默认同 ID 的两个论坛用户是同一账户。
- `source_event_id` 缺失时键为 `forum_instance_id + event_type + actor_user_id + target_type + target_id + action + source_version`；没有可靠来源版本时只能 `pending_review`，禁止 Payload 哈希/接收时间去重发分。
- 规则按 `occurred_at` 与 `Asia/Shanghai` 版本匹配；同日新版本不重置日额度，对象额度跨日期和版本不重置；额度策略 ID 仅经独立迁移决议更换。
- 额度及账本更新在**同一 PostgreSQL 事务**；外部 HTTP 不在事务中。`actual_award=0` 保存结果不写零流水。
- 无可靠状态序列/当前状态时不自动计分；真实 Discourse 测试不能以模拟 API 替代。

---

## File Structure

- `credis/internal/db/migrator/sql/0003_credis_forum.up.sql`：论坛事件、用户映射、规则、行为关系、额度、结算及处理结果表（已发布迁移不修改，续号追加）。
- `credis/internal/model/forum.go`：新表 GORM 映射；`credis/internal/apps/forum/{webhook.go,verify.go,webhook_test.go}`：接收与签名安全；`credis/internal/router/router.go`：注册 `POST /webhooks/discourse`。
- `credis/internal/forum/{normalize.go,relation.go,quota.go,rules.go,process.go}`：归一化、关系收敛、额度分配、规则版本及事务编排；各对应 `_test.go`。
- `credis/internal/task/{constants.go,worker/worker.go,scheduler/scheduler.go}`：PG 恢复型队列投递；`credis/docs/forum-events.md`：事件矩阵字段/版本和人工核查操作。

### Task 1: 安全接收、用户映射、三层幂等

**Files:** Create `credis/internal/db/migrator/sql/0003_credis_forum.up.sql`, `credis/internal/model/forum.go`, `credis/internal/apps/forum/{webhook.go,verify.go,webhook_test.go}`, `credis/internal/forum/{normalize.go,normalize_test.go}`; modify `credis/internal/router/router.go`.

**Interfaces:** `forum.Verify(secret []byte, body []byte, signature string) bool`；`forum.Normalize(instanceID int64, raw []byte, headers http.Header) (Event, error)`；`Event{InstanceID int64, SourceEventID, EventType string, ActorUserID int64, TargetType string, TargetID int64, Action, SourceVersion, StableEventKey string, OccurredAt time.Time, Payload json.RawMessage}`。响应：坏签名 401；过期/超大请求 400/413；可信重复 2xx；缺乏语义的可信事件入 `pending_review` 而非发分。

- [ ] **Step 1: RED 测试真实矩阵 Payload。** 从 01 中保存的脱敏请求逐项变成 fixture；测试正确签名 202、篡改签名 401 且不落库、Body >1 MiB 413、时间偏差 >5 分钟 400、重复 event ID 200 但只有一条 `forum_events`；同用户同帖“新增→取消→再次新增”三次不得合并；不含可靠版本/ID 则 `pending_review`。运行 `go test ./internal/apps/forum -v` 确认失败。

```go
func TestVerifyRejectsTamperedBody(t *testing.T) {
    key := []byte("local-test-secret")
    body := []byte(`{"post_id":42}`)
    sum := hmac.New(sha256.New, key)
    sum.Write(body)
    signature := "sha256=" + hex.EncodeToString(sum.Sum(nil))
    if !Verify(key, body, signature) { t.Fatal("valid signature rejected") }
    if Verify(key, []byte(`{"post_id":43}`), signature) { t.Fatal("tampered body accepted") }
}
```

- [ ] **Step 2: 迁移和接收实现。** `forum_instances(id,base_url,secret_ref)`；`forum_users(id,forum_instance_id,discourse_user_id,ledger_account_id,user_id,username_snapshot,created_at)` 唯一 `(forum_instance_id,discourse_user_id)`；`forum_events(id,instance_id,source_event_id,stable_event_key,event_type,actor_user_id,target_type,target_id,action,source_version,occurred_at,received_at,payload,status,reason,attempts)`，可信源 ID / 规范键分别做**部分唯一索引**；新增 `external_sync_records` 和 `event_processing_results` 唯一 `(event_id,rule_version_id,account_id,action)`。签名协议依据 01 实测 header 选取，严格恒时比较、实例专属 Secret、校验来源实例、限长限时；原文只写受限 DB，日志不含 Body/Secret。

```sql
CREATE UNIQUE INDEX uq_forum_event_source ON forum_events(instance_id, source_event_id)
 WHERE source_event_id IS NOT NULL;
CREATE UNIQUE INDEX uq_forum_event_stable ON forum_events(instance_id, stable_event_key)
 WHERE stable_event_key IS NOT NULL;
CREATE UNIQUE INDEX uq_forum_user_identity ON forum_users(forum_instance_id, discourse_user_id);
```

- [ ] **Step 3: 对接真实 Discourse Webhook，验证 RED→GREEN。** `go test ./internal/apps/forum -v` + 本地真实主题/回复/点赞/取消等逐行重放，Bad signature 无 DB 记录；保存 `received`/`pending_review`/`ignored` 结论及原因，超时/崩溃可从 DB 重领。提交。

```bash
git -C credis add internal/db/migrator internal/model/forum.go internal/apps/forum internal/forum/normalize.go internal/forum/normalize_test.go internal/router/router.go
git -C credis commit -m 'feat: ingest verified forum events idempotently' -m 'Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>'
```

### Task 2: 规则版本、资格判定与 P0 八种奖励

**Files:** Create `credis/internal/forum/{rules.go,rules_test.go}`; extend `credis/internal/db/migrator/sql/0003_credis_forum.up.sql` before first release; create `credis/docs/forum-events.md`.

**Interfaces:** `RuleAt(tx *gorm.DB, occurredAt time.Time, eventType string) (Rule, error)`；`AwardFor(rule Rule, event Event, state RelationState) (int64, string)`；`Rule{VersionID int64, PolicyID int64, Amount int64, Cap int64, EffectiveFrom, EffectiveUntil time.Time, ReleaseOnReversal bool}`。返回 `(expectedAward, reason)`；无适用版本为 ignored，不回退当前版本。

- [ ] **Step 1: 写规则表 RED 测试。** 普通首帖 5、回复 1，共用每日额度；合格 Demo 首帖 50，不叠加基础且不占基础限；有效点赞 1（每帖 100）和收藏 5（每帖 250）；Solved 10 给答案作者且自答 0；合格 Demo 精华 100、普通精华 0；举报管理员确认成立 10 且同一问题去重；勋章只采集不奖励。删除/撤销/恢复按当前净应得返回差额；规则发布日期前的旧事件仍匹配旧版本。先跑 `go test ./internal/forum -run 'TestAward|TestRuleAt' -v` 为 RED。

```go
func TestSolvedSelfAnswerDoesNotReward(t *testing.T) {
    rule := Rule{Amount:10}
    event := Event{EventType:"solved", ActorUserID:11, TargetID:22}
    state := RelationState{QuestionAuthorID:11, AnswerAuthorID:11, Active:true}
    points, reason := AwardFor(rule, event, state)
    if points != 0 || reason != "self_answer" { t.Fatalf("%d %s", points, reason) }
}
```

- [ ] **Step 2: 迁移规则和审计。** `rule_versions(id,effective_from,effective_until,timezone,status,published_by,published_at)`；`quota_policies(id,stable_key,scope,lifecycle,created_at)` 与 `rule_definitions(id,rule_version_id,event_type,quota_policy_id,amount,cap,release_on_reversal,eligibility_json)`；同一规则不允许重叠生效区间，状态发布后不可原地修改；额度稳定策略身份跨版本。明确 Reactions 的可奖励集合版本、Demo 板块 ID/Form 字段、精华标签 ID、举报 Reviewable 结论、反馈受理状态，均取本地真实论坛能力矩阵，不用字符串猜测。
- [ ] **Step 3: 最小实现和测试通过。** 对历史 `occurred_at` 选择规则版本，用稳定论坛 ID 判资格；有效对象状态缺少证据时 `pending_review`；保存 `expected_award`、`actual_award`、`capped_amount`、`cap_reason` 于 `event_processing_results`，而非只写总分。执行 Go 测试及真实 Discourse 八种行为验证；提交。

```bash
git -C credis add internal/forum internal/db/migrator docs/forum-events.md
git -C credis commit -m 'feat: evaluate versioned forum reward rules' -m 'Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>'
```

### Task 3: 版本化关系、确定性额度 Claim、乱序收敛

**Files:** Create `credis/internal/forum/{relation.go,relation_test.go,quota.go,quota_test.go}`; extend migration `0003` before release; modify `credis/internal/forum/process.go` in Task 4.

**Interfaces:** `type RelationState struct { Key string; SourceVersion string; Active bool; QuestionAuthorID, AnswerAuthorID int64 }`；`RelationKey(Event) string`；`Allocate(claims []Claim) []Allocation`，`Claim{Key string; OccurredAt time.Time; StableEventKey string; Expected, EligibleDemand, CapAtEvent, RuleVersionID int64; RelationActive, HoldsQuota, ReleaseOnReversal bool}`；`Allocation{Key string; Actual int64}`。`HoldsQuota` 与当前关系活跃分开：不释放策略取消后仍占位；`Expected` 是首次激活原始应得，`EligibleDemand` 是释放策略下当前仍有效的额度请求（不释放时始终为 `Expected`，释放时可减至零）；每个 Claim 在首次激活时固定发生时上限/规则版本，按 `OccurredAt,StableEventKey` 排序，不以到达时间排序。

- [ ] **Step 1: RED 顺序/并发测试。** 同一新增/取消/恢复的全排列，和另一个竞争同帖上限的赞/收藏组合，最终关系、净流水、日/对象额度一致；先撤销后新增保留 inactive 和历史 claim 而不补发当前分；`release_on_reversal=false` 在满 100 赞取消恢复复用原 claim、占位仍 100；`true` 恢复新周期重新排队。跨天/新规则版本不重置对象额度，当天新版本不重置日额度；`expected=5`、日额剩 1 时实际 1/封顶 4；并发两个来源抢最后 1 最多入账 1。

```go
func TestAllocateDoesNotExceedCap(t *testing.T) {
    claims := []Claim{
        {Key:"b", OccurredAt:time.Unix(2,0), StableEventKey:"b", Expected:1, EligibleDemand:1, CapAtEvent:1, HoldsQuota:true},
        {Key:"a", OccurredAt:time.Unix(1,0), StableEventKey:"a", Expected:1, EligibleDemand:1, CapAtEvent:1, HoldsQuota:true},
    }
    got := Allocate(claims)
    if got[0].Key != "a" || got[0].Actual != 1 || got[1].Actual != 0 { t.Fatalf("%+v", got) }
}

func TestLowerCapKeepsHistoricalGrantAndAllowsRelease(t *testing.T) {
    old := Claim{Key:"old", OccurredAt:time.Unix(1,0), StableEventKey:"old", Expected:80, EligibleDemand:80, CapAtEvent:100, HoldsQuota:true, ReleaseOnReversal:true}
    fresh := Claim{Key:"new", OccurredAt:time.Unix(2,0), StableEventKey:"new", Expected:1, EligibleDemand:1, CapAtEvent:50, HoldsQuota:true}
    got := Allocate([]Claim{fresh, old})
    if got[0].Actual != 80 || got[1].Actual != 0 { t.Fatalf("old grant changed or new grant exceeded cap: %+v", got) }
    old.EligibleDemand = 79 // 同一原 Claim 的部分释放；Expected 仍是 80
    got = Allocate([]Claim{fresh, old})
    if got[0].Actual != 79 || got[1].Actual != 0 { t.Fatalf("release rejected: %+v", got) }
    old.EligibleDemand = 49
    got = Allocate([]Claim{fresh, old})
    if got[0].Actual != 49 || got[1].Actual != 1 { t.Fatalf("new cap did not open at 49: %+v", got) }
}
```

- [ ] **Step 2: 实施关系与额度模型。** `behavior_relations` 关系唯一键、源版本、active/inactive/unknown、最后事件；`relation_state_history` 追加状态链；`quota_claims` 固定政策/作用域/关系/周期/首次激活/排序键、`rule_version_id`、`cap_at_event`、`expected_award`、`eligible_demand`、获配额、是否占位；`quota_usages` 唯一 `(quota_policy_id,scope_key)`，保存 `used` 与 `revision`。scope 分别为 `policy:account:Asia/Shanghai-date`、`policy:post:like`、`policy:post:bookmark`；规范化格式需避免字符串拼接歧义。创建用唯一 upsert，按 scope 字典序锁额度行，再锁账户；**不能**拿当前规则 cap 重算旧 Claim 或用 `used + delta BETWEEN 0 AND current_cap` 拒绝合法降额释放。关系锁→额度锁→账户锁保持统一顺序，重试 PG 死锁事务。
```go
// quota.go：从按 occurred_at 生效的已发布规则填入每项 CapAtEvent；
// 对迟到事件重放整个作用域时也保留该项原版本，不用当前 cap 覆写。
func Allocate(claims []Claim) []Allocation {
    ordered := append([]Claim(nil), claims...)
    sort.Slice(ordered, func(i, j int) bool {
        if ordered[i].OccurredAt.Equal(ordered[j].OccurredAt) {
            return ordered[i].StableEventKey < ordered[j].StableEventKey
        }
        return ordered[i].OccurredAt.Before(ordered[j].OccurredAt)
    })
    used := int64(0)
    out := make([]Allocation, 0, len(ordered))
    for _, claim := range ordered {
        actual := int64(0)
        if claim.HoldsQuota {
            actual = min(claim.EligibleDemand, max(claim.CapAtEvent-used, int64(0)))
            used += actual
        }
        out = append(out, Allocation{Key: claim.Key, Actual: actual})
    }
    return out
}
```

- [ ] **Step 3: 重算而非覆写历史。** 迟到较低版本若补全合法激活，按稳定顺序重算受影响作用域，为其他 claim 产生补发/冲正流水；策略不释放时取消仍保留历史占位，策略释放时 inactive 不占有效额度；缺时间或顺序则 pending_review。锁内先验证每个 Claim 的 `0 <= EligibleDemand <= Expected`，且 `release_on_reversal=false` 时未获批准的规则迁移不得改变 `EligibleDemand`，再通过 `Allocate` 逐项验证历史规则后，用 `UPDATE quota_usages SET used = :recomputed_used, revision = revision + 1 WHERE scope_key = :scope AND used = :old_used AND revision = :old_revision AND :recomputed_used >= 0` 提交总量；不再强制总量不超过**当前**上限。只调整净额，旧流水不改；测试 100 个有效点赞竞争、跨版本上限升/降；例如 100→50 时旧已发 80 保持 80、新 Claim 得 0、合法释放 1 后 used=79、继续不发新分，直到降至 49 才可按新上限发 1；50→100 时仅后续事件获得差额空间；`go test -race ./internal/forum/...` + PostgreSQL 并发。提交。

```bash
git -C credis add internal/forum internal/db/migrator
git -C credis commit -m 'feat: converge behavior and quota claims under lock' -m 'Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>'
```

### Task 4: 事务编排、任务重建和人工核查

**Files:** Create `credis/internal/forum/{process.go,process_test.go}`; modify `credis/internal/task/{constants.go,worker/worker.go,scheduler/scheduler.go}`; create `credis/internal/apps/admin/forum/routers.go`.

**Interfaces:** `ProcessEvent(ctx context.Context, tx *gorm.DB, eventID int64) error` 持有事件行/关系/额度/账户锁并调用 `ledger.Post`；PG 游标 `forum_event_id` 是队列恢复源；管理员重放接口须验证独立 admin 权限、审批理由和审计 ID。

- [ ] **Step 1: RED 集成测试。** 可信事件最终必须二选一：有效流水或带原因的 ignored/pending_review；重复调用不重复入账；失败后 `failed_retryable`、重试耗尽 `failed_terminal`；低版本事件补历史不改当前关系；人工改判/重放留操作者、原因、时间和链路。清空 Redis 后从 `forum_events status IN ('received','failed_retryable','processing')` 恢复，不重复记账。
- [ ] **Step 2: 事务处理与状态机。** 先持有事件行租约，锁关系与额度，按 `occurred_at` 选规则，调用 `Allocate` 算差额，调用 `ledger.Post` 更新三桶/兼容字段/Outbox，写 `event_processing_results` 与状态。严禁在事务内调用 Discourse；需要权威 API 时先外部核查并附证据，再开启事务提交。管理员接口不提供直接改余额，手工 stable key 决议需双人复核并记录审计。
- [ ] **Step 3: 完整 GREEN 与真实论坛回归。** `go test ./internal/forum ./internal/apps/forum ./internal/ledger -count=1`、独立 PG/Redis 集成、真实论坛重复/乱序/迟到/删除恢复/举报改判；核验无资格和 0 分事件保留结论。提交。

```bash
git -C credis add internal/forum internal/task internal/apps/admin/forum
git -C credis commit -m 'feat: process forum events with recoverable transaction state' -m 'Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>'
```

## Phase Gate

对同一事件集合不同到达顺序比较净流水、三桶与额度；只要存在未知事件源版本、未签名来源、未经核验的插件输出或额度超发，保持对应规则关闭。
