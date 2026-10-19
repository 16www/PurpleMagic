# 单网关与独立 GM Web

`gateway/cmd/playergateway` 现在可在同一个进程运行玩家入口、更新 Hub 和 GM 管理 API。`gm-web/` 是独立静态客户端，由管理监听挂载在 `/console/`。原 `gm/` 保留为离线索引工具和迁移来源。

## 当前操作

- 账号编辑保护与游戏登录共享数据库行锁；提交前退出账号的游戏连接，详见 [编辑并发保护](GM_EDIT_CONCURRENCY.md)。
- 副职业支持四种类型、等级与精确经验、切换及取消、PVF任务调整和永久回执。

- 角色页“转职与觉醒”已接职业/分支/觉醒选择、技能与任务变更预览、原子提交和永久记录；职业定义按PVF摘要离线索引，运行时只保留所选职业，见 [职业管理](GM_PROFESSION_OPERATIONS.md)。

- 账号、角色、15 类容器、邮箱、游戏任务、物品和任务目录沿用版本化分页接口。
- 邮件预览与发放、每账号一次／每角色一次福利及运营作业使用既有持久回执。
- 长期福利可配置附件品级、强化/增幅、锻造和期限；按每位收件人的实际投递时间生成实例并保存完整回执，见 [福利参数](GM_BENEFIT_OPTIONS.md)。PVF 换版可核对并确认新物品定义，保留原规则与发放回执，见 [版本核对](GM_BENEFIT_PVF.md)。
- 普通邮件支持单角色、指定名单及在线角色范围。在线名单通过游戏私有快照接口获取、首次预览固定，详见 [在线角色邮件](GM_ONLINE_MAIL.md)。
- 角色邮箱支持筛选、单封/选中批次删除、跨页固定名单清理、未领取资产确认、签名预览和分页记录；分批持久进度支持中断恢复，见 [邮箱操作](GM_MAILBOX_OPERATIONS.md)。
- “账号与角色 → 打开账号 → 点券与流水”支持点券增加、扣减、变更确认和分页流水。查询与写入使用账号钱包 `account_cera`。
- 角色页“金币与流水”支持增加、扣减及设置余额，签名预览、版本校验、原子回执与分页记录；只更新金币和管理版本，见 [金币操作](GM_GOLD_OPERATIONS.md)。
- 角色页“SP / TP”支持额外点数增减/设置、剩余归零与按PVF重算，保留已消耗点数；额外值、技能账本与回执同事务，见 [技能点管理](GM_SKILL_POINTS.md)。
- 角色页“等级与成长”支持1–90级调整，按PVF累计经验、技能点和24项基础属性原子重算，保留已消耗点数及技能存档；见 [等级管理](GM_LEVEL_OPERATIONS.md)。
- 角色页“账号金库”支持开通、按 PVF 档位扩容、金币增减/设置与账号共享流水，见 [账号金库操作](GM_VAULT_OPERATIONS.md)。
- 角色页“容量管理”支持主背包/装扮栏/个人仓库单项或批次扩容、签名预览与独立流水，保留其他容器状态；见 [容量管理](GM_CONTAINER_OPERATIONS.md)。
- 角色页支持任务接取、放弃、可交、无奖励完成、恢复未完成及链路重置，提供批次预览与分页回执；接口和范围见 [任务操作](GM_QUEST_OPERATIONS.md)。
- 背包、个人仓库、账号金库及普通穿戴支持逐格/批量配置和删除；晶块/灵魂支持从零发放、改量及清空。按资产归属使用角色或账号事务与流水，保留 PVF 能力、固定随机预览和实例版本；见 [背包操作](GM_INVENTORY_OPERATIONS.md)。
- 已孵化宠物支持成长详情及背包/穿戴关联删除，预览显示关联影响，物品、成长存档与出战指针同事务保存；宠物装备支持背包/穿戴删除及有期限实例的期限调整，穿戴和颜色存档共同保存；宠物蛋和成长编辑继续适配。
- 登录支持密码或随机 Key 的 SHA256 摘要，网页可导入 `{"version":1,"key":"..."}` 或纯文本 Key 文件。Key 内容只发送到当前管理站点，不写入浏览器持久设置。
- 公告仍使用 Hub 原有认证与发布接口。首版使用单管理员 Key，多管理员分权后续接入。

其余管理动作继续按 [功能清单](GM_FEATURE_MATRIX.md) 接入。当前流水包括 GM 点券增减、角色金币、扩容、账号金库、任务和物品操作；游戏商城消费、其他币种及充值换算另行接入。

## 点券 API

| 方法与路径 | 内容 |
| --- | --- |
| `GET /api/v1/accounts/{id}/currency` | 字符串余额、币种和 revision |
| `POST /api/v1/accounts/{id}/currency-adjustments` | 点券增减与事务回执 |
| `GET /api/v1/accounts/{id}/currency-adjustments?limit=10&cursor=...` | 该账号的分页 GM 点券流水 |

写入示例：

```json
{"currency":"cera","amount":"250","revision":"<读取到的版本>","reason":"活动补偿"}
```

使用会话 Cookie、`X-CSRF-Token` 与 `Idempotency-Key`。数量使用十进制整数字符串，增加为正数、扣减为负数。行锁内重新校验版本、余额与溢出，余额和永久回执在同一事务提交。相同请求编号及内容返回原回执；不同内容返回 409。流水写入失败时余额回滚。异常存储余额返回 503。

网页在当前标签页保留尚未取得回执的请求编号。响应丢失或页面刷新后，“重试并取得回执”沿用原请求；收到明确拒绝后刷新余额并重新填写。点券刷新仍沿用游戏既有的进场／重新打开商城读取链路。

## 安装配置

配置示例见 [gateway-management.example.json](../deploy/templates/gateway-management.example.json)。管理监听绑定明确的私网或 loopback 地址，使用 TLS。玩家和管理监听独立；Hub 与 GM 共用同一管理监听。可只配置 `management` 启动管理服务，也可保留原玩家与 Hub 配置。

数据库角色由安装者按实际分片授予所需权限。安装阶段显式执行：

```text
PlayerGateway --config gateway.json --init-management-schema
```

该命令只初始化福利表和管理回执表，可重复执行；参数福利使用新增的 `_benefit_v2_campaigns` 和 `_benefit_v2_jobs` 表，以及核对历史 `_benefit_pvf_ops` 表，升级时同样执行此初始化步骤。常规启动检查已有表，不自动迁移游戏表。相同 DSN 复用连接池，每池最多 8 个连接、2 个空闲连接。

推荐在 Linux 服务环境设置 `GOMEMLIMIT=128MiB`，作为 Go 运行时软内存目标。配置与 Key 文件、TLS 私钥留在受保护目录。GM Web 独立打包，网关生产二进制不嵌入页面；默认同源提供页面，后续其他客户端直接调用业务 API。

## PVF 与资源占用

PVF 仍按完整摘要匹配离线磁盘目录、物品详情和任务索引。更新批准 PVF 后，用 `gm/` 的离线索引命令生成同代次文件，再切换配置；步骤见 [物品详情](GM_ITEM_DETAILS.md) 与 [角色资源](GM_CHARACTER_RESOURCES.md)。启动不在线构建索引；物品摘要缺失时目录暂不可用，账号查询继续工作。

网关用流式 JSON 读取物品摘要，并复用重复的分类与职业字符串。完整 PVF、完整物品描述和任务正文按磁盘记录读取。下一轮继续将常驻物品摘要改成按需目录，并提供客户端可缓存分片。

2026-10-17 本地 Linux amd64 测量使用真实 V106 PVF、154,729 件物品和示例玩家。早期默认 GC 对照的 RSS 从约 325 MiB 降至 267 MiB；本轮金币版本设置 `GOMEMLIMIT=128MiB`，完成含角色金币/流水、宠物详情和操作能力的 11 类读取后，RSS 约 131 MiB、进程峰值约 153 MiB，swap 为 0。数据库位于另一个容器，未施加并发负载。

邮箱跨页版本在同一条件下完成15类读取，包含筛选邮箱、清理任务、删除记录与PVF金库定义；RSS为131.93MiB、进程峰值153.02MiB、swap为0。此测量为单实例读取负载。

技能点版本在同一条件下完成17类读取，新增SP/TP投影和记录；RSS为133.73MiB、进程峰值155.54MiB、swap为0。PVF技能点小表按摘要复用，数据库仍位于独立容器。

等级版本在同一条件下完成19类读取，新增成长投影和记录；RSS为136.37MiB、进程峰值154.45MiB、swap为0。包含按PVF摘要缓存的经验/点数及职业成长小表。

当前批准PVF的账号编辑保护候选完成23类读取（含副职业及记录），Linux RSS为138.57MiB、进程峰值153.67MiB、swap为0；设置`GOMEMLIMIT=128MiB`，数据库独立容器，使用示例玩家且未施加并发负载。

## 验证与预览

```text
python scripts/verify_component.py gateway
python gateway/scripts/test_mysql.py
python scripts/check_boundaries.py
python scripts/build_gateway.py windows
python scripts/build_gateway.py linux
```

`scripts/preview_gateway_live.py` 创建一次性 MariaDB，装入示例玩家与指定 PVF 索引，生成本地 TLS 和登录 Key，启动实际网关二进制。访问地址和 Key 路径写入 `runtime/preview/gateway-live/ready.json`。网页保留一次“本地预览 · 示例玩家”标识。`--stop` 停止该预览并清理其数据库。

`scripts/verify_gateway_containers.py` 验证多项扩容、回执恢复、容量/物品保持和并发冲突；`scripts/verify_gateway_gold.py` 验证角色金币增减/设置、签名预览、响应恢复和多管理员冲突；`scripts/verify_gateway_currency.py` 验证 Key 登录、增减、响应丢失后的重试、账号隔离、大整数、移动端和主题；`scripts/verify_gateway_quests.py` 验证任务变更及回执恢复；`scripts/verify_gateway_inventory.py` 验证物品编辑、数量、删除及回执恢复。预览启动器为八个示例角色提供认证的运行时状态：原六个离线，流光账号的两个角色在线。`scripts/verify_gateway_online_mail.py` 验证固定在线名单、丢失预览恢复、确认投递及新建名单；`scripts/verify_gateway_mail_dispatch.py` 验证指定名单与附件参数。`scripts/verify_gateway_linux.py --gomemlimit 128MiB` 在本地一次性预览网络验证 Linux 读取与资源占用。预览配置及回执留在 ignored runtime 中。

## 账号金库操作

独立 Web 已接账号金库开通、分档扩容、金币增减/设置及账号统一流水。容量和金币可在同一签名预览中提交，只读取四个相关元数据键，定点写入与永久回执同事务保存。相同账号角色可接续查看和恢复原请求；其他角色在线时拒绝提交。接口、容量/金额契约和验证见 [账号金库操作](GM_VAULT_OPERATIONS.md)。
