# PurpleMagic 仓库与发行边界

PurpleMagic 是独立 Git 仓库，统一管理工具链源码。游戏服务继续保留在原游戏仓库，通过私有运行接口和已有 MariaDB 契约协作。

## 目录和产物

| 路径 | 所属产物 | 维护边界 |
| --- | --- | --- |
| `launcher/Flutter`、`Host`、`Engine`、`Core`、`Bootstrap`、`Portable` | Windows 登录器 | Flutter/C#同一客户端；私有签名profile、图像资产和发布凭据外置 |
| `gateway/` | PlayerGateway、GMIndex、GMService | 一个网关进程含玩家入口、Hub、GM管理模块；GMIndex单独离线运行 |
| `gm-console/` | 兰紫 GM Console 静态包 | 使用外部 GM / Hub API，Node.js 只负责构建 |
| `gm-web/` | 封存的第一版 UI | FROZEN_DO_NOT_REFERENCE，仅保留历史追溯 |
| `gm/` | 旧GM与离线工具、迁移参考 | 保留现有NOTICE、模块、领域代码和测试；不作为新部署额外常驻服务 |
| `console/` | 早期界面参考 | 当前前端入口为`gm-console/`；后续移除前先核对引用 |
| `scripts/`、`deploy/` | 检查、构建、预览与安装辅助 | BAT/Python入口；构建和部署分别执行 |
| `docs/` | 功能、架构、接口、来源说明 | 可公开内容；原始图片、环境回执留私有目录 |
| `local-notes/`、`runtime/`、`dist/`、`.cache/` | 项目内记忆、配置、日志、资产与产物 | Git忽略；不随源码提取或远端推送 |

这次提取保留两个独立 Go 模块，没有本地 `replace` 指向游戏仓库。`gateway/` 使用自己的领域/仓储源码副本，`gm/` 保留旧实现。后续共同领域模块去重需要独立迁移，不在本次改目录时改事务语义。

## 提取与来源

2026-10-17 从现有集成候选复制2,110个源码文件，共20,475,757字节，每文件复制前后SHA256一致。源候选无首次提交，不能用HEAD标识；本次忽略目录中的`copy-receipt.json`固定了逐文件输入摘要。原`source-manifest.json`与`gm-gateway-migration-manifest.json`保留原始来源，新增工作后不将历史导入清单当作当前完整内容。

后续工具链工作以本仓库为入口，公开源码托管于 [Cec1c/PurpleMagic](https://github.com/Cec1c/PurpleMagic)。游戏适配器继续在游戏仓库独立提交。

## 发布前需要对齐的三个基线

| 基线 | 当前记录 | 对齐动作 |
| --- | --- | --- |
| 登录器 | 已精确合入0.2.16发行源码；目录发现功能保留 | 发布时继续核对签名profile、实际玩家通道和下载摘要 |
| 游戏契约 | 本仓复制的仓储和adminapi协议 | 对照实际累计服务端字段、表结构与运行接口版本；在线快照适配尚待部署联调 |
| PVF | 每个批准版本独立生成索引；版本回执保存在项目运行目录 | 切换前核验完整SHA256、物品／任务／职业定义及历史福利规则；旧代次保留供回退 |

副职业已完成隔离数据库与网页验收；原生游戏交互按部署环境独立验收。

## 检查入口

```text
python scripts/check_boundaries.py
python scripts/verify_component.py gateway
python scripts/verify_component.py gm
python scripts/verify_component.py launcher --dotnet <dotnet8>
python scripts/verify_component.py flutter --flutter <flutter>
python scripts/build_gateway.py windows amd64
python scripts/build_gateway.py linux amd64
python gm/scripts/build.py windows amd64
python gm/scripts/build.py linux amd64
```

Flutter引用的私有图片由构建输入提供；缺少图片时不会生成替代图片。此次源码提取没有数据库逻辑变化，不运行会改变数据库的测试。原副职业一次性MariaDB失败和网页验收缺项继续留在项目内交接。

## 项目计划归属

五阶段计划仅属于 PurpleMagic 与原游戏项目的项目内交接：内存定位 → 仓库拆分整合 → 前端视觉方案 → 功能及接口重构 → 上线验收。当前任务完成功能盘点、架构与仓库提取；此前暂停的功能实现按后续指令推进。

EXE同步仍为作者GitGud Release与本方GitHub Release双向；fork只用于源码贡献。工具链提取不改变发布序号、签名profile、玩家通道或线上资源。
