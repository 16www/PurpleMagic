<div align="center">

# PurpleMagic

![Flutter](https://img.shields.io/badge/UI-Flutter-02569B?style=flat-square)
![Go](https://img.shields.io/badge/Gateway-Go-00ADD8?style=flat-square)
![.NET 8](https://img.shields.io/badge/Launcher-.NET%208-512BD4?style=flat-square)
![MariaDB](https://img.shields.io/badge/Storage-MariaDB-003545?style=flat-square)

**面向 DNF90 的玩家登录器、更新网关与 GM 管理工具链。**

统一维护源码，分别发行组件；从玩家登录、资源更新到角色与运营管理。

[English](README.md) ｜ [快速预览](#快速预览) ｜ [功能台账](docs/FEATURE_LEDGER.md) ｜ [全局架构](docs/ARCHITECTURE.md) ｜ [开发文档](#开发文档)

</div>

## 能做什么

| 场景 | 当前能力 |
| --- | --- |
| 玩家登录 | Windows 注册、登录、客户端目录发现、频道检查与游戏启动 |
| 资源更新 | 配置更新源，校验签名和文件摘要，显示下载进度，处理暂停、安装与回退 |
| 玩家资产 | 查询账号与角色；调整点券、金币、账号金库和共享资源；查看变更记录 |
| 物品管理 | 分类查看背包、穿戴、仓库及实例详情；调整品级、强化、增幅、锻造、数量和期限；选中批次删除 |
| 角色成长 | 调整等级、职业、觉醒及 SP/TP，预览关联变化并保存操作回执 |
| 游戏任务 | 查询角色任务与任务定义，接取、放弃、标记可交、无奖励完成及重置；支持选中批次处理 |
| 邮件与福利 | 单人、指定名单和在线名单发放；按账号或角色发放一次性福利；查看批次进度与失败重试 |
| 公告与外观 | Hub 管理登录器公告；GM 提供十套配色、自定义强调色、明暗模式及外观偏好保存 |

各项能力的范围、接口与后续工作记录在 [71 项功能台账](docs/FEATURE_LEDGER.md)。副职业验收、物品高级属性、游戏编辑锁、封禁治理、通关采集和每日公开快照是接下来的开发重点。公告编辑目前使用 Hub 页面，独立 GM 页面接入列入后续迭代。

## 一套网关，两个客户端

玩家登录器和 GM Web 共用一套网关设施。网关在同一进程运行玩家 HTTPS 入口、更新 Hub 和私有管理 API，并连接现有游戏服务与 MariaDB。玩家入口和管理入口各有监听与鉴权配置。

| 组件 | 目录 | 交付形式 |
| --- | --- | --- |
| 玩家登录器 | `launcher/` | Flutter Windows 界面、C# 引擎、安装包与自更新组件 |
| 网关与管理 Hub | `gateway/` | Linux / Windows 可执行文件；包含管理领域服务与数据库事务 |
| GM Web | `gm-web/` | 独立静态网页包，由管理监听提供 `/console/` |
| 离线工具与迁移来源 | `gm/` | 原 GM 实现、PVF 索引工具及兼容契约 |

[查看交互式架构图](docs/architecture/current.html) · [查看仓库与发行边界](docs/REPOSITORY_BOUNDARIES.md)

### 按需读取与可恢复操作

账号、背包、邮箱和游戏任务按页面、容器和游标读取。PVF 按完整摘要建立磁盘索引，物品详情与任务定义按需加载；职业索引只缓存当前选中的职业数据。更新 PVF 后重新生成对应索引，福利规则通过差异预览延续已有发放记录。

管理操作经领域事务写入，结合版本校验、幂等请求和永久回执处理重复提交与响应丢失。邮件、福利和跨页邮箱清理保存批次进度，支持继续执行与结果追踪。具体调用链见[基础设施说明](docs/FOUNDATIONS.md)。

## 快速预览

安装 Python 3 后，在仓库根目录运行：

```text
python scripts/preview_console.py --port 8765
```

打开 **http://127.0.0.1:8765/**，使用示例数据体验账号检索、物品选择、邮件草稿、导航和主题调色。按 `Ctrl+C` 结束预览。页面交互与数据范围见[预览说明](docs/CONSOLE_PREVIEW.md)。

接入实际管理 API 时，准备网关配置、MariaDB、游戏私有接口及匹配的 PVF 索引，按[单网关接入说明](docs/GM_GATEWAY_IMPLEMENTATION.md)启动服务。管理网页使用登录 Key 建立会话。

## 构建与检查

工具链：Go 1.26.5、Python 3、.NET 8、Flutter Windows；登录器发行构建使用 Visual Studio C++ 与 NativeAOT。

```text
python scripts/check_boundaries.py
python scripts/build_gateway.py linux amd64
python scripts/package_gm_web.py
```

网关产物位于 `dist/gateway/linux-amd64/`，网页包位于 `dist/gm-web/GM-Web.zip`。将 `linux` 换成 `windows` 可构建 Windows 网关。原 GM 工具使用 `python gm/scripts/build.py linux amd64` 构建。

```text
python scripts/verify_component.py gateway
python scripts/verify_component.py gm
python scripts/verify_component.py launcher --dotnet <dotnet8>
python scripts/verify_component.py flutter --flutter <flutter>
```

登录器分发包使用 `BUILD_FLUTTER.bat --flutter <flutter> --dotnet <dotnet8> --profile-dir <profile-directory> --output <new-output-directory>`。准备[签名 profile 与图片输入](docs/LAUNCHER_INPUTS.md)后构建，再用 `BUILD_PORTABLE.bat` 包装完整 ZIP。

> [!IMPORTANT]
> 运行配置、签名材料、游戏资源和玩家数据由部署环境提供。发布时核对玩家、游戏服务与 GM 使用的 EXE/PVF/profile 组合，并按[仓库边界与发布顺序](docs/REPOSITORY_BOUNDARIES.md)完成读回验证。

## 开发文档

| 要了解什么 | 文档 |
| --- | --- |
| 功能范围、进度与页面重构 | [功能台账](docs/FEATURE_LEDGER.md) · [前端分区](docs/FRONTEND_PLAN.md) |
| 服务组成、缓存与事务 | [架构说明](docs/ARCHITECTURE.md) · [15 类基础设施](docs/FOUNDATIONS.md) |
| 管理接口与操作契约 | [当前路由目录](docs/API_CATALOG.md) · [邮件、福利与作业](docs/GM_OPERATIONS_V1.md) |
| 物品描述与索引准备 | [PVF 物品详情](docs/GM_ITEM_DETAILS.md) · [角色资源](docs/GM_CHARACTER_RESOURCES.md) |
| 公告和公开查询 | [登录器公告](docs/LAUNCHER_ANNOUNCEMENTS.md) · [玩家公开快照设计](docs/PUBLIC_PLAYER_SNAPSHOTS.md) |
| 版本与变更 | [更新记录](changlog.md) · [来源与兼容基线](docs/PROVENANCE.md) |

## 来源与许可

各组件沿用原始许可和版权声明，详见[源码来源](docs/PROVENANCE.md)、[GM 声明](gm/NOTICE.md)和[前端第三方声明](gm-web/THIRD_PARTY_NOTICES.md)。
