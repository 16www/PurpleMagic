# 游戏服务如何接入 PurpleMagic 网关

PlayerGateway 同时提供公开玩家 API 和独立私有管理监听。现有适配器面向本仓的 DNF90/ServerS5A3 Go 存档与登录协议。接入其他服务端时，需要实现下面的 HTTP/运行态协议，并适配 `gateway/internal/modules/dnf/repository/mysql/` 的存档映射；仅换数据库地址不能转换存档结构。

## 接入关系

| 方向 | 配置 | 工作内容 |
| --- | --- | --- |
| 登录器 → PlayerGateway | profile `ApiUrl` | 注册、登录、状态、签名 bootstrap 和下载 |
| PlayerGateway → 游戏账号服务 | gateway `upstream`、`proxy_secret` | 白名单转发三个玩家接口 |
| 登录器 → 游戏 TCP | profile `GameHost`、`BootstrapPort` | 原生登录和频道，默认直连 |
| GMService → 游戏侧管理网关 | service `gateway` | 查询、事务、回执、后台福利扫描 |
| 游戏侧网关 → GMService | management `catalog` | 按需获取 PVF 定义 |
| 游戏侧网关 → MariaDB | management DSN/表映射 | 本服领域事务与永久回执 |
| 游戏侧网关 → 游戏运行态 | management `gameApi`、`gameToken` | 角色/账号编辑准入与完整在线快照 |

[Compose 部署](DEPLOYMENT.zh-CN.md)的配置准备工具会生成互相匹配的 `gateway.json` 和 `service.json`。数据库连接只出现在游戏侧配置。

## 账号服务

游戏上游需要提供：

- `POST /api/account/register` 与 `POST /api/account/login`，请求字段为 `account`、`password`、`device`。
- 登录成功返回 `{"ok":true,"account_id":"…","token":"…"}`；失败返回 `ok:false` 和用户可读 `message`。注册同样返回 `ok`，登录票据须被游戏登录服务接受。
- `GET /api/status` 返回 HTTP 200 作为可用信号。

网关在转发时移除外部 `Cookie`、`Authorization`、伪造的来源头，写入 `X-Player-IP` 与 `X-Player-Proxy-Key`。游戏服务必须验证代理凭据，并按实际会话机制生成短期登录票据。现有客户端启动协议要求票据匹配 `de509f65` 加 24 位小写十六进制，参数构造见 [GameContract](../launcher/Core/PlayerConfig.cs)。

游戏 TCP 握手、频道目录和端口格式见 [ChannelProbe](../launcher/Core/ChannelProbe.cs)。客户端读取目录后还会探测代表性游戏端口；仅账号接口可用不足以完成游戏连接。

## 私有运行态 API

游戏提供 `POST /api/game/admin`，接收 JSON `version:1`、`action` 及相应目标字段，验证 `Authorization: Bearer <gameToken>`。响应包含 `version:1`、`ok` 与 action 对应的数据。

| action | 输入/输出要点 | 管理用途 |
| --- | --- | --- |
| `character_edit_status` | `accountId`、`characterId`；响应 `characterEdit.protocol=character-admission-v1`、目标身份与 `busy` | 指定角色的登录准入、在线与退出清理状态 |
| `account_edit_status` | `accountId`；响应 `accountEdit.protocol=account-row-v1`、账号身份与 `busy` | 账号共享资产保护 |
| `online_snapshot` | 响应快照 ID、时间、`complete`、角色及会话 generation | 在线筛选与在线邮件投递 |
| `catalog` | 支持的奖励/游戏定义目录 | 部分资产与定义校验 |

具体字段以 [protocol.go](../gateway/internal/modules/dnf/adminapi/protocol.go) 及同目录协议类型为准。准入判断要与游戏登录和退出清理共享实际锁/状态，避免角色在线内存覆盖管理写入；详见[编辑并发](GM_EDIT_CONCURRENCY.md)。角色专属修改只检查目标角色，账号共享资产使用账号级保护。

## 存档与建表

`registrationDsn` 指向注册/福利记录库，`repositoryDsn` 和 `databases` 指向游戏存档库及分片，`accountTable`、`tablePrefix` 对应现有表名。DSN 使用 `parseTime=true`，禁用多语句执行。按部署指南运行 `PlayerGateway -init-management-schema` 初始化管理附加表。

保留规则 ID、任务 ID、领取回执和发布状态。福利发放与回执在同一事务中提交，迁移服务器时应整体迁移这些记录。

## 跨主机接入

示例：游戏主机保留 management `127.0.0.1:18444`，控制主机运行 catalog `127.0.0.1:18081`。从游戏主机建立：

```sh
ssh -N -o ExitOnForwardFailure=yes -o ServerAliveInterval=30 \
  -L 127.0.0.1:18081:127.0.0.1:18081 \
  -R 127.0.0.1:18444:127.0.0.1:18444 control-host
```

`-L` 让游戏网关读取控制主机的 PVF 定义；`-R` 让控制主机访问游戏网关。使用受限 SSH 用户并把此连接交给服务管理器保持运行；SSH 主机密钥先按运维流程核验。隧道两端继续使用服务 Token、协议头和相同 PVF hash；management 保留 TLS，配置 `caFile` 和证书 `serverName`。

两台主机分别安装对应服务及持久目录。玩家 `ApiUrl`、游戏 `GameHost` 和下载源继续直连其各自服务，不经 GM 控制主机。

## 接入检查

先验证玩家 status 与签名 bootstrap，再用 GM 查询角色和 PVF 物品、检查 Hub 页面。接着运行登录器 `Probe --channel-only`；最后在隔离账号上检查离线角色编辑、单次邮件、福利去重和暂停恢复。不同服务端的端到端适配以这些协议和数据行为为准。

For English readers: the gateway expects the game's account HTTP routes, a version-1 private runtime API, the matching MySQL repository schema, and the native DNF90 TCP/login contract. The [deployment recipe](DEPLOYMENT.md) wires the current adapter; other server implementations must implement these contracts and their storage mapping. Player HTTPS, direct game TCP and private GM/catalog traffic remain separate.
