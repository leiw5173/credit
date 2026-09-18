# Credis 上游同步规程

## 前提

- `origin` 仅指向获授权的 Credis Fork。
- `upstream` 必须为 `https://github.com/linux-do/credit.git`。
- 同步只在隔离分支进行；不得直接更新部署分支。

## 流程

1. 从已验收的 Credis 基线创建 `sync/upstream-<date>-<sha>` 分支。
2. 核对 `git remote get-url upstream` 为约定 URL，再执行 `git fetch upstream master`；记录本次同步前已评审的 `OLD_SHA` 和新取得的 `NEW_SHA=$(git rev-parse upstream/master)`，不暗中追踪浮动版本。
3. 在隔离分支执行 `git diff --stat "$OLD_SHA" "$NEW_SHA"` 及 `git diff "$OLD_SHA" "$NEW_SHA" -- internal frontend go.mod go.sum`，并审阅完整差异；保存两端 SHA 与审查结论。重点核查：
   - Schema、迁移、GORM `AutoMigrate` 行为；
   - `available_balance`、`pending_balance`、`community_balance` 等余额写点；
   - OAuth、权限、Webhook、Secret、日志和依赖安全变更。
4. 差异评审后，才在该隔离分支合入固定的 `NEW_SHA`，解决冲突并再次审阅合并结果；不得直接在部署分支合并。随后在隔离 PostgreSQL/Redis 上运行 Go 全量测试、前端冻结安装/lint/type-check/build、API/Worker/Scheduler 启动与任务测试。
5. 对本地真实 Discourse 重新执行身份、Webhook、Gamification 和 P0 行为回归；mock 不能替代。
6. 保存版本、许可证、测试与差异证据，经人工评审后合并至 Credis 分支。
7. 父仓库仅在 Credis 提交验收后更新 gitlink。

## 禁止项

- 不自动合并或部署 upstream 默认分支。
- 不在同步时读取生产论坛数据或生产备份。
- 不修改 Discourse Core。
- 不跳过失败测试，不把缺少工具或未执行项目记作 PASS。
- 不在未审计时启用支付、转账、商户、红包、争议或旧 Gamification 反向余额同步。
