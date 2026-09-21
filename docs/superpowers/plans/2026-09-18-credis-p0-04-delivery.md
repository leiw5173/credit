# Credis P0 外部同步与对账 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 可靠地将已提交账本的贡献分、通知和对账任务投递至 Discourse，并明确区分成功、失败和执行结果未知。

**Architecture:** PostgreSQL Outbox 为唯一持久任务源；Asynq 仅做加速。按目标串行领取并通过远端幂等键、业务操作终态或经证明的执行时限解除阻塞；不具备这些能力时人工核查或单独批准最终一致性降级。

**Tech Stack:** Go 1.26、GORM/PostgreSQL、Redis/Asynq、Discourse REST、OpenTelemetry、Gin。

**Spec:** `docs/superpowers/specs/2026-09-18-credis-p0-design.md` §6、§8.1–8.3、§9–12；依赖 01 事件能力矩阵、02 账本 Outbox、03 事务处理。

## Global Constraints

- 旧 `internal/apps/user/tasks.go` 从论坛榜单导入余额的方向必须关闭；新方向是 Credis ledger → forum Gamification 展示投影，不能并开。
- 贡献榜只计切换时间后有效奖励净额；消费/冻结/释放/退款不动榜；规则冲正允许榜分下降。
- `delivery_unknown`/客户端超时可能是远端仍在运行，**远端当前分值=旧值不足以确认旧请求结束**；租约到期也不是自动重发许可。
- 禁止无幂等/查询能力时自动重试用户可见私信、邮件或原帖回复；Redis 丢失不得丢失事实。
- Discourse 写入能力未由真实实例证实时，保持外部投影关闭，不能以 mock 通过代替强一致验收。

---

## File Structure

- `credis/internal/outbox/{contract.go,claim.go,deliver.go,recover.go}`：适配器契约、PG 租约、发送状态机和 Redis 重建；各自 `_test.go`。
- `credis/internal/integrations/discourse/{client.go,gamification.go,notifications.go}`：真实 API 客户端、贡献榜投影与通知通道；`client_test.go` 使用受控迟到响应服务器，另有真实 Discourse E2E。
- `credis/internal/reconcile/{accounts.go,events.go,external.go,discourse.go}`：账本/处理结果/Outbox/规则/论坛 API 对账。
- `credis/internal/task/{constants.go,worker/worker.go,scheduler/scheduler.go}`、`credis/internal/apps/admin/reconciliation/routers.go`：队列、调度、异常清单与管理员操作。
- `credis/docs/{external-delivery-contracts.md,gamification-cutover.md,reconciliation-runbook.md}`：逐目标能力证据、置零/切换基线、恢复手册。

### Task 1: PG Outbox 租约、恢复和交付契约

**Files:** Create `credis/internal/outbox/{contract.go,claim.go,deliver.go,recover.go,deliver_test.go}`; modify `credis/internal/task/{constants.go,worker/worker.go,scheduler/scheduler.go}` and versioned SQL migration `credis/internal/db/migrator/sql/0004_credis_delivery.up.sql`.

**Interfaces:** `Contract{IdempotencyKey bool, QueryByOperation bool, ConditionalWrite bool, MaxExecutionDuration *time.Duration, AtLeastOnce bool}`；`Deliver(ctx context.Context, operationID string, adapter Adapter) error`；`Adapter{Send(ctx,operationID,payload) (remoteID string,error), VerifyOperation(ctx,operationID) (status string,evidence []byte,error)}`。状态：`pending/leased/completed/failed_retryable/delivery_unknown/superseded/failed_terminal`。

- [ ] **Step 1: RED 故障注入测试。** PG 有 pending Outbox、Redis 清空后仍投递；请求远端已成功但客户端响应丢失时：带幂等键的重试复用同一操作 ID，只有查询能力的先核查终态，无两者的转人工 `delivery_unknown`；远端已完成但 Worker 未标记本地完成时效果相同。租约超时仍可能有远端在途，不直接重发。跑 `go test ./internal/outbox -v` 为 RED。

```go
func TestUnknownWithoutRemoteEvidenceBlocksRetry(t *testing.T) {
    contract := Contract{AtLeastOnce:true}
    if CanRetryAutomatically(contract, "delivery_unknown", false) { t.Fatal("unknown delivery retried without evidence") }
}
```

- [ ] **Step 2: 扩展 Outbox 持久元数据。** 加 `request_digest`、`delivery_contract`、`last_error`、`next_attempt_at`、`remote_operation_id`、`checked_at`、`claimed_by`、`lease_token`，`external_sync_records` 留目标、对象、版本、请求/响应、远端业务 ID 与核查证据；PG `FOR UPDATE SKIP LOCKED` 领取带租约任务，租约到期先 VerifyOperation；明确失败才重试。`CanRetryAutomatically` 的返回仅在远端幂等可保证，或核查确认操作未执行且不在途时为真。

```go
func CanRetryAutomatically(c Contract, status string, terminalNotExecuted bool) bool {
    if status != "delivery_unknown" { return status == "pending" || status == "failed_retryable" }
    return c.IdempotencyKey || (c.QueryByOperation && terminalNotExecuted)
}
```

- [ ] **Step 3: 实现三队列和 PG 扫描恢复。** `critical` 接事件/冲正、`normal` 同步/通知、`maintenance` 对账/快照；Scheduler 定期读 PG 中的 `forum_events`、`outbox_events`、`external_sync_records` 和游标，补投 Asynq；Redis 重启后无任何凭空重发未知状态。`go test ./internal/outbox ./internal/task/... -count=1`、PG/Redis 断线重启实验 GREEN。
- [ ] **Step 4: 提交。**

```bash
git -C credis add internal/outbox internal/task internal/db/migrator
git -C credis commit -m 'feat: deliver recoverable outbox operations under lease' -m 'Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>'
```

### Task 2: Gamification 单调投影和置零切换

**Files:** Create `credis/internal/integrations/discourse/{client.go,gamification.go,gamification_test.go}`, `credis/docs/{external-delivery-contracts.md,gamification-cutover.md}`; modify `credis/internal/apps/user/tasks.go` and Outbox payload producer in `credis/internal/ledger/post.go`.

**Interfaces:** `ProjectScore(ctx context.Context, forumUserID int64, version int64, target int64) error`；`target` 为切换后累计净贡献分，不等于可用余额；`projection_version` 每目标单调。`Operation{Version int64; Status string; TargetScore int64}`；`CanAdvance(previous Operation, observedScore int64, operationTerminal bool) bool` 仅在远端已确认终态/有版本栅栏/已验证最大执行时限并复核时返回 true，不能以分值相等解锁。实际论坛写入接口/终态语义来自真实能力测试，不假设上游已有写 API。

- [ ] **Step 1: RED 竞态测试。** 初始论坛 10，旧请求“设置 10”在途，查到论坛 10，Credis 新目标 15：不能发送 15 直到旧请求终态或可靠栅栏；旧写晚于新写完成仍不能使严格模式最终停在 10；投影从 15 冲正到 12 必须允许下降。若仅有分值查询，测试要求状态保持阻塞；单独获批最终一致性模式可暂时回退但持续核对修复到 15 并记录时长。运行 `go test ./internal/integrations/discourse -run TestProjection -v` RED。

```go
func TestReadingOldScoreDoesNotUnblock(t *testing.T) {
    previous := Operation{Version:1, Status:"delivery_unknown", TargetScore:10}
    current := int64(10)
    if CanAdvance(previous, current, false) { t.Fatal("current score cannot prove old write terminated") }
}
```

```go
func CanAdvance(previous Operation, observedScore int64, operationTerminal bool) bool {
    // observedScore 仅供对账；不得将值相等视作请求终态。
    return operationTerminal || previous.Status == "completed" || previous.Status == "superseded"
}
```

- [ ] **Step 2: 基于真实 API 证据选择交付模式。** `external-delivery-contracts.md` 逐端点记录：绝对写分能力、远端幂等键、按操作 ID 查终态、条件版本写、最大执行/排队时限；不满足任一强保证时默认停用严格自动投影并由管理员决定是否接受单独的最终一致性契约。`CanAdvance(old Operation, observedScore int64, operationTerminal bool) bool` 不使用 `observedScore` 解锁，除非已验证版本化条件写或超过*已验证*最大时限且再次核验；未发送旧版本标记 superseded，在途旧版本不得抢跑新版本。
- [ ] **Step 3: 建立上线置零证据。** 在**授权的预发布论坛**记录站点设置、现有榜单、排除用户、切换时间；把 Discourse 内置主题/回复/点赞/阅读/访问/Solved/邀请分值全置零并逐项验收。历史榜单单独存基线、不补算；Credis 生效后对新增、冲正和消费分别核验 +N/-N/0；生产站点变更另行批准，本计划不执行。
- [ ] **Step 4: 跑真实论坛 E2E 与模拟竞态，两者均 GREEN 再提交。** 如果论坛写 API 缺失，只能提交关闭的适配器和阻塞证据，不能宣称本 Task 完成或上线门槛通过。

```bash
git -C credis add internal/integrations/discourse internal/apps/user/tasks.go internal/ledger/post.go docs/external-delivery-contracts.md docs/gamification-cutover.md
git -C credis commit -m 'feat: project net contribution with remote-write fencing' -m 'Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>'
```

### Task 3: 私信、邮件、原帖回复与结果未知队列

**Files:** Create `credis/internal/integrations/discourse/{notifications.go,notifications_test.go}`; extend `credis/internal/outbox/deliver.go`; create `credis/internal/apps/admin/delivery/routers.go`.

**Interfaces:** `SendNotice(ctx context.Context, notice Notice) (remoteID string, err error)`；`Notice{OperationID, Channel, Recipient, Subject, Body string}`；渠道分别 `forum_message`/`email`/`post_reply`，每条出站操作在 Outbox 只有一个稳定 ID。

- [ ] **Step 1: RED 测试。** 处罚与积分状态发私信/邮件、内容缺项/迁移发原帖回复；远端成功响应丢失或 Worker 崩溃，在不支持幂等/操作终态时进入 `delivery_unknown` 而不再发第二封/第二帖；管理员确认未送达后才能重发，审批人/原因/证据入 audit_logs。`go test ./internal/integrations/discourse ./internal/outbox -run TestNotice -v`。
- [ ] **Step 2: 最小实现。** 仅从可信规则/审核结果生成通知；基于真实接口提供的幂等或查询能力择优实现。仅当目标能力允许才将低干扰 `OperationID` 放在正文/元数据用于人工核验；日志永不记录收件内容、个人信息或 Secret。接口返回不明时保留阻塞，管理员仅能通过有审计的 `confirm_not_delivered` 再投递。
- [ ] **Step 3: 真实 Discourse 私信/回帖/邮件测试，记录唯一远端 ID；失败注入与重复投递均通过后提交。**

```bash
git -C credis add internal/integrations/discourse/notifications* internal/outbox internal/apps/admin/delivery
git -C credis commit -m 'feat: deliver audited non-duplicating forum notices' -m 'Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>'
```

### Task 4: 六类对账与异常观测

**Files:** Create `credis/internal/reconcile/{accounts.go,events.go,external.go,discourse.go,reconcile_test.go}`, `credis/internal/apps/admin/reconciliation/routers.go`, `credis/docs/reconciliation-runbook.md`; modify `credis/internal/task/scheduler/scheduler.go`.

**Interfaces:** `Run(ctx context.Context, from, until time.Time) (RunResult, error)`；`RunResult{Status string, Differences int64, IncompleteSources []string}`，状态为 `complete`/`incomplete`/`failed`；异常清单记录类型、来源 ID、首次/最后发现、处理人和证据。

- [ ] **Step 1: RED 测试六类差异。** 账户投影≠流水净额、冻结/结算链错、processed 事件无流水/忽略结论、Outbox 长期未知、Gamification 与 Credis 投影不等、规则生效区间缺口各产生差异；Discourse 分页失效、403、空结果、网络失败统一标记 `incomplete`，不生成“删除/撤销”事件。`go test ./internal/reconcile -v` 为 RED。
- [ ] **Step 2: 实施只读对账。** 从 PostgreSQL 聚合流水、三桶/原 user、额度、来源事件结论和 Outbox；论坛 API 分页必须验证页数/游标完整且权限有效才采纳对象状态。每次将起止游标、检查数、差异、完整性和核查证据保存到 `reconciliation_runs`；差异只告警/生成复核任务，不直接修改历史流水。
- [ ] **Step 3: 运维验收。** 注入误删预警、真实删除、网络断线、投影回退和 Redis 清空；OTel 指标至少包括 Webhook backlog、Outbox unknown、账本差异、投影偏差、对账不完整和日结时长，探针区分 Redis 故障与 PG 事实存储不可用。`go test ./internal/reconcile ./internal/outbox ./internal/ledger -count=1` 和真实论坛分页回归 GREEN 后提交。

```bash
git -C credis add internal/reconcile internal/apps/admin/reconciliation internal/task/scheduler/scheduler.go docs/reconciliation-runbook.md
git -C credis commit -m 'feat: reconcile ledger and forum projections without false deletions' -m 'Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>'
```

## Phase Gate

出示论坛内置计分置零、切换时间、历史榜单快照、真实远端操作终态和前后投影核对；缺任何一项则保持外部投影关闭并继续对账，不以本地 mock 成功宣称上线通过。
