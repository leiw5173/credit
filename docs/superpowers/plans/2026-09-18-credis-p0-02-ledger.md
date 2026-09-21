# Credis P0 不可变账本 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 以不可变整数流水为唯一账务事实，事务内维护三桶投影/兼容余额，并对旧余额入口设置启用门槛。

**Architecture:** 在现有 GORM/PostgreSQL 上新增版本化 SQL 迁移和 `internal/ledger` 服务。流水、投影、兼容 `users` 字段、Outbox 及审计在一笔数据库事务提交；对冲正与结算依业务链计算当前桶位而非反转第一笔流水。

**Tech Stack:** Go 1.26、GORM、PostgreSQL、`shopspring/decimal`（仅用于旧字段兼容）、Go `testing`。

**Spec:** `docs/superpowers/specs/2026-09-18-credis-p0-design.md` §5、§9–12；依赖 `2026-09-18-credis-p0-01-foundation.md`。

## Global Constraints

- 上游审阅基线 `2d09c890e06e7c12906f1b641e8248646816590a`（执行前核对）；保留 `module github.com/linux-do/credit`，除非单独批准全仓替换。
- 1 LDC = 1 个 `int64` 最小单位；所有余额增减不得用浮点。现有 `users.*_balance numeric(20,2)` 仅作兼容投影；已有小数余额不得静默截断，须拒绝并进入迁移核查。
- `available_balance = Σ available_delta`、`frozen_balance = Σ frozen_delta`、`pending_balance = Σ pending_delta`；可用余额仅对既有奖励消费后冲正允许负值。
- 所有新增 Schema 采用显式版本迁移；禁止仅以现有 `internal/db/migrator/migrator.go` 的 `AutoMigrate` 作为新表发布机制。
- 旧支付/转账/商户/红包/争议默认关闭；未证明其所有余额入口经账本，不能重新开启。
- 当前不导入真实论坛或生产余额；`opening` 只定义能力和批准流程，不创建无证据历史流水。

---

## File Structure

- `credis/internal/db/migrator/{migrator.go,sql/0001_credis_ledger.up.sql,sql/0002_credis_ledger_chain.up.sql}`：按序执行的不可回退扩展迁移及版本记录。
- `credis/internal/model/{ledger.go,forum.go}`：持久化模型，使用 SQL 迁移而不是 AutoMigrate 新表。
- `credis/internal/ledger/{types.go,post.go,chain.go,rebuild.go}`：唯一记账 API、链路结算/冲正与投影重建；各自对应 `_test.go`。
- `credis/internal/service/payment.go`、`credis/internal/apps/user/tasks.go`、`credis/internal/apps/payment/routers.go`、`credis/internal/apps/redenvelope/{routers.go,tasks.go}`、`credis/internal/apps/merchant/link/routers.go`：盘点旧写入口，先关闭再迁入账本，不能任其与新账本并存。
- `credis/docs/ledger-invariants.md`：动作表、兼容差异、上线校验查询。

### Task 1: 显式迁移与账务基础表

**Files:** Create `credis/internal/db/migrator/sql/0001_credis_ledger.up.sql`, `credis/internal/model/ledger.go`, `credis/internal/db/migrator/migrator_test.go`; modify `credis/internal/db/migrator/migrator.go`.

**Interfaces:** Produces `ledger_accounts(id, forum_user_id, available_balance, frozen_balance, pending_balance, projection_version)`、`ledger_entries(id, account_id, action, available_delta, frozen_delta, pending_delta, source_kind, source_id, rule_version_id, settlement_batch_id, idempotency_key, original_entry_id, occurred_at, posted_at, audit_id)`、`outbox_events`、`audit_logs`；整数均为 `bigint`。

- [ ] **Step 1: 先写 PostgreSQL 测试。** 用隔离 `TEST_DATABASE_URL` 建空库，运行 `ApplyVersioned` 两次、确认版本只执行一次；直接 `UPDATE/DELETE ledger_entries` 必须失败；插入重复 `(account_id,idempotency_key)` 失败；提供两个文件 `0002_a.up.sql`、`0002_b.up.sql` 时 `ValidateVersions` 必须失败，编号 0001/0003 缺口也必须失败。先跑 `go test ./internal/db/migrator -run TestLedgerMigration -v` 观察 RED。

```go
func TestDuplicateMigrationVersionFails(t *testing.T) {
    if err := ValidateVersions([]string{"0001_credis_ledger.up.sql", "0002_a.up.sql", "0002_b.up.sql"}); err == nil {
        t.Fatal("duplicate migration version accepted")
    }
}

func TestLedgerMigration(t *testing.T) {
    dsn := os.Getenv("TEST_DATABASE_URL")
    if dsn == "" { t.Skip("requires isolated PostgreSQL") }
    db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
    if err != nil { t.Fatal(err) }
    if err := ApplyVersioned(db); err != nil { t.Fatal(err) }
    if err := ApplyVersioned(db); err != nil { t.Fatal(err) }
    var count int64
    if err := db.Table("schema_migrations").Where("version = ?", 1).Count(&count).Error; err != nil { t.Fatal(err) }
    if count != 1 { t.Fatalf("migration applied %d times", count) }
}
```

- [ ] **Step 2: 写迁移核心，补足全部约束及索引。** 以下为必须落实的 DDL 核心；`audit_logs`、`outbox_events` 同批建立；所有表新增 `created_at`、索引和 FK，`ledger_entries` 仅 INSERT 权限；不可变 trigger 排除业务方更新/删除。版本执行器 `ApplyVersioned(db *gorm.DB) error` 在取得 PG advisory lock 前调用 `ValidateVersions(names []string) error`，拒绝重复/缺失迁移编号，再用 PG advisory lock + 单事务 `schema_migrations`，在 `migrator.Migrate()` 中先执行版本 SQL，旧 `AutoMigrate` 仅留旧表且不能修改新表。

```sql
CREATE TABLE ledger_accounts (
 id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
 forum_user_id bigint NOT NULL UNIQUE,
 available_balance bigint NOT NULL DEFAULT 0,
 frozen_balance bigint NOT NULL DEFAULT 0 CHECK (frozen_balance >= 0),
 pending_balance bigint NOT NULL DEFAULT 0 CHECK (pending_balance >= 0),
 projection_version bigint NOT NULL DEFAULT 0
);
CREATE TABLE audit_logs (
 id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
 actor_id text NOT NULL, reason text NOT NULL, approved_by text,
 created_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE ledger_entries (
 id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
 account_id bigint NOT NULL REFERENCES ledger_accounts(id),
 action text NOT NULL CHECK (action IN ('earn','freeze','consume','release','pending_settle','refund','reversal','restore','opening','manual_adjustment')),
 available_delta bigint NOT NULL DEFAULT 0,
 frozen_delta bigint NOT NULL DEFAULT 0,
 pending_delta bigint NOT NULL DEFAULT 0,
 source_kind text NOT NULL, source_id text NOT NULL,
 rule_version_id bigint, settlement_batch_id bigint,
 idempotency_key text NOT NULL, original_entry_id bigint REFERENCES ledger_entries(id),
 occurred_at timestamptz NOT NULL, posted_at timestamptz NOT NULL DEFAULT now(),
 audit_id bigint REFERENCES audit_logs(id),
 UNIQUE (account_id,idempotency_key),
 CHECK (available_delta <> 0 OR frozen_delta <> 0 OR pending_delta <> 0)
);
CREATE TABLE outbox_events (
 id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
 operation_id text NOT NULL UNIQUE, target text NOT NULL,
 target_key text NOT NULL, projection_version bigint NOT NULL DEFAULT 0,
 payload jsonb NOT NULL, status text NOT NULL DEFAULT 'pending',
 lease_until timestamptz, attempts integer NOT NULL DEFAULT 0,
 remote_id text, evidence jsonb, created_at timestamptz NOT NULL DEFAULT now()
);
```

```go
func ValidateVersions(names []string) error {
    seen := make(map[int]bool, len(names))
    for _, name := range names {
        parts := strings.SplitN(filepath.Base(name), "_", 2)
        if len(parts) != 2 { return fmt.Errorf("invalid migration name %q", name) }
        version, err := strconv.Atoi(parts[0])
        if err != nil || version < 1 || seen[version] { return fmt.Errorf("invalid or duplicate migration version %q", name) }
        seen[version] = true
    }
    for i := 1; i <= len(names); i++ {
        if !seen[i] { return fmt.Errorf("missing migration version %04d", i) }
    }
    return nil
}
```

- [ ] **Step 3: GREEN 后验证旧 Schema 与新 Schema 可重复启动。** `go test ./internal/db/migrator -v`，分别在全新空库应用 0001→0002 与已应用 0001 的旧库升级到 0002，连续启动三角色并查 `schema_migrations`，确认两个路径均不重放 0001、不自动降级、不产生凭空 opening；提交当前最小改动。

```bash
git -C credis add internal/db/migrator internal/model/ledger.go
git -C credis commit -m 'feat: add explicit immutable ledger schema' -m 'Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>'
```

### Task 2: 单一记账 API、三桶投影及双写

**Files:** Create `credis/internal/ledger/{types.go,post.go,post_test.go,rebuild.go,rebuild_test.go}`, `credis/docs/ledger-invariants.md`.

**Interfaces:** Export `type Delta struct { Available, Frozen, Pending int64 }`; `type EntryInput struct { AccountID int64; Action string; Delta Delta; SourceKind, SourceID, IdempotencyKey string; RuleVersionID, SettlementBatchID, OriginalEntryID *int64; OccurredAt time.Time; AuditID *int64 }`; `func Post(tx *gorm.DB, input EntryInput) (entryID int64, duplicate bool, err error)`；`func Rebuild(tx *gorm.DB, accountID int64) (Delta, error)`。**只允许调用者在已开启的事务里调用 Post，不得嵌套提交。**

- [ ] **Step 1: 写红灯测试。** `earn` 即时 `(5,0,0)`、待结算 `(0,0,5)`、`pending_settle (5,0,-5)`、`freeze(-2,2,0)`、冻结消费 `(0,-2,0)`、退款 `(2,0,0)`；非法 earn 双桶、零额、无来源/审批 manual adjustment 必须报错。重复 key 返回原 ID、三个余额和 outbox 条数不变。事务回滚后无 entry/account/user/outbox。负可用仅用于合法 reversal，冻结/待结算不得负。

```go
func TestEarnCannotWriteTwoBuckets(t *testing.T) {
    err := Validate(EntryInput{AccountID:1, Action:"earn", Delta:Delta{Available:5, Pending:5}, SourceKind:"forum_event", SourceID:"e1", IdempotencyKey:"e1"})
    if err == nil || err.Error() != "invalid earn buckets" { t.Fatalf("unexpected validation: %v", err) }
}
```

- [ ] **Step 2: 实施动作校验与 Post。** `Validate(EntryInput) error` 显式列出每个动作的 delta 组合、来源关联与审批；审核日志必须有非操作者本人 `approved_by` 且在同一事务中已确认，不能只凭非空 audit ID；`Post` 使用 `SELECT ... FOR UPDATE` 锁账户、`INSERT ... ON CONFLICT DO NOTHING RETURNING id` 做幂等，再分别累加三桶、增加版本；只在成功插入时更新 `users.available_balance` 和 `users.pending_balance`（用 `decimal.NewFromInt`，不乘/除 100），并写 outbox；所有操作使用同一 `tx`。`frozen_balance` 仅在新表；对旧 decimal 小数数据 fail closed。`Rebuild` 用 `SUM` 三桶比较投影与旧字段，输出差异但不自动修正。

```go
type Delta struct { Available, Frozen, Pending int64 }
func Validate(in EntryInput) error {
    if in.AccountID <= 0 || in.IdempotencyKey == "" || in.SourceKind == "" || in.SourceID == "" { return errors.New("missing account or source") }
    d := in.Delta
    if d.Available == 0 && d.Frozen == 0 && d.Pending == 0 { return errors.New("zero delta") }
    switch in.Action {
    case "earn":
        if !(d.Available > 0 && d.Frozen == 0 && d.Pending == 0 || d.Available == 0 && d.Frozen == 0 && d.Pending > 0) { return errors.New("invalid earn buckets") }
    case "pending_settle":
        if in.OriginalEntryID == nil || d.Available <= 0 || d.Pending != -d.Available || d.Frozen != 0 { return errors.New("invalid settlement") }
    case "freeze":
        if d.Available >= 0 || d.Frozen != -d.Available || d.Pending != 0 { return errors.New("invalid freeze") }
    case "release":
        if d.Available <= 0 || d.Frozen != -d.Available || d.Pending != 0 { return errors.New("invalid release") }
    case "consume":
        if d.Pending != 0 || !(d.Available < 0 && d.Frozen == 0 || d.Available == 0 && d.Frozen < 0) { return errors.New("invalid consume") }
    case "refund":
        if in.OriginalEntryID == nil || d.Available <= 0 || d.Frozen != 0 || d.Pending != 0 { return errors.New("invalid refund") }
    case "reversal":
        if in.OriginalEntryID == nil || d.Available > 0 || d.Frozen > 0 || d.Pending > 0 { return errors.New("invalid reversal") }
    case "restore":
        if in.OriginalEntryID == nil || d.Available < 0 || d.Frozen < 0 || d.Pending < 0 { return errors.New("invalid restore") }
    case "opening":
        if in.AuditID == nil || d.Available < 0 || d.Frozen < 0 || d.Pending < 0 { return errors.New("opening requires audited source") }
    case "manual_adjustment":
        if in.AuditID == nil { return errors.New("manual adjustment requires approval") }
    default: return errors.New("unknown action")
    }
    return nil
}
```

- [ ] **Step 3: PostgreSQL 并发/恢复测试。** 两个合法不同来源并发消费仅剩 1 的可用，最多一个成功；多个账户锁按 account ID 排序。杀掉调用者在提交前/后重试，重复不多记；`go test -race ./internal/ledger/...`，再用 SQL `SUM` 逐账户核对三桶和原 User 兼容字段；测试未配置 PostgreSQL 时跳过**不算完成**，CI 必须有真实 PG job。
- [ ] **Step 4: 在 Fork 提交。**

```bash
git -C credis add internal/ledger docs/ledger-invariants.md
git -C credis commit -m 'feat: post immutable entries and project balances atomically' -m 'Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>'
```

### Task 3: 结算、冲正、恢复与链路约束

**Files:** Create `credis/internal/ledger/{chain.go,chain_test.go}`, `credis/internal/db/migrator/sql/0002_credis_ledger_chain.up.sql`, `credis/internal/model/settlement.go`; do not rewrite released or already-applied `0001`.

**Interfaces:** Export `Settle(tx *gorm.DB, earnID int64, amount int64, batchID int64) error`, `Reverse(tx *gorm.DB, earnID int64, amount int64, eventKey string) error`, `Restore(tx *gorm.DB, earnID int64, amount int64, eventKey string) error`, `ReversalDeltas(pendingNet, availableNet, amount int64) (availableDelta, pendingDelta int64, err error)`；`CanReverseCycle(granted, alreadyReversed, amount int64) bool`；`reward_cycles(earn_entry_id, cycle_no, opened_by_entry_id, granted_amount, reversed_amount, pending_net, available_net)` 为可重建链路投影，流水增加 `reward_cycle_id` 关联。结算按当前周期仍有效的 pending 权益核限，所有周期对同一笔奖励不得重复结算；**冲正上限按当前周期未冲正的净应得核定，而非跨所有周期累计历史冲正**；`release_on_reversal=false` 恢复只能复用原 Claim 的获配额、至多补回该关系已撤销的净额；`true` 时新激活周期由 03 的新 Claim 按当期版本获配，金额可能变化但必须有新规则版本/额度证据，均创建新周期。`settlement_batches`、周期表、FK 和索引在 `0002` 中添加；单次撤销操作键为 `account_id + reward_cycle_id + source_event_id/stable_event_key + reversal`，映射**恰好一条** `ledger_entries` 流水；该流水可同时写负的 `available_delta` 和 `pending_delta`，两桶变动不得拆成共用幂等键的两条记录。同一事件重试复用同一键并返回同一流水 ID；不同合法撤销使用不同源事件版本/周期，不得被误判为重试。

- [ ] **Step 1: 写链路 RED 测试。** 待结算 5→冲正 5 为 `(0,0,-5)`；待结算 5→结算 2→冲正 5 为**一条** `(-2,0,-3)` 的 reversal；此时冲正前账户 `(available=2,pending=3)`、冲正后 `(0,0)`，余额总额恰好减少 5；以相同 `eventKey` 重复请求返回首次的流水 ID，reversal 行数仍为 1、三桶投影和 Outbox 条数不变；全部结算 5→消费 5→冲正 5 为 `(-5,0,0)`，可用变 -5；恢复 5 仅产生 `restore`，保留历史。重点以**不同事件版本**验证 `earn 5 → reversal 5 → restore 5 → reversal 5 → restore 5 → reversal 5` 最终净奖励 0、同周期至多冲正 5、三个取消各有原始流水；每个撤销/恢复的相同 `eventKey` 重试不新增流水。同组事件的多种到达顺序经 03 的版本收敛重放后净账一致；含待结算/部分结算恢复的链条也验证当前桶位。并发结算/同周期冲正最多当前应得额。

```go
func TestReversalDeltasAfterPartialSettlement(t *testing.T) {
    available, pending, err := ReversalDeltas(3, 2, 5)
    if err != nil { t.Fatal(err) }
    if available != -2 || pending != -3 { t.Fatalf("available=%d pending=%d", available, pending) }
    originalID := int64(42)
    entry := EntryInput{
        AccountID:1, Action:"reversal", Delta:Delta{Available:available, Frozen:0, Pending:pending},
        SourceKind:"forum_event", SourceID:"cancel-v2", OriginalEntryID:&originalID,
        IdempotencyKey:"account:1:cycle:7:cancel-v2:reversal",
    }
    if err := Validate(entry); err != nil { t.Fatalf("mixed-bucket reversal rejected: %v", err) }
}

func TestRepeatedCancellationUsesRestoredCycle(t *testing.T) {
    if !CanReverseCycle(5, 0, 5) { t.Fatal("first reversal rejected") }
    if CanReverseCycle(5, 5, 5) { t.Fatal("same cycle reversed twice") }
    // 新 restore 开始下一周期：旧周期已冲正 5 不占新周期的上限。
    if !CanReverseCycle(5, 0, 5) { t.Fatal("second valid reversal rejected") }
    if _, _, err := ReversalDeltas(0, 5, 5); err != nil { t.Fatal(err) }
}
```

```go
// 同一周期累计撤销不能超过该周期获配额；新 restore 周期单独计数。
func CanReverseCycle(granted, alreadyReversed, amount int64) bool {
    return granted > 0 && alreadyReversed >= 0 && alreadyReversed <= granted &&
        amount > 0 && amount <= granted-alreadyReversed
}

// 桶位来自当前 reward_cycle 的净应得（含最近 restore），不是历史 earn 总额。
func ReversalDeltas(pendingNet, availableNet, amount int64) (int64, int64, error) {
    if pendingNet < 0 || availableNet < 0 || amount <= 0 || amount > pendingNet+availableNet {
        return 0, 0, errors.New("reversal exceeds active cycle entitlement")
    }
    fromAvailable := min(amount, availableNet)
    return -fromAvailable, -(amount-fromAvailable), nil
}
```

```sql
-- 在隔离 PG 集成测试中对 earn(0,0,+5)→settle(+2,0,-2)→Reverse(5,"cancel-v2")
-- 连续执行两次后的同一账户/源事件查询；结果须为 (1,-2,0,-3)。
SELECT count(*) AS rows, COALESCE(sum(available_delta),0) AS available,
       COALESCE(sum(frozen_delta),0) AS frozen, COALESCE(sum(pending_delta),0) AS pending
FROM ledger_entries
WHERE account_id = (SELECT account_id FROM ledger_entries WHERE source_id = 'cancel-v2' ORDER BY id LIMIT 1)
  AND action = 'reversal' AND source_id = 'cancel-v2';
-- 账户三桶须为 available=0, frozen=0, pending=0；
-- 记录第一次 entry_id，第二次 Reverse 须返回同一个 ID，Outbox 行数保持首次操作后的值。
```

- [ ] **Step 2: 实施 `ReversalDeltas`（先冲减当前周期 available，再冲减 pending）、周期行锁和限制。** `earnID` 与当前 `reward_cycles` 行按固定顺序加锁，结算批次唯一防并发；周期净额从**该周期开始的 earn/restore、相关 pending_settle、该周期 reversal** 重建并核对投影，不能以原 earn 的“累计结算−累计冲正”代替。每次全额恢复在同一 `earnID` 下创建 `cycle_no+1`，restore 关联被恢复的前次撤销、reversal 关联本周期 earn/restore，同时保留 root earn；部分恢复/部分冲正按本周期当前净额核限；`Reverse` 在同一 PG 事务中一次调用 `ledger.Post`，写入 `Delta{Available:availableDelta, Frozen:0, Pending:pendingDelta}` 一行；`ledger.Post` 在同事务内更新账户投影、兼容余额与 Outbox，`Reverse` 只补充更新当前周期投影，任一失败整体回滚，重复键不再更新任何投影。有效净权益须等于 03 的当前 Claim 获配额；已结束周期仅保留历史，不能给当前权益重复计数。一个周期只有一笔有效全额冲正（部分冲正可多笔但总额不超本周期）；**新周期可合法再次冲正**。原始事件稳定 key 与周期号进入账本幂等键，重复重试返回既有 ID；拒绝超额和负金额，不修改旧流水。执行 `go test ./internal/ledger -run 'Test(Settle|Reverse|Restore|RewardCycle|MixedBucketIdempotency)' -v`，其中 `TestMixedBucketIdempotency` 必须使用真实 PostgreSQL、执行两次相同撤销并断言流水 ID/行数、三个桶及 Outbox 计数。
- [ ] **Step 3: 逐桶重建并提交。** SQL 逐账户对账 `ledger_accounts`、`ledger_entries` 和 `users`；核查冻结/结算守恒、每个周期全额冲正唯一、跨周期多轮撤销/恢复的净额、发生与入账时间及原流水/恢复关联。记录查询和恢复手册；严禁自动修补原流水。

```bash
git -C credis add internal/ledger internal/model/settlement.go internal/db/migrator docs/ledger-invariants.md
git -C credis commit -m 'feat: reconcile settlement and reversal chains' -m 'Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>'
```

### Task 4: 收口历史余额写入口与上线保护

**Files:** Modify `credis/internal/service/payment.go`, `credis/internal/apps/user/tasks.go`, `credis/internal/apps/payment/routers.go`, `credis/internal/apps/redenvelope/{routers.go,tasks.go}`, `credis/internal/apps/merchant/link/routers.go`, `credis/internal/task/{worker/worker.go,scheduler/scheduler.go}`; create `credis/internal/service/balance_gate_test.go`, `credis/docs/balance-writer-audit.md`.

**Interfaces:** Any authorized monetary action must call `ledger.Post(tx, ledger.EntryInput)` inside the existing order transaction. Default-off legacy paths must return explicit disabled status at all route/job entry points; re-enable condition is zero untracked write sites and green chain tests.

- [ ] **Step 1: 记录真实写入清单与失败测试。** 上游 `internal/service/payment.go` 有 `UpdateBalance`、`SettlePendingToAvailable` 及退款直写，`internal/apps/user/tasks.go` 有 Gamification 反向导入直写；扫描全部 GORM `UpdateColumns/Updates/Exec`、原子 SQL 和 admin 路由。为每类支付、退款、pending settle、红包、商户写禁用/审计测试：flag=false 时调用不得影响 users 或 ledger；flag=true 且路径尚未改造时必须拒绝启动。先运行 `go test ./internal/service ./internal/apps/...` 为 RED。

```bash
cd credis
grep -RInE 'available_balance|pending_balance|community_balance|total_community' internal --include='*.go'
go test ./internal/service ./internal/apps/...
```

- [ ] **Step 2: 把允许开启的旧写入逐个路由至 Post。** 保留订单作为业务事实；在同一事务里将订单 ID 作为 `source_id`，指定冻结/消费/释放/退款等明确动作和唯一 `idempotency_key`；不同角色账户加锁排序，校验原余额与新投影一致。任何小数 `decimal.Decimal` 或旧余额无 opening 证据时拒绝启用，不自动四舍五入。**旧 Gamification 导入不得借此重开**；`admin` 仅允许经审计双人批准的 `manual_adjustment`，不可写 users.balance。
- [ ] **Step 3: 全路径集成测试和静态写点核查。** 对每一旧调用点创建真实 PG 事务测试，比较 `User = ledger_accounts = SUM(ledger_entries)`；增加 CI 脚本拒绝新增在 `internal/ledger` 之外对余额字段的写 SQL（明确列出兼容投影白名单）。再跑 `go test ./...`，若非 P0 旧模块仍未全部改完，保留永久关闭标记且文档标注**不得启用**，不能声称账务迁移完成。
- [ ] **Step 4: 提交收口和门禁。**

```bash
git -C credis add internal/service internal/apps internal/task docs/balance-writer-audit.md
git -C credis commit -m 'feat: guard and route legacy balance mutations through ledger' -m 'Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>'
```

## Phase Gate

必须提交真实 PostgreSQL 迁移/并发/冲正测试结果、余额差异为零、旧写点清单。发布顺序为扩展 Schema → 双写核对 → 切换唯一写入口 → 以后单独清理兼容字段；禁止假设迁移可自动降级。
