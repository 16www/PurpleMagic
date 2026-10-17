# 角色 SP / TP 管理

角色资料的“SP / TP”页签展示当前等级的 PVF 基础、额外点数、总量、已消耗和剩余点数，支持签名预览、提交及分页记录。页面打开和手动刷新时读取；空闲时不轮询。

## 操作与计算

| action | target | amount | 行为 |
| --- | --- | --- | --- |
| `add` / `subtract` | `sp` / `tp` | 正整数字符串 | 增加或扣减所选额外点数 |
| `set` | `sp` / `tp` | 非负整数字符串 | 设置所选额外点数 |
| `clear_remaining` | `sp` / `tp` | `"0"` | 所选剩余点数归零，已学技能保持 |
| `sync` | `both` | `"0"` | 按当前等级及 PVF 重算两种点数 |

总量 = PVF 累计基础 + 角色额外点数。已消耗 = 原总量 − 原剩余；新剩余 = 新总量 − 已消耗。每次操作均重算 SP 和 TP，预览展示两者变化。额外点数及总量范围为 0–65535；新总量小于已消耗点数时拒绝。剩余归零会将所选剩余计入已消耗，后续重算保持这一结果。

两种技能页面共用角色的一份点数账本。缺少账本、负数或超出范围的存档返回明确错误。等级、职业、技能等级、快捷栏与冷却沿用现有存档。

## API

| 方法与路径（前缀 `/api/v1`） | 内容 |
| --- | --- |
| `GET /characters/{id}/skill-points` | 点数、等级、PVF 基础及两个 revision |
| `POST /characters/{id}/skill-point-operations/preview` | 变更前后值与有效十分钟的 `planToken` |
| `POST /characters/{id}/skill-point-operations` | 原子提交并返回永久回执 |
| `GET /characters/{id}/skill-point-operations?limit=10&cursor=...` | 独立分页记录 |

预览请求：

```json
{"accountId":"<账号ID>","action":"add","target":"sp","amount":"100","revision":"<角色与账本版本>","definitionRevision":"<PVF定义版本>","reason":"活动补偿"}
```

提交使用原请求加 `planToken`，并携带会话、`X-CSRF-Token` 和 `Idempotency-Key`。预览签名绑定账号、角色、操作、点数、原因、操作者及两个版本。分页游标绑定账号、角色和分页条件。

网页在当前标签页保存待取得回执的请求，刷新后沿用原编号恢复。已提交的相同请求优先返回永久回执，恢复时无需 PVF 或游戏在线接口；同编号不同内容返回冲突。

## 存储与 PVF

读取采用有界一致性投影：角色身份、等级、删除标记、额外点数、管理序号、时间以及技能账本五个数值和时间。写入按角色行 → 技能行取得锁，再读取附属字段，兼容游戏只锁技能的保存事务。

只更新 `characters.bonus_sp/bonus_tp/updated_at`、技能点数及时间和 `gm_skill_points_sequence_v1`。旧镜像兼容模式按既有规则读补充表并同步其中已存在的额外点数；默认以角色列为权威。金币、负数哨兵、其他属性、技能等级、快捷栏和冷却保持。额外点数、技能账本、序号及回执同事务提交。

回执表为 `<前缀>_skill_point_management_operations`。升级时执行已有 `--init-management-schema`；正常启动检查表结构。

PVF 只读取磁盘索引中的 `Etc/spTable.etc`，复用游戏累计算法，按完整 PVF SHA256 缓存小表。定义版本含摘要、解析版本、等级和累计 SP/TP；PVF 换版后旧预览拒绝，新读取重新解析对应代次。索引构建和配置切换沿用项目批准的 PVF 更新流程。

## 验证与运行接入

`TestV1SkillPointsMySQL` 覆盖单/双分片、权威列及旧镜像、幂等与版本并发、失败回滚、原字段保持、真实技能事务锁等待、签名与游标、PVF 换版和回执恢复。`scripts/verify_gateway_skill_points.py` 在真实网关与一次性数据库上验证网页操作、恢复、在线拒绝、主题和手机布局。

提交目前在事务前后核对角色离线。上线前仍需接入游戏侧跨进程编辑锁，以覆盖检查结束后重新登录的竞争窗口。
