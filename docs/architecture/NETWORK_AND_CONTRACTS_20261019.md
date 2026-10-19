# PurpleMagic 网络拓扑、模块职责与接口方案

2026-10-20 公开版。本文区分当前接口与下一版规划；部署示例已使用通用地址。图：[网络拓扑](network-20261019.html) · [模块分工](modules-20261019.html) · [阅读入口](overview-20261019.html)。

## 1. 推荐布局

**控制主机承担控制服务，国内主机承担玩家入口与游戏，下载服务独立承载资源，Windows 构建节点生成登录器。**

游戏 TCP、账号 HTTPS、更新下载是三个独立连接。默认游戏直连国内入口，账号 API 直连国内玩家网关，资源直连配置的下载源。管理流量通过控制主机的 HTTPS 入口进入，再经 SSH 隧道访问游戏管理网关。

先拆清模块与契约，按负载独立进程：控制核心和发布编排可以同进程，PVF 索引构建采用独立 Worker，Windows 构建必须独立运行。当前规模先使用持久作业表和有界缓存；不新增 Redis、消息中间件或 Kubernetes 依赖。

| 模块 | 运行位置 | 负责什么 | 当前基础 / 下一步 |
| --- | --- | --- | --- |
| 游戏接口网关 | 国内游戏主机 | 账号/角色查询、编辑准入、领域事务、幂等和永久回执；适配游戏私有 API | 已有 `playergateway` 管理模块；保持轻量 |
| 调度核心 | 控制主机 | 管理认证、服务器配置、作业状态、重试/暂停/恢复、调用网关 | GMService 已有邮件队列和福利扫描；通用调度与服务注册待建 |
| PVF 索引与查询缓存 | 控制主机，可拆进程 | 批准 PVF 的索引构建、定义查询、静态搜索；玩家查询短缓存独立分区 | 六类索引与按需查询已有；统一索引作业和玩家缓存待建 |
| 发布器 | 控制端编排，国内/存储端发布适配器 | 兼容资源集、验哈、签名清单、原子发布、递增序号、回退 | Hub 已有检查/签名发布；通用发布作业与存储适配待建 |
| Web 前端 / App / 移动端 | 管理员设备 | 共用管理 API；保留认可的兰紫毛玻璃布局 | Web 已有；移动网页优先，原生 App 后续接入 |
| 登录器生成器 | 控制端 UI + Windows Worker | 选择服务器配置、品牌资源和登录器模板，生成便携包/更新包及构建回执 | 有 Windows 构建脚本；网页生成器、构建队列待建 |
| 登录器 | 玩家 Windows 设备 | 账号登录、配置验签、下载/安装更新、目录发现、启动游戏 | 当前源码 0.2.16；保留其频道目录兼容逻辑 |
| 下载源 | 国内 HTTP 文件服务 / CDN / 对象存储前端 | 已签名清单、不可变内容文件、登录器包 | 已有 HTTPS 内容源与百度分发适配；通用上传适配待建 |
| 游戏服务本体 | 国内游戏主机 | 游戏登录/频道/玩法、在线态、内存与存档一致性 | 游戏项目独立维护 |
| 数据与运行保障 | 各服务所属主机 | 游戏 DB、控制状态、密钥、日志/健康、备份 | 玩家 DB 留在游戏侧；控制作业、发布状态各自持久化 |

论坛以后作为独立项目和独立业务 API。若展示游戏信息，使用专门的只读公开快照，管理接口不复用于论坛访客请求。

## 2. 三种“直连”分别设置

| 连接 | 默认方案 | 配置含义 | 经过中转的代价 |
| --- | --- | --- | --- |
| 游戏实时连接 | 玩家 → 国内游戏 IP 和频道端口 | `GameHost`、`BootstrapPort`、独立频道表 | 跨地域 TCP 中转会增加延迟和带宽成本 |
| 账号与状态 | 登录器 → 国内玩家 HTTPS API | 当前 `ApiUrl` | 只影响登录/注册/状态请求；不等同游戏流量 |
| 更新文件 | 登录器 → HTTPS 下载源/CDN | `content.endpoint` 和 `launcher.url` | 大文件经控制主机会占用中转带宽；采用下载重定向或源直连 |
| 管理操作 | 管理端 → 控制主机 → 隧道 → 游戏网关 | 管理 Origin、私有 peer URL、凭据引用 | 可接受管理请求往返；重任务在外部执行 |

“直连”包含国内主机上的 NAT、端口映射或本机转发，但游戏流量不穿过控制主机。若以后确有游戏加速线路需求，另设 `relay` 模式，配置专用四层线路及完整频道地址映射；不能仅替换登录入口 IP。

建议生成页面展示三个独立选项：“游戏线路：直连”“账号入口：国内玩家 API”“下载线路：所选内容源”。首版默认直连；普通使用只需选择已保存的服务器配置，再生成。

## 3. 部署示例（公开地址为示例值）

以下端口说明当前服务分工；域名、IP、主机别名和路径采用示例值。部署者按自己的环境填写配置。

| 位置 | 监听 / 地址示例 | 用途 |
| --- | --- | --- |
| 控制主机公网 | `https://admin.example.com` | 管理 HTTPS 入口；`/admin/*` 转游戏侧 Hub，其余转外部 GM |
| 控制主机 loopback | `127.0.0.1:29446` | GMService Web、会话、v1 API |
| 控制主机 loopback | `127.0.0.1:29447` | PVF 定义服务 |
| 控制主机 loopback | `https://127.0.0.1:29445` | SSH 反向转发到游戏侧管理监听，保留 TLS 校验 |
| 游戏主机公网 | `https://player.example.com:443` | 玩家 API、签名 bootstrap、登录器包；可配置本地内容源 |
| 游戏主机 loopback | `127.0.0.1:10999` | 独立管理监听，处理 GM 事务与 Hub 管理 |
| 游戏主机 loopback | `127.0.0.1:29447` | SSH 本地转发到控制主机的 PVF 定义服务 |
| 游戏主机 loopback | `http://127.0.0.1:17002` | 游戏账号上游与私有 `/api/game/admin` |
| 游戏入口 | `203.0.113.20:10000` | 发布 profile 的 `GameHost:BootstrapPort`；内部转发按部署配置 |
| 游戏频道 | 独立外部频道表 | 公网端口由频道配置和端口映射生成 |

游戏主机主动建立 SSH 长连接，两个方向分别承载管理请求与定义查询：

```text
SSH 发起：game-host → control-host
管理请求：control-host 127.0.0.1:29445 → -R → game-host 127.0.0.1:10999
定义查询：game-host 127.0.0.1:29447 → -L → control-host 127.0.0.1:29447
```

“上游检查源”与“玩家下载入口”是独立配置。发布器可检查一个来源，再把批准文件发布到本服存储。当前仓库已对齐登录器 0.2.16，精确来源见 [导入清单](../launcher-0.2.16-import.json)。

## 4. 接口与网络契约

以下“现有”按实际源码路由；“拟新增”是下一版设计，尚未注册。

| 调用方 → 服务方 | 方法与路径 | 作用 | 身份与传输 | 状态 |
| --- | --- | --- | --- | --- |
| 登录器 → 国内玩家网关 | `POST /api/account/login`、`POST /api/account/register` | 登录/注册 | HTTPS；玩家账号凭据 | 现有 |
| 登录器 → 国内玩家网关 | `GET /api/status` | 运行状态 | HTTPS，公开只读 | 现有 |
| 登录器 → 发布读取入口 | `GET/HEAD /launcher/v2/bootstrap.json` | 公告、profile、内容与登录器发布信息 | HTTPS；本服 RSA-PSS 签名、公钥验签、发布序号 | 现有，入口绑定 `ApiUrl` |
| 登录器 → 内容源 | `GET/HEAD /blobs/{sha256}` | 下载不可变文件 | HTTPS、Range/206、长度和 SHA256；gzip 另验解压后文件 | 现有 |
| 登录器 → 登录器包源 | `GET/HEAD /launcher/v2/bundles/{sha256}.zip` | 更新登录器 | HTTPS、Range/206、签名清单中的长度/摘要 | 现有 Hub 路径 |
| Web → 外部 GM | `POST /api/auth/login`，`GET /api/v1/capabilities` | Token 登录、30天会话、能力/CSRF | 同源 HTTPS、HttpOnly Cookie；写请求 CSRF | 现有 |
| Web → 外部 GM | `/api/v1/accounts`、`/characters`、`/items`、`/quests` 等 | 领域查询和操作 | 管理会话；修改含 revision、幂等及领域校验 | 现有 |
| 核心 → 游戏管理网关 | `/api/v1/*` 中玩家领域路径 | 有界查询、预览/事务、回执 | SSH/TLS + Bearer + 协议头 + PVF hash | 现有，静态 PVF 搜索在外部完成 |
| 调度 → 游戏管理网关 | `POST /api/internal/gateway/v1/scan` | 福利扫描，每次最多 5 个账号 | 私有服务身份，游标推进 | 现有 |
| Web → 外部 GM 队列 | `POST /api/v1/tasks/{id}/schedule`、`/pause` | 确认后后台执行/暂停邮件 | 管理会话；队列持久化 | 现有 |
| 游戏网关 → PVF 定义服务 | `POST /internal/catalog/v1/resolve` | 按指定操作读取有界定义 | 独立 catalog Token + 协议/PVF hash | 现有，注意调用方向 |
| 游戏网关 → 游戏服务 | `POST /api/game/admin` | 在线快照、角色准入、活动及运行态动作 | 本机私网 + 游戏专用 Bearer；版本化 action JSON | 现有 |
| 发布后台 → Hub | `GET /admin/api/state`；`PUT /admin/api/source` | 读取发布状态、保存上游源 | 私有管理会话/CSRF | 现有 |
| 发布后台 → Hub | `POST /admin/api/check`、`/publish` | 检查候选、确认兼容后签名发布 | 私有管理会话；固定 candidateId | 现有 |
| 发布后台 → Hub | `PUT /admin/api/connection`、`/news` | 修改游戏入口、公告 | CSRF + `If-Match`；发布序号递增 | 现有，保存即发布 |
| 发布后台 → Hub | `POST /admin/api/bundle/{version}` | 上传登录器更新包 | 私有管理会话/CSRF | 现有，上传与发布有现有语义 |
| 管理端 → 核心 | `/control/v1/servers`、`/jobs`、`/launcher-builds`、`/releases` | 服务器配置、通用作业、生成/发布 | 统一管理会话；写操作幂等、revision | 拟新增 |
| Worker → 核心 | `POST /internal/control/v1/jobs/claim`、`/{id}/heartbeat`、`/{id}/complete` | 主动领取作业、续租、提交产物回执 | HTTPS + 独立受限 Worker 凭据 | 拟新增 |

Web 与未来 App 共用领域契约。移动网页继续使用现有同源会话；原生 App 若接入，新增可撤销设备会话，不向客户端下发网关服务 Token。游戏私有 API 由网关适配；外部调度无需获得数据库 DSN。

### SSH 与 Token 的分工

推荐 **SSH 隧道 + 服务 Token**。SSH 负责连通性、加密和主机身份；Token 负责应用服务身份，两者叠加使用。当前控制主机→国内还保留 TLS 校验。

现有私有服务请求头：

```http
Authorization: Bearer <该方向专用服务凭据>
X-PurpleMagic-Protocol: purplemagic-service-v1
X-PVF-SHA256: <批准 PVF 的完整 SHA256>
```

SSH 专用账号限定转发地址/端口、固定主机密钥、systemd 自动恢复；隧道两端绑定 loopback。两方向 Token 独立。若以后改为直接跨主机调用，采用 HTTPS + Token；mTLS 可作为后续传输方案。当前服务客户端支持 CA/SNI，尚未配置客户端证书契约。

公网开放：管理 HTTPS、国内玩家 HTTPS、游戏登录/频道端口、下载 HTTPS。管理监听、PVF 定义、游戏私有 HTTP、MariaDB 留在 loopback/私网；SSH 只承担运维和受限转发。

## 5. 更新源到底填什么

把更新链路分成四种对象，后台分别命名，避免都叫“更新地址”。

| 对象 | 要填什么 | 示例 | 何时使用 |
| --- | --- | --- | --- |
| 上游检查源 | `id/name/endpoint/publicKey/enabled`，可选 `certificateSha256` | `https://updates.example.com` | 发布器检查上游稳定/预览通道 |
| 本服签名清单 | 本服发布读取入口和本服公钥 | 当前 `ApiUrl + /launcher/v2/bootstrap.json` | 登录器获取已批准配置和资源集 |
| 玩家内容下载入口 | HTTPS 源站 Origin，具有 `/blobs/{hash}` | `https://cdn.example.com` | 玩家下载 EXE、PVF、dstr、NPK |
| 登录器程序包 | 签名清单中的完整包 URL、长度、hash、版本 | `https://player.example.com/launcher/v2/bundles/<hash>.zip` | 更新登录器本身 |

### 当前上游 Source 契约

`endpoint` 必须是 HTTPS **Origin**：域名或 IP，可带端口；路径只能为空或 `/`，不含账号密码、query、fragment。检查时服务拼接：

```text
{endpoint}/channels/stable/manifest.signed.json
{endpoint}/channels/preview/manifest.signed.json
```

配置示意（公钥值需替换为有效 PEM）：

```json
{
  "id": "primary-content",
  "name": "主内容源",
  "endpoint": "https://updates.example.com",
  "publicKey": "<上游 RSA PUBLIC KEY PEM>",
  "certificateSha256": "",
  "enabled": true
}
```

请求检查：`POST /admin/api/check`，body 为 `{"id":"primary-content","channel":"stable"}`；成功返回 candidateId、摘要及兼容性。发布使用此 candidateId 与兼容确认。上游 Envelope 为 `Payload/Signature`，当前协议是 RSA-SHA256 PKCS#1 v1.5；本服 bootstrap 使用 RSA-PSS。两份公钥和签名协议分别管理。

### 资源文件服务

最简源站是能返回静态文件的 HTTPS 服务，按契约放置清单和内容寻址文件即可。下载支持 HEAD/GET、Content-Length、Range/206 与 Content-Range。不可变 blob 可长期缓存，动态发布指针使用 no-store/短缓存；更新时先上传文件并验哈，再切换清单。

| 存储/来源形式 | 使用方式 | 适配状态 |
| --- | --- | --- |
| 自建 HTTPS 文件站 / Nginx / Caddy | 暴露上述清单和 blob 路径 | 可满足现有下载契约 |
| CDN + 对象存储（S3/OSS/COS/R2 等） | 自定义 HTTPS 域名提供相同路径；上传凭据留在发布端 | 下载可用通用 HTTP；SDK 上传适配待建 |
| 当前国内 Hub 内容源 | 由已发布清单允许的 hash 提供 `/blobs/` | 已有 |
| 百度分发 | 服务端适配器解析，客户端能力协商后下载，最终验同一 hash | 已有专门适配；不等于可填任意分享页 |
| GitHub / GitGud Release | 发布器导入资产、校验、镜像到契约化源站 | 仓库/Release 页面 URL 不是现有 Source endpoint；导入适配待建 |

当前一个发布 Content 使用一个 endpoint。自动多镜像择优、测速和分文件源选择属于新增契约；首版保留主源与管理员手动切换。切换只影响资源，不改变账号入口和信任根。

## 6. 登录器生成配置

配置保存在调度核心，区分“内部运维配置”和“嵌入玩家包的公开配置”。管理员选择服务器配置后生成；核心记录配置 revision、源码 commit、工具链、产物 hash 与构建日志。

| 页面字段 | 当前映射 / 目标字段 | 必填条件与校验 |
| --- | --- | --- |
| 服务器标识、显示名 | 拟新增 `serverId/displayName` | 必填，稳定 ID |
| 游戏线路 | 拟新增 `gameConnectionMode=direct` | 首版默认直连；不复用管理中转地址 |
| 游戏入口 IP | `GameHost` | 当前登录器只接受 IP；域名支持需统一服务端和客户端解析契约 |
| 登录入口端口 | `BootstrapPort` | 当前客户端要求 1024–65535；与后台目前 1–65535 的校验统一 |
| 账号 API | `ApiUrl` | HTTPS；当前同时决定 bootstrap 读取位置，信任根锁定账号入口 |
| 配置清单地址 | 当前由 ApiUrl 派生；拟新增 `BootstrapUrl` | 若要独立托管清单，需同步升级登录器验签与下载客户端 |
| HTTPS 证书指纹 | `CertificateSha256` | 当前玩家 API 使用证书 pin；受信 CA 模式应在新契约中显式设计 |
| 本服发布公钥 | `UpdatePublicKey` | 必填，从发布器公钥导入；私钥留在签名端 |
| 批准兼容资源集 | `ExeSha256/PvfSha256` + 完整内容清单 | 发布器选择已验哈的 EXE/PVF/dstr/NPK/profile 集合 |
| 频道目录身份与兼容 | `ChannelServerId/PreviousChannelServerId/ChannelHistory` | 继承 0.2.16 和当前独立外部频道表 |
| 下载源 / 登录器版本 | 签名 bootstrap 的 `content/launcher` | 选择已有发布；不用手填临时下载地址 |
| 最低登录器版本 | `MinimumLauncherVersion` | 绑定兼容要求；当前线上为 0.2.16 |
| 客户端默认路径 | `DefaultClientDirectory` | 可选；保留自动目录发现 |
| 名称、图标、背景 | 拟新增品牌资源配置 | 采用批准素材，记录文件 hash |

当前 `SettingsUrl` 是旧版更新/Probe 保留字段；Flutter 0.2.16 的主入口实际读取 `ApiUrl + /launcher/v2/bootstrap.json`。仅修改 SettingsUrl 不会迁移当前 Flutter 清单入口。

公开 profile 示意（占位 hash、公钥需由发布器替换）：

```json
{
  "ApiUrl": "https://player.example.com",
  "CertificateSha256": "<玩家 API 证书 SHA256>",
  "UpdatePublicKey": "<本服发布公钥 PEM>",
  "SettingsUrl": "https://player.example.com/launcher/settings.json",
  "GameHost": "203.0.113.20",
  "BootstrapPort": 10000,
  "ExeSha256": "<批准 EXE SHA256>",
  "PvfSha256": "<批准 PVF SHA256>",
  "DefaultClientDirectory": "",
  "ChannelServerId": 1,
  "MinimumLauncherVersion": "0.2.16"
}
```

内部分别保存 `gatewayUrl/sshCredentialRef/serviceTokenRef/catalogUrl/publisherKeyRef/storageCredentialRef`；玩家包仅含公开 profile、公钥和资源指纹。

生成流程：保存配置 → 核心校验可达性与兼容集 → Windows Worker 领取固定 revision 作业 → 构建便携包及 app-update → 回传产物和摘要 → 查看/下载。**生成完成是构建结果；“发布到玩家通道”是独立动作。** 现有 Hub 的连接保存/包上传带发布语义，下一版先增加 draft/stage，再接生成器页面。

Windows Worker 主动通过 HTTPS 领取任务，接收固定模板、公开 profile 与批准素材，返回指定产物；控制 API 不接受任意 shell 命令。签名私钥由发布器使用，构建节点不需要持有。

## 7. 调度、缓存和发布的一致性

| 状态 | 权威归属 | 更新方式 |
| --- | --- | --- |
| 玩家背包/资产/任务、规则发放回执 | 游戏侧 MariaDB 和领域事务 | 网关调用，在线准入、revision、幂等；成功后失效相关查询缓存 |
| 游戏在线态、编辑准入 | 游戏运行 API | 写入前实时核对；角色操作只要求目标角色离线，账号共享资产走账号级保护 |
| 邮件/福利调度进度 | 控制端持久队列 + 游戏侧永久回执 | 至少一次派发、原幂等键重试、读回回执；不能用超时判定“未执行” |
| PVF 定义与索引 | 批准 PVF hash + 解析器/索引格式版本 | hash 改变，新建完整代次、验证、发布兼容集、切换双端；旧代次单独保留 |
| 静态查询缓存 | PVF 代次 | key 含全 hash/索引版本；有界 LRU，不能混用代次 |
| 玩家查询缓存（拟建） | 游戏网关返回的 observedAt/revision | 只作页面读取优化；按账号/角色/页签有界 TTL，写后失效，提交时回权威态校验 |
| 发布清单与安装包 | 发布器 | 文件先入库验哈，清单后发布；回退旧内容使用更高发布序号 |

中转或 PVF 服务断线时，管理请求显示不可用，调度暂停并恢复；既有玩家 API、游戏和已发布文件入口继续独立运行。游戏网关只保留小型定义缓存，当前摘要上限 512 条，不加载外部完整 PVF。

PVF 内容一致由完整 hash 判断；路径、mtime、大小不作为身份。解析器或索引格式升级即使 PVF hash 不变，也需要生成相应的新索引版本。

发布兼容集包括 EXE/PVF/dstr/NPK/profile、游戏程序和频道配置的关联。保留部署环境的独立外部频道表；游戏版本部署仍沿用游戏项目的累计 active 和统一 verify。发布器负责记录兼容与部署回执，游戏启动/容器生命周期由游戏部署适配器承担。

## 8. 建设顺序与验收

1. **契约收口**：沿用已合入的 0.2.16；统一 GameHost/端口校验；把账号入口、清单入口、下载入口和私有网关字段分开；建立配置草稿与发布态。
2. **生成器首版**：管理页复用兰紫布局；服务器配置模板 + Windows Worker + 构建回执；先提供包下载，再接发布动作。
3. **通用调度**：将现有邮件/福利队列抽象为持久作业；加入优先级、租约、重试、暂停、取消与状态查询，保留原规则 ID 和发放回执。
4. **索引与发布作业**：批准 PVF → 六索引验证 → 兼容检查 → 存储上传 → 签名发布 → 玩家通道读回；再加入存储 SDK 和多镜像。
5. **客户端扩展**：移动网页先用同一套 API；原生 App、独立论坛、公开快照按需接入。

验收重点：公网与私有路由隔离；跨隧道双方向鉴权/PVF代次；构建配置与包内 profile 一致；登录器真实验签、完整下载验哈、二次零下载、原生目录/频道首次与重复迁移；断线重试以永久回执去重；发布回退保持序号递增。网络验收分别测账号往返、游戏延迟和下载吞吐，不用单一“服务器可达”替代。

## 9. 源码依据与交付验证

- [玩家/管理监听](../../gateway/cmd/playergateway/main.go)、[外部 GM 进程](../../gateway/cmd/gmservice/main.go)
- [管理配置](../../gateway/internal/management/config.go)、[服务鉴权](../../gateway/internal/servicehttp/service.go)
- [私有网关/外部代理](../../gateway/internal/services/gmtool/external.go)、[目录查询](../../gateway/internal/services/gmtool/remote_catalog.go)
- [Hub 数据契约](../../gateway/internal/services/launcherhub/contracts.go)、[实际发布路由](../../gateway/internal/services/launcherhub/admin.go)、[内容下载](../../gateway/internal/services/launcherhub/content_blob.go)
- [候选登录器 profile/启动参数](../../launcher/Core/PlayerConfig.cs)、[bootstrap 验签](../../launcher/Engine/Contracts.cs)、[Windows 构建脚本](../../deploy/windows/build_flutter_launcher.py)
- [0.2.16 源码导入清单](../launcher-0.2.16-import.json)记录准确版本与逐文件摘要。
- 图的精确规格和 HTML SHA256、9/9 Showcase 检查结果见 [网络图回执](network-20261019.delivery.json)、[模块图回执](modules-20261019.delivery.json)；视觉核验见 `REVIEW_20261019.md`。

本页描述架构和接口设计；源码同步与服务部署分别执行。
