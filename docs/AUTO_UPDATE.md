# 登录器自动更新

设置中的“自动更新”默认开启。选项保存在本机，旧版本明确关闭的选择继续保留。

启动时自动识别包含非空 `DNF.exe` 和 `Script.pvf` 的客户端目录，并保存识别结果。已有有效选择优先保留；未设置或旧目录失效时，检查便携 EXE 所在目录、完整安装根目录、当前工作目录及其 `client` 子目录，再尝试配置中的默认目录。拒绝包含链接的候选路径。旧版资源也能被识别，以便随后更新；启动游戏前仍执行完整版本校验。

便携入口会将自身位置传递给解包后的更新核心。该行为需要新版便携 EXE；旧便携入口只更新内部 app 时无法补传原始位置，已保存的客户端目录继续有效。

- 登录器启动、开启自动更新、选择客户端目录后，执行一次自动更新流程。
- 先获取本服签名发布信息；若有新版登录器，下载完整 app 包并交给独立助手替换、重启。新版启动后继续处理客户端更新。
- 登录器已是最新版时，检查所选目录中的客户端文件，自动下载并安装已发布的更新。尚未选择客户端目录时提示选择目录，仍可更新登录器本体。
- 关闭自动更新后，可使用“检查更新”“安装更新”和“更新登录器并重启”。登录前的客户端兼容校验继续执行。

客户端文件包括 `DNF.exe`、`Script.pvf`、`dstr.dat` 和清单允许的 `ImagePacks2/*.npk`。沿用后台配置并批准发布的外部内容源；仅配置来源不会自动采用尚未批准的上游版本。登录器本体来自本服签名的完整 app 包。

自动和手动更新共用签名、版本序号、文件大小、摘要、路径校验及原子替换流程。游戏运行时停止更新，下载可暂停；客户端安装失败沿用事务恢复。登录器替换失败后保留旧版及失败提示，下次启动等待手动重试，避免反复自动重启。

## 实现与验证

- `launcher/Engine/LauncherEngine.cs`：自动更新顺序与共享操作锁，元数据读取、客户端计划和安装共用实现。
- `launcher/Engine/ClientDirectoryDiscovery.cs`、`launcher/Portable/Program.cs`：入口位置传递、有效客户端目录识别及持久化。
- `launcher/Host/Program.cs`：`autoUpdate` 请求及完成后的 `restart` 回执。
- `launcher/Flutter/lib/`：默认开启的开关、启动/目录选择触发、重启处理；磁盘继续使用旧字段 `autoCheck` 以保留用户选择。
- `launcher/EngineTests/AutomaticUpdateChecks.cs`：默认值、关闭持久化、关闭时无网络操作、自更新优先于缺失目录/基线。
- `launcher/EngineTests/Integration.cs`：隔离 HTTPS 内容源的手动与自动 EXE/PVF 安装、重复执行和未管理文件保留。
- `launcher/Flutter/test/preview_test.dart`：开关、自动触发、错误显示、重启请求和手动更新入口。

运行 `scripts/verify_component.py launcher`、`scripts/verify_component.py flutter` 和 `scripts/verify_integration.py interop`，通过参数指定本机 .NET 8 与 Flutter。原生玩家界面的重启及实际游戏使用另行验收。
