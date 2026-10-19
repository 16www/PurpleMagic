# 2026-10-20 公开源码同步

本次同步纳入当前兰紫 GM Console、真实 Go v1/Hub 接入、外部 GM/PVF 服务、持久发放队列、自动福利、角色编辑保护，以及登录器 0.2.16 的精确发行源码增量。

## 版本与入口

- 当前前端：[`gm-console/`](../gm-console/README.md)，打包输入为 `gm-console/dist/`。
- 认可设计：[`purplemagic-orchid-20261019`](design/purplemagic-orchid-20261019/README.md)；冻结的四份样式与当前对应文件逐字节核对。
- 登录器：0.2.16，来源提交与八个文件的原始 SHA256 见[导入清单](launcher-0.2.16-import.json)。
- 架构：[网络、接口与生成器契约](architecture/NETWORK_AND_CONTRACTS_20261019.md)，配套独立 HTML 网络图和模块图。
- 第一版 `gm-web/` 保留历史源码，标记为 `FROZEN_DO_NOT_REFERENCE`。

## 本次重复验证

| 范围 | 结果 |
| --- | --- |
| 新前端 | 26 项测试通过；TypeScript 检查、Vite 生产构建和静态包生成通过 |
| Go 模块 | `gateway`、`gm` 的 list/test/vet 通过 |
| 数据库 | 两个模块各自新建的一次性 MariaDB 回归通过 |
| 依赖边界 | 仓库边界与 GM 独立依赖检查通过 |
| 双平台构建 | PlayerGateway、GMService、GMIndex、GM 工具的 Windows/Linux 构建通过 |
| C# 登录器 | Core、EngineTests、PortableTests 通过，使用 .NET 8 |
| Flutter | pub get、analyze、test 通过；美术文件作为本地构建输入提供 |
| 发布内容 | 网页 ZIP CRC/manifest、导入摘要、设计样式和文档链接核对 |

本轮验证针对公开源码工作树。此前页面交互和隔离数据库联调的范围见 [GM Console 检查记录](../gm-console/design-qa.md)。

## 公开内容处理

公开部署示例使用 `example.com` 域名和文档保留地址；运行配置、实际服务凭据、个人目录、现场截图与部署回执保留在部署环境。检查覆盖工作树、暂存内容及待发布提交的可达历史，并与受保护配置中的凭据做精确匹配。负向测试所需的虚构凭据、地址与配置占位符保留。

源码和文档按一个公开快照提交，同步不包含内部开发分支的提交历史。第三方来源及许可证继续保留在各组件声明中。
