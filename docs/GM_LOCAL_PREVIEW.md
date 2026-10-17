# 当前 GM 网页预览

2026-10-17 当前预览使用实际单网关 `scripts/preview_gateway_live.py` 与独立 `gm-web/`，地址与 Key 文件写入其 runtime 回执。已接点券增减与流水、游戏任务操作、背包/个人仓库配置与删除。详见 [单网关实现](GM_GATEWAY_IMPLEMENTATION.md)。

```text
python scripts/preview_gateway_live.py --pvf <asset-directory>/Script.pvf --index-dir <index-directory> --sha256 <approved-pvf-sha256>
```

打开 `https://127.0.0.1:8768/console/#players`，导入 `runtime/preview/gateway-live/login-key.json`。选择“晨雾”查看点券与流水，进入“晨雾·剑影”查看背包、任务及各自操作记录。停止入口为 `python scripts/preview_gateway_live.py --stop`；重启会重置示例数据库和 Key。

下文保留原 GM 预览入口。

预览使用当前 GM 页面和 Go API，玩家数据存放在独立临时 MariaDB；物品与任务读取已生成的真实 PVF 索引。页面顶栏标记“本地预览 · 示例玩家”。

从项目根目录运行，需要 Docker、Go 1.26.5、Python 3.10+ 及已准备的 PVF 索引：

```text
python scripts/preview_gm_live.py --port 8767 --pvf <asset-directory>/Script.pvf --index-dir <index-directory> --sha256 <approved-pvf-sha256>
```

打开 http://127.0.0.1:8767/console/#players ，管理密码为 `preview`。选择 **晨雾 → 晨雾·剑影**，即可查看有内容的背包、穿戴、仓库、账号资源、邮箱和游戏任务。物品目录和任务库使用完整索引，邮件与福利操作写入这套示例数据库。

当前可检查账号/角色检索、资源分页及详情、PVF 物品说明、邮件预览/发放、两种福利范围、运营作业和主题切换。登录器公告页需要配套 Hub；此独立预览只启动 GM。

停止预览：

```text
python scripts/preview_gm_live.py --stop
```

预览最长运行 12 小时，退出时清理临时数据库容器。重启后重置示例数据。进程状态和日志在 runtime/preview/gm-live，浏览器检查和截图在 runtime/verification/live-preview。
