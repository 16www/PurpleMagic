<div align="center">

# PurpleMagic

![React](https://img.shields.io/badge/GM-React%20%2B%20TypeScript-61DAFB?style=flat-square)
![Flutter](https://img.shields.io/badge/Launcher-Flutter-02569B?style=flat-square)
![Go](https://img.shields.io/badge/Services-Go-00ADD8?style=flat-square)
![.NET 8](https://img.shields.io/badge/Engine-.NET%208-512BD4?style=flat-square)
![MariaDB](https://img.shields.io/badge/Storage-MariaDB-003545?style=flat-square)
[![Stars](https://img.shields.io/github/stars/Cec1c/PurpleMagic?style=flat-square)](https://github.com/Cec1c/PurpleMagic/stargazers)

**面向 DNF90 的 Windows 玩家登录器、更新网关与 GM 管理工具链。**

统一维护源码，分别构建组件；从玩家登录、资源更新到背包、邮件与福利管理。

[English](README.md) ｜ [快速预览](#快速预览) ｜ [网络与接口](docs/architecture/NETWORK_AND_CONTRACTS_20261019.md) ｜ [功能台账](docs/FEATURE_LEDGER.md) ｜ [开发文档](#开发文档)

</div>

## 能做什么

| 场景 | 当前能力 |
| --- | --- |
| 玩家登录 | Windows 注册、登录、客户端目录发现、频道检查与游戏启动；登录器源码对齐 0.2.16 |
| 资源更新 | 配置更新源，校验签名清单和文件摘要，处理下载进度、暂停、安装与回退；登录器程序与游戏内容独立更新 |
| 账号与角色 | 搜索、游标分页、在线筛选、完整账号角色展开，以及批量勾选和收件去重 |
| 物品管理 | 物品栏、穿戴栏、宠物栏、仓库、账号仓库、宠物、时装、徽章八区；查看实例，调整已支持属性、替换同类物品及批量删除 |
| 成长与资产 | 调整等级、职业、觉醒、SP/TP、金币和点券；预览后执行事务并保存永久回执 |
| 游戏任务 | 查询角色任务与定义；管理 API 支持接取、放弃、标记可交、无奖励完成及重置，当前工作台提供完成与重置操作 |
| 邮件与福利 | 选择账号覆盖其全部现有角色；最终名单确认一次后进入持久队列；福利创建并启用后自动发放 |
| 公告与下载 | 工作台通过 Hub 管理公告、更新源和签名游戏连接配置 |
| GM 工作台 | 兰紫毛玻璃深浅主题、悬浮导航、过渡动画、固定位置加载圈，以及居中 Token 入口和可选的 30 天会话记忆 |

[真实接口接入说明](docs/GM_CONSOLE_INTEGRATION_20261019.md)列出各页面与 Go API 的对应关系。宠物成长编辑、完整时装属性、徽章镶嵌、装备附魔和代币券仍待补齐本服写契约。[功能台账](docs/FEATURE_LEDGER.md)继续跟踪封禁治理、通关采集及公开快照等后续工作。

## 服务分工

外部 GMService 承担管理会话、PVF 定义与检索、持久邮件队列和福利调度；游戏侧 PlayerGateway 承担数据库事务、游戏准入检查和永久回执。公开玩家流量与私有管理流量使用独立监听和凭据。

| 组件 | 目录 | 交付形式 |
| --- | --- | --- |
| 玩家登录器 | `launcher/` | Flutter Windows 界面、C# 引擎、便携安装包和自更新组件 |
| 游戏接口网关与 Hub | `gateway/` | `PlayerGateway`：玩家 API、签名发布与私有管理事务 |
| 外部 GM 与 PVF | `gateway/` | `GMService`、离线 `GMIndex`；完整 PVF 索引放在游戏服务外部 |
| GM Console | `gm-console/` | React/TypeScript 静态网页，由 `/console/` 提供 |
| 离线工具与迁移来源 | `gm/` | 原 GM 工具、PVF 工具及兼容契约 |

[网络与接口方案](docs/architecture/NETWORK_AND_CONTRACTS_20261019.md)分别说明游戏 TCP、账号 HTTPS、管理隧道和下载源，并区分现有接口与调度核心、发布器、登录器构建 Worker 的下一版规划。下载仓库后，可在浏览器打开[离线阅读页](docs/architecture/overview-20261019.html)、[网络拓扑图](docs/architecture/network-20261019.html)和[模块分工图](docs/architecture/modules-20261019.html)。

### 读取、修改与恢复

账号、背包、邮箱和任务按页面、容器、游标读取。PVF 以完整 SHA256 标识代次，文件改变后重建并校验索引；游戏网关按需向外部服务请求有界定义。

管理写入使用版本校验、幂等键和永久回执。角色专属修改检查目标角色离线状态，共享资产继续采用账号保护。邮件确认后关闭网页仍继续发放，服务重启后恢复；福利在重试与索引换代时保留已有投递记录。

## 快速预览

使用 Node.js 24 和 npm：

```text
cd gm-console
npm ci
npm run dev
```

打开 **http://127.0.0.1:8778/**，查看使用示例数据的独立视觉预览。生产构建使用真实 API 适配器。本地一次性数据库联调见 [GM Console 接入说明](docs/GM_CONSOLE_INTEGRATION_20261019.md)，服务配置见[外部 GM 运行说明](docs/GM_EXTERNAL_SERVICES.md)。

> [!NOTE]
> `gm-console/` 是当前前端与唯一设计基准。第一版 `gm-web/` 已封存为 `FROZEN_DO_NOT_REFERENCE`，正式打包使用 `gm-console/dist/`。

## 构建与检查

工具链：Node.js 24、Go 1.26.5、Python 3、.NET 8、Flutter Windows；登录器发行构建使用 Visual Studio C++ 与 NativeAOT。

安装前端依赖后，从仓库根目录运行：

```text
npm --prefix gm-console test
npm --prefix gm-console run build
python scripts/package_gm_web.py
python scripts/check_boundaries.py
python scripts/build_gateway.py linux amd64
```

网页包位于 `dist/gm-console/PurpleMagic-GM-Console.zip`。`PlayerGateway`、`GMService`、`GMIndex` 位于 `dist/gateway/linux-amd64/`；将 `linux` 换为 `windows` 可构建 Windows 版本。原 GM 工具使用 `python gm/scripts/build.py linux amd64` 构建。

```text
python scripts/verify_component.py gateway
python scripts/verify_component.py gm
python scripts/verify_component.py launcher --dotnet <dotnet8>
python scripts/verify_component.py flutter --flutter <flutter>
python gateway/scripts/test_mysql.py
python gm/scripts/test_mysql.py
```

数据库检查创建一次性 MariaDB 容器，需要 Docker。登录器使用 `BUILD_FLUTTER.bat --flutter <flutter> --dotnet <dotnet8> --profile-dir <profile-directory> --output <new-output-directory>` 构建；提供[签名 profile 与美术输入](docs/LAUNCHER_INPUTS.md)后，再以 `BUILD_PORTABLE.bat` 包装完整 ZIP。

> [!IMPORTANT]
> 运行配置、签名材料、游戏资源和玩家数据由部署环境提供。发布时核对玩家、游戏服务与 GM 的 EXE/PVF/profile 组合，并按[发行顺序](docs/REPOSITORY_BOUNDARIES.md)完成已发布文件读回。

## 开发文档

| 要了解什么 | 文档 |
| --- | --- |
| 网络、接口与后续模块 | [拓扑与契约](docs/architecture/NETWORK_AND_CONTRACTS_20261019.md) · [架构说明](docs/ARCHITECTURE.md) |
| 新版工作台与设计基准 | [真实接口接入](docs/GM_CONSOLE_INTEGRATION_20261019.md) · [兰紫设计归档](docs/design/purplemagic-orchid-20261019/README.md) |
| 功能与事务 | [功能台账](docs/FEATURE_LEDGER.md) · [基础设施](docs/FOUNDATIONS.md) |
| 管理服务 | [外部 GM/PVF](docs/GM_EXTERNAL_SERVICES.md) · [当前路由](docs/API_CATALOG.md) · [邮件与福利](docs/GM_OPERATIONS_V1.md) |
| 物品与角色数据 | [PVF 物品详情](docs/GM_ITEM_DETAILS.md) · [角色资源](docs/GM_CHARACTER_RESOURCES.md) |
| 公告与公开查询 | [登录器公告](docs/LAUNCHER_ANNOUNCEMENTS.md) · [公开快照设计](docs/PUBLIC_PLAYER_SNAPSHOTS.md) |
| 变更与源码版本 | [更新记录](changlog.md) · [来源](docs/PROVENANCE.md) · [登录器 0.2.16 导入清单](docs/launcher-0.2.16-import.json) |

## 来源与许可

组件沿用原始许可和版权声明，详见[源码来源](docs/PROVENANCE.md)、[GM 声明](gm/NOTICE.md)、[新版前端声明](gm-console/THIRD_PARTY_NOTICES.md)和[历史前端声明](gm-web/THIRD_PARTY_NOTICES.md)。
