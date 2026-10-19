# GM 邮件、福利与任务接口

独立 GM Web 在网关管理端的 `/console/` 提供账号、角色、物品查询，以及邮件、福利规则和任务回执。界面沿用已确认的 页面布局与主题编辑 配色。

## 操作流程

1. 从账号列表打开角色资料，选择“发送邮件”；从物品目录选择“用于邮件”带入附件。
2. 选择单个角色、最多100人的指定名单或在线角色；指定名单可从账号页添加角色。在线名单在保存预览时采集并固定，最多5000人。填写标题、正文、金币和一种物品；装备数量为1。读取附件参数后设置品级、强化/增幅、锻造及有效天数，保存预览。
3. 在任务详情核对收件人、奖励，点击“确认发放”。每次请求处理最多 10 个目标，页面连续推进至待发名单完成。可点击“暂停发放”；关闭页面或请求中断后，从任务记录“继续发放”。失败项单独重试。
4. 任务回执显示投递状态、实际收件角色和邮件编号。失败项可以显式重试，已成功项保留原回执。

福利规则可选“每账号一次”或“每角色一次”。新规则默认停用，奖励保存后保持固定；相同规则的多次预览、启停和重试共用永久发放回执。账号级预览按角色槽位选择一个有效收件角色，账号内其他角色不会重复获得此规则奖励。角色级沿用创建时间区分删角重建。

新建福利规则同样提供附件参数。规则保存发放选项，每位收件人在实际投递时生成随机品级和指定期限；实际附件及时间进入持久回执，收件记录可查询。默认期限继续由PVF定义。参数、独立版本表及PVF换代状态见 [长期福利参数](GM_BENEFIT_OPTIONS.md)。

启用“自动发给新建角色”时，资格起点为规则首次启用时间；现有角色通过预览名单手动发放。重新启用保留首次启用时间和历史回执。不同规则独立发放。

页面空闲时不轮询。输入查询条件后提交、翻页或刷新才请求数据。邮件和规则草稿保存在当前标签页的 `sessionStorage` 中；重复提交同一份草稿返回同一条记录。“新邮件”开始新的发放请求。

## 全部角色的单次金币福利

在“邮件与福利”点击“全部角色福利”，填写邮件标题与金币数，例如 `20000000`。快捷入口选择“每角色一次”，并勾选“启用后自动发给新建角色”；物品 ID 留空、数量为 0。

保存规则，在规则列表启用，再点击“预览名单”。系统汇总全部有效现有角色，显示人数和每人的发放状态；核对后“确认发放”。同一规则中，每个角色只投递一次，重复预览使用原回执。未来角色从首次启用时间起获得资格，由自动扫描投递。

“全部角色福利”创建一份新规则草稿。要续发已有福利，从原规则或任务记录进入；要发新一轮福利，创建新规则。

## HTTP 契约

沿用 GM 会话 Cookie。`GET /api/v1/capabilities` 返回会话绑定的 `csrfToken`，写请求带 `X-CSRF-Token` 和 `Content-Type: application/json`。Hub 校验浏览器来源并透传 GM 的 CSRF 与幂等请求头。

创建规则和预览使用 `Idempotency-Key`：16–128 位字母、数字、下划线或连字符。同一资源类型下，同一键和内容返回已保存记录；更换内容返回 `409 idempotency_conflict`。此映射与业务记录在同一数据库事务中持久化，跨重启保留。

规则启停和任务推进使用 `revision` 与 `confirm: true`。旧版本返回 `409 revision_conflict`；客户端读取最新记录后决定下一步。响应丢失时，先读取任务；创建或预览可使用原幂等键重试。

| 方法与路径 | 请求 / 响应 |
| --- | --- |
| `POST /api/v1/mail/preview` | `accountId, characterId, title, body, gold, itemId, count`；返回任务摘要 |
| `POST /api/v1/mail/online-preview` | `title, body, gold, itemId, count, options, pvfChecksum`；首次创建时从游戏获取完整在线名单，返回含 `recipientSource` 的任务摘要 |
| `GET /api/v1/items/{itemId}/grant-options` | 当前物品的强化、增幅、锻造、品级、期限能力与PVF摘要 |
| `GET /api/v1/benefits?limit=&cursor=` | 福利规则分页 |
| `POST /api/v1/benefits` | `scope, title, body, gold, itemId, count, autoNewCharacters`；新参数规则另带 `options, pvfChecksum`，返回停用的规则 |
| `GET /api/v1/benefits/{id}` | 单条规则、奖励、版本、首次启用时间 |
| `POST /api/v1/benefits/{id}/state` | `revision, enabled, confirm`；返回新版本 |
| `POST /api/v1/benefits/{id}/preview` | `{}`；冻结现有目标，返回任务摘要 |
| `GET /api/v1/tasks?limit=&cursor=` | 任务摘要分页，按创建时间及 ID 倒序 |
| `GET /api/v1/tasks/{id}` | 状态、版本、奖励、总数、待发、成功、失败计数 |
| `GET /api/v1/tasks/{id}/recipients?limit=&cursor=` | 收件人分页、实际投递角色、邮件回执、失败原因 |
| `POST /api/v1/tasks/{id}/run` | `revision, confirm, retry`；处理最多 10 个目标，返回最新摘要 |

`scope` 为 `account` 或 `character`，身份和物品 ID 为字符串。普通邮件和福利规则均可只发金币；纯金币规则省略 `itemId/options/pvfChecksum`，`count` 为 0，`gold` 大于 0。纯金币规则不依赖物品索引。数量和金币为整数，范围沿用现有邮件兼容约束。标题最多 60 UTF-8 字节，正文最多 240 字节。

指定名单通过 `recipients: [{accountId, characterId}]` 传入，与单角色字段互斥；最多100个不同角色。`options` 支持 `equipmentQuality`（default/random/top）、`reinforce`（0–31）、`amplifyType`（0–4）、`forgingLevel`、`expireDays`（0–3650）。装备类型、增幅初值、锻造上限使用当前PVF。使用名单或参数的附件请求同时带能力接口返回的 `pvfChecksum`；纯金币名单不带物品参数和摘要。普通邮件请求上限32KiB；福利等小表单保留4KiB。

随机品级和到期时间只在首次保存预览时计算，随任务持久化。同一请求编号和内容读回原结果，收件顺序变化视为同一名单；改内容返回冲突。有效天数从预览创建时间开始，0沿用物品默认期限。任务摘要返回附件最终实例属性及到期时间。PVF换代后旧任务可查询和恢复，继续发放前核对PVF一致；前端保留原请求编号，重新选择物品后创建新任务。

普通邮件名单读取仅查询角色基础摘要，实际投递在原账号/邮箱事务内再次核对归属、删除状态和角色创建时间。每次最多投递10人，失败项显式重试。在线发放固定首次预览的会话名单和时间，后续离线仍投递、新上线留到下一次；恢复原预览不重新取名单。“新建在线发放”生成新请求编号，完整契约见 [在线角色邮件](GM_ONLINE_MAIL.md)。长期福利已接[跨PVF重新核对](GM_BENEFIT_PVF.md)，保持原规则及回执；背包直发继续按功能清单实现。

分页默认 20、最大 100；游标绑定资源和页大小。任务列表逐条解码后只保留摘要，收件人另行分页。福利预览当前最多 1000 个注册账号、5000 个有效角色，超限整次返回错误。读取期限 3 秒，写入期限 8 秒；这些接口同步返回 `200` 和当前结果。

任务状态为 `preview`、`running`、`partial` 或 `completed`。预览中全部目标已有回执时直接显示完成。`sent` 表示投递成功；领取状态需要游戏侧独立证据。

响应沿用 `data, page, observedAt, requestId`。常见错误：`400 invalid_body`、`403 csrf_required`、`409 revision_conflict/idempotency_conflict/rule_disabled`、`422 invalid_reward/recipient_changed/preview_unavailable`、`503 operation_unavailable`。数据库细节写入服务日志；浏览器获取明确的输入、目标变化或服务错误。操作日志记录请求编号、路由、资源 ID、版本和投递计数。

## 存储与验证

继续使用现有规则/任务表及账号级隔离表，新增请求摘要和任务版本字段写入 JSON payload。原有规则 ID、邮件事务与永久回执保持。正式切换需包含这些表及账号元数据的备份。

自动验证覆盖单/双分片 MariaDB、同键并发创建、变更内容冲突、跨进程存储重建、账号/角色去重、删除邮件后回执保留、删除角色后投递拒绝、显式失败重试、游标绑定、CSRF、Hub 透传与来源校验，以及真实 Go HTTP 服务上的浏览器操作。运行：

```text
python gateway/scripts/test_mysql.py --gold-benefit-browser-script scripts/verify_gateway_gold_benefit.py
python gm/scripts/test_mysql.py --console-browser-script scripts/verify_console_backend.py
python scripts/verify_console_live.py
python gm/scripts/test_browser.py
```

封禁、过图事件和公开玩家快照继续按 [阶段四接口设计](GM_API_V1_DRAFT.md) 与 [公开快照契约](PUBLIC_PLAYER_SNAPSHOTS.md) 推进。
