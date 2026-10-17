# 邮箱删除与记录

角色资料的“邮箱”支持标题/编号、发件人、领取状态和起止时间筛选，以及单封、选中批次和跨页清理。“邮件删除记录”按角色分页展示回执与原附件信息。每批最多100封，网页每页20封。

填写原因后先预览。确认框逐封展示标题、领取状态、原金币和附件数量；含未领取金币或附件时，需勾选“同时丢弃未领取资产”。已领取的附件只保留历史摘要。删除后邮箱列表移除对应邮件。

## 接口

选中删除的预览与提交请求体上限32KiB，覆盖100封邮件及其版本摘要；跨页清理仍按固定名单分批执行。

| 方法与路径 | 行为 |
| --- | --- |
| `GET /api/v1/characters/{id}/mailbox` | 分页列表 |
| `GET /api/v1/characters/{id}/mailbox/{mailId}` | 正文与附件 |
| `POST /api/v1/characters/{id}/mailbox-operations/preview` | 返回逐封影响、带版本的选中列表和签名 |
| `POST /api/v1/characters/{id}/mailbox-operations` | 提交批次并返回永久回执 |
| `GET /api/v1/characters/{id}/mailbox-operations?limit=20&cursor=...` | 删除记录 |

邮箱列表接受 `q`、`sender`、`claimed=all|claimed|unclaimed`、`from`、`to`。标题/编号和发件人采用字面子串匹配；时间为带时区的 RFC3339，按创建时间包含起点、排除终点。筛选在数据库执行，分页游标绑定当前筛选、账号及角色。

预览请求：

```json
{
  "accountId": "<账号编号>",
  "items": [{"mailId": "<邮件编号>"}],
  "discardUnclaimed": false,
  "reason": "清理已领取邮件"
}
```

响应 `data` 包含 `receipt`、`items`、`planToken`。提交使用响应中的 `items` 和 `planToken`，其余字段保持一致，并附 `Idempotency-Key`。两次 POST 均使用会话 Cookie 与 `X-CSRF-Token`。

预览签名有效期 10 分钟，绑定账号、角色、邮件版本、丢弃选项、原因和操作者。每封版本由邮箱更新时间与有界邮件字段生成，领取、投递或其他邮箱变更后重新预览。同请求编号及内容返回原回执；响应丢失后网页保留原请求，刷新可重试。回执恢复不依赖已删除邮件或当前在线状态接口。

## 事务与读取

管理事务先锁邮箱父行，再核对角色归属及有效状态，与游戏领取复用同一个邮箱行锁。只读取选中的邮件，每封正文最多 64 KiB、附件最多 100 件，预览不保存游戏数据。

删除定点更新选中邮件的 `deleted_flag/read_flag/gold`，清除选中邮件的附件及附件扩展字段，更新邮箱时间；邮件正文、领取状态、投递元数据和其他邮件保持。删除与 `mailbox_management_operations` 回执同事务提交，任一步失败全部回滚。新表通过网关显式初始化命令创建。

提交前后检查角色在线状态；跨进程编辑锁仍是上线前置项。游戏部分投递事务与领取事务的锁顺序不同，InnoDB 死锁会回滚该次请求，使用同请求编号重试。

## 跨页清理

应用筛选、填写原因后点击“预览跨页清理”。网页分批准备本次名单，显示总数、未领取资产邮件数、金币和附件数；名单可分页核对。确认后分批执行，可暂停、继续或结束任务，历史任务可从“查看清理任务”打开。

| 方法与路径 | 行为 |
| --- | --- |
| `POST /api/v1/characters/{id}/mailbox-cleanups` | 按筛选创建任务及首批名单，使用幂等键 |
| `POST /api/v1/characters/{id}/mailbox-cleanups/{jobId}/prepare` | 按任务版本继续准备最多100封 |
| `POST /api/v1/characters/{id}/mailbox-cleanups/{jobId}/run` | 确认或继续执行最多100封 |
| `POST /api/v1/characters/{id}/mailbox-cleanups/{jobId}/cancel` | 结束剩余工作，保留已处理批次 |
| `GET /api/v1/characters/{id}/mailbox-cleanups` | 分页清理记录 |
| `GET /api/v1/characters/{id}/mailbox-cleanups/{jobId}` | 当前进度及待确认签名 |
| `GET /api/v1/characters/{id}/mailbox-cleanups/{jobId}/items` | 本次名单分页 |

创建字段为 `accountId, filter, discardUnclaimed, reason`。响应 `data.job` 包含 `id/status/revision/total/processed/unclaimed/gold/attachments`；名单准备完成后同时返回 `planToken`。后续写入提交当前 `revision`，首次 `run` 另附 `confirm:true` 与 `planToken`。签名有效期10分钟，绑定完整待确认任务。旧版本请求返回当前进度，便于恢复丢失响应。

状态依次为 `building → ready → running → completed`；主动结束为 `cancelled`，邮箱版本或选中内容发生变化时为 `conflict`。出现变更时保留已完成批次，按当前邮箱重新预览剩余邮件。未领取资产需在创建时明确选择丢弃。

`mailbox_cleanup_jobs` 保存任务与进度，`mailbox_cleanup_items` 保存固定名单及预览摘要。准备阶段只保存管理快照；执行阶段每批邮件变更、删除回执和进度在同一事务提交，回执的 `cleanupId` 关联任务。名单固定后，新邮件保留在邮箱中。

每个请求最多读取或处理100封，页面活跃处理时逐批推进。关闭页面或暂停后，可读取持久进度继续；空闲页面无轮询。网关沿用现有数据库池、邮箱事务和有界单封投影。

## 验证与后续

一次性 MariaDB 覆盖单/双分片、混合批次、100 封上限、原字段保持、并发重放与冲突、保存及回执失败回滚、后置在线回滚、真实领取及锁等待、签名/CSRF/归属与游标隔离。网页验证入口为 `python scripts/verify_gateway_mailbox.py`。

跨页验证入口为 `python scripts/verify_gateway_mailbox_cleanup.py`，覆盖205封跨三批、准备及执行响应丢失后刷新恢复、未领取保护、结束任务、版本冲突、筛选、分页和移动布局。数据库另覆盖快照/进度/回执失败回滚、同版本并发与真实系统投递后的旧预览拒绝。

后续补附件完整实例展示、邮件发放参数与任意收件名单。
