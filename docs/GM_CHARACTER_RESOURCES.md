# 角色资源查询与 PVF 换代

在账号与角色页打开角色资料，选择“背包与仓库”“邮箱”“游戏任务”或“任务库”。打开页签、切换容器、分页和主动刷新时发送请求；列表显示读取时刻，选中记录后再读详情。切换角色或关闭资料会取消在途请求并清除旧结果。

## 容器与数据来源

| 范围 | 容器 ID | 数据归属 |
| --- | --- | --- |
| 角色背包 | equipment、consumables、materials、quest-items、profession、quickbar | 角色 Inventory 的 slots |
| 装扮与宠物 | avatars、pets、pet-equipment、pet-consumables | 角色 Inventory 的 slots |
| 勋章与守护珠 | medals | 角色 Inventory 的 slots |
| 身上穿戴 | equipped | 角色 Equipment |
| 个人仓库 | warehouse | 角色 Inventory 的 warehouse |
| 账号金库 | account-cargo | 所属账号 AccountInventory |
| 晶块与灵魂 | shared | 所属账号 AccountInventory |

列表使用数据库槽位游标和 LIMIT，默认 20、最多 100 条。列表只读基础字段；实例详情在只读事务中读取当前行及附加字段。账号共享内容按当前角色的权威所属账号取数。

实例展示真实数量、绑定、期限、品级种子、强化、增幅、锻造和耐久。已验证的 119 字节实例格式使用对应解析器；其他长度仅展示有明确存储键的字段。未知字段返回 null，实际 0 保留为 0。附魔与徽章目前读取已知附加键，完整实例结构继续适配。实例 revision 覆盖原记录，静态物品详情通过物品 ID 链接。

邮箱列表读取标题、发件人、金币、领取状态和时间；单封详情再取正文与附件。角色任务在 SQL 中按 active/completed/all 分页，states 记录优先于同 ID 的 progress。列表保留进度及状态更新时间，任务详情当前展示静态 PVF 定义；当前角色可接资格及任务修改入口待接。

## v1 接口

所有路径使用 `/api/v1` 前缀及 GM 会话，沿用统一响应、请求编号、3 秒读取期限与错误格式。

| GET 路径 | 返回内容 |
| --- | --- |
| `/characters/{id}/inventory?container=&limit=&cursor=` | 容器、槽位摘要、所属账号、PVF 摘要 |
| `/characters/{id}/inventory/{container}/{slot}` | 单格实例、revision、已知属性与 PVF 摘要 |
| `/characters/{id}/mailbox?limit=&cursor=` | 邮箱摘要分页 |
| `/characters/{id}/mailbox/{mailId}` | 单封正文、金币、附件与领取状态 |
| `/characters/{id}/quests?state=&limit=&cursor=` | 游戏任务状态、进度与当前定义摘要 |
| `/quests?q=&kind=&limit=&cursor=` | 名称/ID 搜索、PVF grade 分类及分页 |
| `/quests/{questId}` | 说明、职业/等级条件、前置分组、检查数量与奖励 |

身份 ID、数量和大整数使用字符串。无记录返回空列表；数据库或必需索引不可用返回 503。新 PVF 删除的玩家物品/任务保留原 ID 和存储值，definitionKnown=false。旧 PVF 游标返回 400，刷新后从新代次首页读取。

## 离线任务索引

`quests-v1-<完整PVF SHA256>.bin` 与 itemIndexPath 放在同一目录。文件包含版本、PVF 身份、摘要清单和逐条压缩的任务定义。常驻内存保留简要清单；详情按 ID 定位、校验后有界解压。重复构建先逐条核对已有文件，匹配则复用。

生成流程先写临时文件，完成全部校验和 Sync 后更名。失败或取消保留上一代文件。新版任务查询不在 HTTP 请求中构建完整目录；旧任务写操作仍使用原任务目录加载路径。

从 gm 目录执行，Linux 使用相应 dist/linux-amd64/DNF90GM：

```text
dist/windows-amd64/DNF90GM.exe --config runtime/gm-next.json --build-pvf-index
dist/windows-amd64/DNF90GM.exe --config runtime/gm-next.json --build-item-details
dist/windows-amd64/DNF90GM.exe --config runtime/gm-next.json --build-quest-index
```

三条命令均在打开数据库和 HTTP 监听前退出。已有匹配的 PVF 目录时跳过第一条。

## PVF 更新流程

1. 将批准的新 PVF 放到独立资产目录，核对完整 SHA256。
2. 准备该代次 GM 配置：pvfPath、pvfSha256、独立 pvfIndexPath 和 itemIndexPath。保留旧配置及索引供回退。
3. 离线生成三层物品索引和任务索引，核对数量、失败条目及抽样详情。同 ID 的新内容也按新摘要建索引。
4. 切换 GM 配置并重开服务，使归档文件和内存摘要一起切换。当前不提供归档热替换。
5. 读取响应中的 pvfChecksum，核对新定义、旧游标拒绝、缺失 ID 与账号共享资源，再记录实际进程摘要及内存指标。

资源发布与玩家兼容 profile 由统一发布流程控制；此流程只描述 GM 读取和索引准备。

## 验证

隔离 MariaDB 覆盖单/双分片、跨账号隔离、27 格分页、大整数、未知值、状态优先级和取消。真实 Go HTTP 浏览器覆盖页签、详情、过期响应、文本转义、移动布局及空闲无轮询。合成 PVF 回归覆盖同 ID 内容变化、摘要和版本不匹配、损坏重建、取消及复用。

V106 PVF 的离线验证生成 154,729 件物品和 3,091 条任务；全部任务逐条读回，物品进行分布查找及原文抽样对照，重复生成均复用。结果位于忽略目录 runtime/verification/pvf-v106。线上 GM 内存及实际玩家数据验收在部署阶段执行。
