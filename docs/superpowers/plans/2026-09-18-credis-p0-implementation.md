# Credis P0 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 依照已确认的设计，把 Credis Fork、不可变账本、论坛事件、可靠同步、效果监控和部署验收分阶段交付为可独立测试的软件。

**Architecture:** 上游 `linux-do/credit` 整体 Fork 是业务底座，`D:\SpacemIT` 文档仓库通过 submodule 固定它的提交。Go 服务在 PostgreSQL 同事务写入论坛事件结果、账本、投影及 Outbox，Worker 仅以数据库事实驱动外部同步；Discourse 是独立部署单元。

**Tech Stack:** Go 1.26、Gin、GORM、PostgreSQL、Redis/Asynq、Next.js 16.1.1/React 19.2.3、Docker、真实 Discourse 稳定/ESR。

**Spec:** `docs/superpowers/specs/2026-09-18-credis-p0-design.md`（2026-09-18，全文 §1–17）；背景需求 `01-战略方案/激励计划/论坛能力盘点与开发PRD.md`（本计划以已确认设计为准）。

## Global Constraints

- 初期同机隔离、仅 HTTP(S) 通信；论坛和 Credis 不共享 DB、Redis、Docker 项目/网络/卷、Secret、备份、日志。
- 不使用生产论坛数据进行开发；只在未来预发布的已授权脱敏副本上做升级验收；真实迁移与生产 DNS 切换不在此轮实施。
- 不开发无证据的 Discourse 插件，不修改 Core；不自动同步/部署上游默认分支。
- 账本流水为唯一账务事实，整数单位、三桶投影和原用户余额双写核对；信用榜投影不等于余额。
- 旧支付/转账/商户/红包/争议关闭，禁止旧 Gamification 论坛→余额同步与 Credis P0 计分并存。
- 当前工作区只有设计文档、没有 Fork 源码；文件路径依据 2026-09-18 核实的上游 `master` SHA `2d09c890e06e7c12906f1b641e8248646816590a`，开始实施时先固定 Fork SHA，差异需重新核对。
- 计划中的 `git commit` 是将来的执行步骤，本次**不创建远端仓库、不提交、不部署、不使用生产数据**。

---

## File Structure

本设计跨越多个可独立验收的子系统；依 writing-plans 的范围检查拆为六份分项计划。每份的 `File Structure` 节列出准确路径、职责和修改边界；各项 `Task` 包含测试→实现→验证→提交。以父仓库的 `开发文件/` 为相对路径根，`credis/` 以下是独立 Fork 的 gitlink 内容。

| 顺序 | 计划 | 独立可验收结果 | 依赖与入口门槛 |
|---|---|---|---|
| 01 | [Fork 基线与论坛能力](2026-09-18-credis-p0-01-foundation.md) | 三角色可运行、旧模块默认关闭、真实 Discourse 能力矩阵 | 先取得独立 Fork 创建授权，版本固定 |
| 02 | [不可变账本](2026-09-18-credis-p0-02-ledger.md) | PG 显式迁移、流水/三桶/双写/冲正不变量及旧入口门禁 | 01 的 Fork 基线 |
| 03 | [论坛事件与规则](2026-09-18-credis-p0-03-forum-events.md) | Webhook、版本关系、P0 八项规则、额度并发与乱序收敛 | 01 能力矩阵 + 02 `ledger.Post` |
| 04 | [外部同步与对账](2026-09-18-credis-p0-04-delivery.md) | 可恢复 Outbox、Gamification、通知和六类对账 | 01/02/03 的事务事实及真实论坛写入契约 |
| 05 | [贡献效果监控](2026-09-18-credis-p0-05-analytics.md) | 排他贡献/状态历史、观察成熟批次、可下钻重算 API | 03 事件、02 流水、04 完整性 |
| 06 | [部署与上线验收](2026-09-18-credis-p0-06-deployment.md) | 同机模板、CI、升级与板块 dry-run、拆分演练、Go/No-Go | 01–05 全部证据；真实预发布操作另行授权 |

## 迁移编号合同

固定为 `0001_credis_ledger` → `0002_credis_ledger_chain` → `0003_credis_forum` → `0004_credis_delivery` → `0005_credis_analytics` → `0006_credis_fencing`。编号全局唯一且连续；执行器先校验重复/缺号，再运行迁移；已发布迁移不可改写。验证全新安装及仅部署过 0001 的升级路线，后续计划一律使用此编号，不因提交先后临时抢号。

## 实施顺序和并行性

- [ ] **Gate A — 上游基线：** 先完成 01 Tasks 1–2；在新建本地论坛开展 01 Task 3。未固定 SHA/Discourse 版本前不启动账务改造。
- [ ] **Gate B — 账务事实：** 完成 02 Tasks 1–3 后再实现 03 计分；02 Task 4 收口/锁死旧入口可与 03 的真实行为能力盘点并行，但**启用 P0 前必须验收**。
- [ ] **Gate C — 事件收敛：** 03 的可信事件、额度、冲正和真实 Discourse 多顺序回放通过后，04 可发送外部投影；05 的来源采集与快照调度可同步开发，但正式指标只取已核验数据。
- [ ] **Gate D — 外部副作用：** 04 的 Gamification 写操作终态能力未获验证时，保持目标串行阻塞或经单独审批才降级为最终一致；绝不能仅因读取当前分数一致而宣称通过。
- [ ] **Gate E — 上线：** 06 的目标版本升级/板块标签 dry-run/论坛内置计分置零/旧榜单基线/全链路真实测试、备份恢复、审计和完整指标证据齐全才允许正式结算。未取得脱敏副本批准时标记 No-Go，不以本地测试替代该门槛。

## 设计覆盖核对

| 设计范围 | 承担计划/任务 |
|---|---|
| §1–3 Fork、版本、同机拓扑、稳定接口 | 01 Tasks 1/3；06 Tasks 1/4 |
| §4 上游复用、真实论坛能力、非 P0 flag | 01 Tasks 1–3；02 Task 4 |
| §5 账本、双写、幂等、额度、关系、冲正 | 02 Tasks 1–4；03 Tasks 1–4 |
| §6 Outbox、远端在途/结果未知、Redis 恢复 | 04 Tasks 1–3；06 Task 4 |
| §7 规则八项、版本与生效时间 | 03 Task 2 |
| §8 榜单、通知、对账、效果监控 | 04 Tasks 2–4；05 Tasks 1–4 |
| §9 错误/人工核查与重放 | 03 Task 4；04 Tasks 1/3/4 |
| §10–12 安全、五层测试、CI/Schema 迁移 | 01 Tasks 2/3；02 Task 1；03 Task 1；06 Tasks 1/2 |
| §13 拆分迁移 | 06 Task 4 |
| §14 真实论坛迁移边界（只写清单） | 06 Task 5 |
| §15–17 阶段、交付物、排除项 | 全部计划；06 Task 5 |

## 决策记录与阻断条件

1. **上游同步方向差异：** 已核实 `credis/internal/apps/user/tasks.go` 目前读取论坛 Gamification 并写入 `users.available_balance`；P0 改为由 Credis 记账后向论坛投影。旧任务/管理手动下发/路由必须在 P0 下禁用并测试。
2. **旧余额精度差异：** 上游 `users` 为 `numeric(20,2)`，新流水采用整数；不能默默把小数直接截成整数或用浮点转换，必须在实际迁移入口核对并阻断不兼容数值。
3. **论坛能力未知：** 点赞、收藏、Solved、举报、徽章、Gamification 的事件版本与写入契约必须在真实论坛实测，不能把设计中的理想能力当上游已实现。
4. **生产数据授权：** 计划给出预发布脱敏副本升级的验收方式，但实际获取和处理该副本需另外批准；此时若未完成，正式结算的结论仍是 No-Go。
5. **开发工具现状：** 当前机器可读取上游 Git 仓库，但 `gh`、`go`、`pnpm`、`docker` 均未在当前 Bash PATH；可用的便携 Node 位于 `C:\Users\wanglei\AppData\Local\Programs\nodejs\node.exe`。执行前须在获授权的隔离环境提供 Go 1.26、Docker、pnpm 与数据库服务并核验版本；本次未运行任何实现测试。Fork 经授权在 GitHub 页面创建后由 `git submodule add` 接入，不依赖本机管理员权限。

## 执行方式

逐份阅读本总览、对应分项计划及原设计；对每项先写失败测试、确认失败原因、实现最小变更、重新运行相关/全量测试、做审查和提交。每个交付门槛保存实际命令输出与真实论坛证据；失败/未运行要按实记录，不能把“计划中应通过”写成“已通过”。
