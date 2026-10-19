## 2026-10-19 网络与接口重整方案

新增[兰紫阅读入口](architecture/overview-20261019.html)、[目标网络图](architecture/network-20261019.html)、[模块分工图](architecture/modules-20261019.html)及[完整网络/API/生成器契约](architecture/NETWORK_AND_CONTRACTS_20261019.md)。包括通用部署端口与双向 SSH 示例、控制核心/PVF/发布器/Windows 构建节点分工、更新源字段、当前与拟新增接口。此次仅交付方案。

本次源码同步已纳入登录器 0.2.16，详见 `launcher-0.2.16-import.json`。

<!-- orchid-live-20261019 -->
## 2026-10-19 兰紫 GM Console 真实接入

新客户端 `gm-console/` 已接同源 Go v1 与 Hub，覆盖查询、八类背包、邮件、任务、成长/资产、自动福利、领域流水和公告/下载源/游戏连接。现有本服能力范围与余项见 [本轮接入与验收](GM_CONSOLE_INTEGRATION_20261019.md)。第一版 `gm-web/` 已封存，新打包入口使用 `gm-console/dist/`。新版已完成隔离数据库和浏览器接入验证；下文历史阶段描述保留作追溯。

<!-- 2026-10-18 -->
外部 GM／PVF、持久邮件队列与福利调度已接入，见[服务分工与运行](GM_EXTERNAL_SERVICES.md)。下文同时保留本地一体模式。

# PurpleMagic：当前架构

2026-10-19 源码快照。PurpleMagic 将 Flutter 登录器、Linux 网关、更新 Hub 和独立 GM Console 放在同一个仓库，分别构建、发行。游戏主机运行一个工具网关，外部主机运行 GM 服务；游戏服务和 MariaDB 是外部运行依赖。

[交互式全局架构图](architecture/current.html) · [功能台账](FEATURE_LEDGER.md) · [基础设施与调用链](FOUNDATIONS.md) · [实际路由](API_CATALOG.md) · [前端重构分区](FRONTEND_PLAN.md) · [仓库边界](REPOSITORY_BOUNDARIES.md)

## 当前组成

| 组件 | 代码位置 | 当前职责 |
| --- | --- | --- |
| Windows 登录器 | `launcher/Flutter`、`Host`、`Engine`、`Core` | Flutter 界面；Host 通过标准输入输出的逐行 JSON RPC 调用 C# 引擎；处理注册、登录、更新、目录发现、客户端检查和启动 |
| GM Console | `gm-console/` | React/TypeScript/Vite构建为独立静态资源；兰紫玻璃布局与主题编辑；调用私有管理接口。第一版`gm-web/`已封存。 |
| 玩家监听 | `gateway/cmd/playergateway/main.go` | 白名单代理登录/注册/状态，分发登录器更新；注入可信来源信息 |
| 管理监听 | 同一进程的 `privateHandler` | `/console/` 与 `/api/v1/` 交给管理模块；`/admin/` 交给 Hub |
| 管理模块 | `gateway/internal/management/` | 数据库连接池、类型化事务与回执；外部模式按需获取定义 |
| 外部 GM | `gateway/cmd/gmservice/` | 管理会话、GM Web、PVF与索引、静态检索、邮件队列及福利调度 |
| GM API 与领域 | `gateway/internal/services/gmtool/`、`internal/modules/dnf/adminops/` | 有界读取、预览、操作、事务回执；业务约束与事务放在领域/仓储 |
| 更新 Hub | `gateway/internal/services/launcherhub/` | 配置内容源、检查版本、签名发布、公告、安装包缓存 |
| PVF 离线工具 | `gateway/cmd/gmindex/`、`gm/cmd/` 的索引工具 | 按批准 PVF 的完整摘要生成、核对磁盘索引 |
| 游戏适配器 | `gateway/internal/modules/dnf/adminapi/` | 调用配置固定的游戏私有 `/api/game/admin`，查询在线状态、会话快照、目录等运行态信息 |
| 外部存储与游戏服务 | 由游戏项目维护 | MariaDB 玩家存档和权威玩法结算；PurpleMagic 不包含游戏监听与服务启动入口 |

两个 HTTPS 监听使用不同地址与管理凭据。GM Web 可独立打包，目前由私有监听以同源 `/console/` 提供，鉴权仍要求同源。跨域托管、PWA 安装和 Android 原生端属于后续客户端工作。

## 三条主要调用链

**玩家登录与更新**：Flutter → Host JSON RPC → Engine/Core → 玩家 HTTPS → 游戏账号 API 或 Hub。Hub 从已配置源读取元数据/资源，按签名和文件摘要提供更新；客户端执行文件事务、安装与回退。游戏连接由客户端走游戏频道。

**GM 查询与修改**：GM Web → 私有 HTTPS → v1 API → 领域服务 → 仓储投影/事务 → MariaDB。读请求按账号、角色、容器、页签和游标限定范围。修改请求使用参数校验、版本/预览校验、幂等编号及永久回执；涉及在线状态时通过游戏适配器查询。

**PVF 更新**：批准 PVF → 完整 SHA256 → 离线建立目录与各领域索引 → 校验记录与解析版本 → 配置新代次 → 管理服务读取。外部运行实例在启动时绑定 PVF/索引；游戏侧绑定相同摘要；无缝热切换与集中索引作业页面尚未完成。长期福利已有跨 PVF 差异预览、确认与历史记录，保留账号/角色去重身份。

## 当前缓存与内存边界

| 数据 | 当前方式 | 失效依据 / 下一步 |
| --- | --- | --- |
| PVF 文件目录与条目正文 | `DiskArchive` 磁盘目录及定点读取 | 完整 PVF SHA256；管理进程不整体展开 PVF |
| 物品摘要目录 | 从持久 JSON 流式加载、字符串复用，摘要仍常驻内存 | PVF 摘要/索引版本；后续可分页或分片 |
| 物品详情 | 磁盘详情索引，按物品读记录 | PVF 摘要、记录摘要 |
| 游戏任务定义 | 磁盘任务索引，按查询/单条读取 | PVF 摘要与解析版本 |
| 职业、觉醒 | 磁盘索引；运行时只保留一个所选职业的技能及职业任务子集 | PVF 摘要、索引与定义版本 |
| 成长、SP/TP、金库等小表 | 按摘要缓存少量配置 | PVF 摘要；无需全文件反复解析 |
| 玩家资产、邮箱、任务状态 | 按需读取数据库投影、分页，写后显式刷新 | 实例/行版本和操作回执；尚未建立统一玩家快照缓存层 |
| 网页状态 | 当前页状态、请求取消、显式刷新 | 切换目标/页签；已验证流程空闲不轮询 |
| 更新资源 | Hub 文件缓存 | 签名/文件完整摘要 |

已保存的前一轮 Linux 职业功能测试：`GOMEMLIMIT=128MiB`，21 类读取，RSS 138.99MiB、峰值152.86MiB、swap 0。数据库在另一容器，无并发负载；这是历史测试结果，副职业候选尚未重新测量。Go 软内存目标不是进程 RSS 上限。

## 后续基础能力

1. **游戏侧编辑保护**：账号准入与数据库行锁协议已接入；2026-10-18候选增加角色选角准入，只阻断目标角色。角色协议须整合最新游戏源码并联合上线，见[并发保护](GM_EDIT_CONCURRENCY.md)。
2. **统一权限与审计**：现有 Key/Cookie/CSRF、各功能回执已落地。多管理员身份、细粒度角色权限及统一审计检索继续补齐。
3. **作业执行**：邮件、福利、邮箱清理已有持久批次与恢复；自动福利扫描已接。通用调度器、并发租约、索引/公开快照作业待建设。
4. **公开玩家快照与异常分析**：每日低优先级快照、GM 名单/全部角色（含未来角色）、只读公开接口；依赖背包投影和游戏侧通关事件。当前只保留设计契约。
5. **前端接入统一**：公告在 Hub `/gm/` 挂载模式可用；当前独立 `/console/` 入口需要统一 Hub 会话和公告页面。功能页面按完整台账重组。

Redis 不属于当前管理模块的必要运行依赖；源码继承依赖不代表应部署额外缓存服务。客户端 PVF 分片/IndexedDB 以及通用多层玩家缓存是目标能力，尚未实现。

## 当前版本与上线依据

当前集成候选已对齐登录器0.2.16的确切发行源码，保留目录发现、原生退出等待、下载恢复和程序／内容独立更新。网关合入内容寻址下载与百度分发，同时保留独立GM管理监听和公告版本校验。来源见[逐文件接入清单](live-toolchain-import.json)。副职业隔离数据库与浏览器验收已通过。

工具仓不负责推进游戏上线。若服务端改动依赖新 EXE，顺序为：最终 EXE 双端 Release/指定镜像 → 玩家实际更新通道验签验哈 → EXE/PVF/dstr/NPK/profile 完整匹配 → 部署依赖服务端。PVF 使用本服批准来源；索引按其摘要换代。

## 源码核对入口

- [监听与路由组装](../gateway/cmd/playergateway/main.go)
- [管理模块与连接池](../gateway/internal/management/module.go)
- [当前管理路由入口](../gateway/internal/services/gmtool/managed.go)
- [v1读取/操作路由](../gateway/internal/services/gmtool/api_v1.go) 与 [邮件/福利作业](../gateway/internal/services/gmtool/api_v1_operations.go)
- [游戏运行接口客户端](../gateway/internal/modules/dnf/adminapi/client.go)
- [公告页面挂载条件](../gm-web/announcements.js)
- [登录器Host协议](../launcher/Host/Program.cs)

当前状态以本文件、功能台账及实际路由目录为准；`GM_GATEWAY_ARCHITECTURE.md` 和 `GM_API_V1_DRAFT.md` 保留早期设计背景。
