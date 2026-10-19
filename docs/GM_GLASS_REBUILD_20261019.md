> 本文保留最初的设计阶段。当前实际接口与能力见 [接入说明](GM_CONSOLE_INTEGRATION_20261019.md)。

# GM 毛玻璃前端重构 — 2026-10-19

当前入口：`gm-console/`，<http://127.0.0.1:8778/>。React 19 + TypeScript + Vite，Node.js 仅用于构建，静态产物可交给外部 GM 服务托管。本地可操作前端已建立；正式 v1 与 Hub 适配待接。当前不部署。

## 本轮决定

2026-10-19 追加：用户认可毛玻璃版布局，品牌改为稍偏粉的 Purple 主题。`gm-console/src/purple.css` 统一兰紫深浅配色、主按钮、选中态及玻璃光晕；布局、圆角、功能分组保持。

用户指定背包编辑器功能、dforigin.online 视觉，并补充允许直接提取 CSS。布局按 GM 工作重排：悬浮功能导航、胶囊顶栏、持续角色身份条、容器页签、主列表与详情编辑区。使用原站深浅色、玻璃/圆角/阴影变量与背景素材；不沿用第一版布局，也不继续套用 86jp 原页面布局。

### 第一版 UI 封存

`gm-web/` 三个位置（main、production-candidate、frontend-candidate）分别保存 ZIP 与逐文件 SHA256，原始文件每份 45 个。源文件/压缩文件逐项核验通过，新增目录级 AGENTS 禁止参考。

归档：主工作区 `local-notes/archive/gm-ui-v1-20261019/manifest.json`。状态 **FROZEN_DO_NOT_REFERENCE**，除非用户另有说明，禁止用其 HTML/CSS/JS/组件/布局/主题做新界面。旧文件留原位用于历史追溯，且没有被新工程导入。Go 领域服务可继续复用。

## 功能取舍

| 一级区 | 首轮工作区 | 本地交互 | 正式接口适配方向 |
| --- | --- | --- | --- |
| 查询 | 账号/角色查询、在线筛选、批量勾选、跳转角色 | 已实现演示 | `GET /api/v1/accounts`、`characters`、账号角色列表；游标分页 |
| 背包 | 八类页签、检索、选择、实例参数、删除 | 已实现演示 | `inventory`、单格详情、`options`、`inventory-operations/preview` 与执行/回执 |
| 邮件 | 写信、附件、金币、收件名单、邮箱、回执 | 已实现演示 | `mail/preview`、`mail/online-preview`、现有持久批次与邮箱操作 |
| 任务 | 章节、状态、检索、批量完成/重置 | 已实现演示 | `characters/{c}/quests`、`quests/{id}`、`quest-operations`；奖励/物品联合事务按本服能力开放 |
| GM | 成长/职业、SP/TP、金币/点券、福利、操作记录 | 已实现演示子集 | 分别接 level/profession/skill-point/gold/currency/benefits；服务端操作边界保留 |
| 账号与角色 | 账号与角色双视图、归属、共享账号收件范围 | 已实现演示 | 账号目录与详情；改名/封禁/迁移/恢复等尚无完整 v1 的操作另列能力，不照搬第三方 SQL |
| 公告与下载源 | 公告编辑、草稿/发布、下载地址、游戏连接 | 已实现演示 | Hub `/admin/api/state`、`PUT /admin/api/news`、`PUT /admin/api/source`；游戏地址映射既有 profile 的准确字段 |

首轮排除：会员充值/兑换、小游戏、广告/关于/发现入口、数据库控制台、任意 SQL、SSH 自定义命令、NPK/PVF 原始资源编辑、二进制槽位导入导出、截图中的暖暖功能。PVF 检索和实例定义继续是后台基础能力，按完整 hash 管理索引。BUG 反馈论坛保留为后续独立模块。

## 八类背包与本服容器

来源：`gateway/internal/modules/dnf/repository/character_resources.go`，不是截图工具的字段名。

| 界面分类 | 本服容器/对象 | 接入要点 |
| --- | --- | --- |
| 物品栏 | `equipment`、`consumables`、`materials`、`quest-items`、`profession`、`quickbar` | 有界分页，保留实际 collection/list/slot；页面分类不改变所有者 |
| 穿戴栏 | `equipped` | Equipment 所有者；不要写成主背包副本 |
| 宠物栏 | `pet-equipment`、`pet-consumables` | 区分宠物装备与消耗品 |
| 仓库 | `warehouse` | 角色个人仓库及容量 |
| 账号仓库 | `account-cargo`；共享资源 `shared` | AccountInventory 所有者；账号范围编辑保护 |
| 宠物 | `pets`，实例关联的已孵化宠物状态 | `v1AttachPetState`；名称/等级等写契约继续补齐 |
| 时装 | `avatars`，实例附加属性 | 本服装扮选项 capability；不复制参考 `user_items` |
| 徽章 | 待核对背包内徽章分类与镶嵌属性 | **不把 `medals` 勋章/守护珠误当成徽章**。已知详情键 `emblem_0..2`；专门写契约仍待补 |

徽章、宠物成长和完整时装属性在此轮用演示样例呈现布局，不能据此记作正式接口已交付。

## 三种数据模型的实际差异

| 来源 | 已核对结构 | 可参考的范围 |
| --- | --- | --- |
| Python 背包编辑器 | `taiwan_cain` / `taiwan_cain_2nd`；`inventory`/`equipslot`/`creature` BLOB；`DnfItemSlot` 61 字节；`account_cargo`、`user_items`、`creature_items`、`postal`/`letter` | 容器分类、实例字段含义、邮件附件目标、账号/角色关系 |
| 86jp 固定版本 | `.csproj` 引用 `Microsoft.Data.Sqlite 10.0.9`；`NewInventoryStore`、`SqliteConnection`、`BeginTransaction`；库存新结构与补充字段 | 参数化操作、所有者解析、同事务维护邮件与附件、按能力生成物品选项 |
| 当前 Go/MariaDB | `mysql_character_resources.go` 按 owner 选择库存/穿戴/账号表，通过 DatabasePlan 路由；有界读取、行锁、定点写、版本/永久回执 | 新前端的正式契约与写入实现；不迁入第三方表名/偏移/事务语法 |

原子操作复用的是领域含义：解析账号与角色 → 读取指定容器/槽位与版本 → 获得可编辑选项 → 预览变化 → 一次用户确认 → 幂等执行 → 显示回执。前端不接数据库。内部预览请求不增加用户步骤。

源码证据：Python `sqlManager2.py` 的 `DnfItemSlot`、`getInventoryAll`、`getCargoAll`、`get_Account_Cargo`、`commit_change_blob`、`send_message`/`send_postal`；86jp 的 `GmService.InventoryConfiguration.cs`、`GmService.Mailbox.cs`、`GmService.Quests.cs`；本服 `api_v1_resources.go`、`api_v1_inventory_operations.go`、`repository/character_resources.go` 与 `repository/mysql/mysql_character_resources.go`。

## 必须保留的操作规则

1. 勾选账号包含该账号全部现有角色；与单独选择的角色合并去重。发送确认框列最终姓名和数量。
2. 配置后确认一次。服务端 preview、幂等 key、连续批次及回执读回由客户端协调，不拆成额外人工步骤。
3. 福利创建并启用后交给后台自动发放；永久去重保持。现有角色固定集合与包含未来角色分别建模。
4. 角色资产操作只检查目标角色；同账号其他在线角色不阻断角色操作。账号仓库保留账号级保护。
5. PVF 全 SHA256 与索引代次一致；hash 变化触发重建索引与规则核对，原批准 PVF 保留。

演示状态由 `src/domain.ts` 模拟前四条可见流程；生产调度、数据库事务、PVF hash 核对继续由现有 Go 服务负责。

## 正式接入后续

- 新版只读目录/详情采用本服 v1 envelope，取消旧请求、游标分页、角色切换隔离。
- 写入按各领域接口分别适配，特别是点券、邮件、福利的契约，不以一个通用 SQL 保存按钮替代。
- 未支持的徽章/宠物/装扮细项先补能力定义与隔离 MariaDB 验证，再接页面；共享账户/角色作用域由服务端校验。
- Hub 登录/CSRF/If-Match 与 GM 会话分别处理，公告和下载源不假定可直接复用 GM 写入 token。
- 继续保留外部 GM/PVF 服务与游戏轻量网关的既有边界。生产发布需要用户另行给出上线指令。

## 验证入口

`gm-console/design-qa.md`：桌面/手机与来源对照。
`npm test`：8 项行为测试；`npm run build`：TypeScript 与生产构建。
浏览器证据：主工作区 `local-notes/gm-glass-20261019/`。
