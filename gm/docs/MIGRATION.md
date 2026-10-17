# 从服务端仓库迁移 GM

## 一体化候选的磁盘 PVF 索引

此候选启动前需生成匹配 PVF 的磁盘目录索引。保留原配置与状态文件，用新二进制
运行 `--config <config> --build-pvf-index`，待命令成功退出后再安排服务切换。
该命令不连接数据库、不启动福利扫描，不覆盖已存在的索引。`pvfIndexPath` 可显式指定；
省略时使用 `itemIndexPath` 加 `.pvfidx`。PVF 更新须使用新索引路径，并同步配置 SHA256。
详细命令见 [DEPLOYMENT.md](DEPLOYMENT.md)。现有物品 JSON 缓存、政策、任务与回执继续使用。
回退只恢复旧二进制与旧配置，不回退玩家数据库；新增磁盘索引可留存。

## 一体化候选的福利范围升级

备份注册库、游戏库及配置后，在服务切换时停止旧 GM，用新版 `--init-schema` 初始化
`<accountTable>_benefit_acct_jobs` 和 `_benefit_acct_campaigns`。该命令还会启动服务并恢复
已有启用规则扫描，需按正式切换流程执行。旧规则、历史批次、signup 快照和回执不搬表、不清空。

账号级规则使用独立表，旧 GM 不会将它们误读成每角色一次。回退旧二进制与配置后，
账号级规则暂停处理，角色级规则继续按原行为工作；保留新增表和所有回执，再升级可继续。
不要把新表内容复制到旧福利表，也不要回退玩家数据库覆盖新进度。表升级属于候选实现，
上线前仍需核对目标数据库权限、备份完整性与实际运行版本。

## 物品详情索引升级

物品摘要格式升为 6，适用职业保留 PVF 中的全部条目。运行新版
`--build-item-details` 可离线准备新摘要及详情文件；相同 PVF 摘要和解析版本复用已验证的文件。
生成的 `item-details-v3-<sha256>.bin` 与 `itemIndexPath` 放在同一目录，按当前运行用户设置读取权限。
PVF 更新时同步匹配的目录索引、详情索引和配置摘要，旧一代文件保留供旧配置回退。
本次索引升级只涉及派生资产文件，步骤见一体化项目 [物品详情](../../docs/GM_ITEM_DETAILS.md)。

## 原独立 GM 拆仓迁移说明

本次是源码与发布边界迁移。现有 GM 进程、接口路径、配置格式和数据格式保持兼容；
清理服务端 Git 仓库不会自动停止正在运行的 GM。

1. 记录旧 GM 镜像/二进制摘要、实际配置路径、端口、运行用户、网络和挂载目录。
   备份配置、GM 状态目录以及注册库和游戏库；不要把备份或凭据提交 Git。
2. 从新仓库构建或下载 GM。沿用原数据库映射、PVF SHA256、管理密码和 gameToken。
   用 `--check` 核对配置格式；该命令不证明数据库/API 连通。
3. 保留原政策表、注册额度快照、批次、规则 ID、账号/邮件回执与角色创建时间。
   不清空表、不新建同内容规则替代旧规则、不恢复旧数据库覆盖玩家进度。
4. 如果旧 GM 由游戏的 Compose 项目管理，先准备新 GM 专用项目并核对端口和网络；
   在切换时只停止旧 GM，再启动新 GM。旧私有 bridge 网络可作为 external network
   继续使用，或采用本仓 Linux host 网络模板并按真实地址调整。不要启动两个争用
   相同端口的实例，也不要对旧游戏项目运行 `down` 或 `--remove-orphans`。
5. GM 已迁出后，才从游戏 instance.json 移除 `gm_*` 设置；保留
   `player_admin_secret`，其值必须与 GM `gameToken` 一致。
   Windows 旧 `server.gmListen` 也需移除。新版游戏生成器会在写入前拒绝未迁移配置。
6. 验证 GM 登录、状态、物品查询、规则列表、预览和只读在线/API 查询；迁移验证
   不向真实玩家发奖。已有启用规则会继续正常扫描。记录实际运行镜像/二进制摘要。

需要回退时只恢复旧 GM 二进制/镜像和匹配配置，继续使用当前数据库与发放回执。
现有管理 Hub 可以继续代理同一 GM 地址，外层 token 和记住登录机制无需重置。

The migration preserves existing data and receipts. Repository cleanup is not
a deployment. Cut over only GM, retain current databases, and never recreate
campaigns as a substitute for migrating their original IDs and history.
