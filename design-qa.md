# FROZEN_DO_NOT_REFERENCE — archived first frontend

Current validation: [GM Console](gm-console/design-qa.md). The record below describes the archived `gm-web/` and is not a design baseline.

---

# 侧栏布局对照验收

- source visual truth: `../cpamp-reference-20261015/img/dashboard-zh.png`，固定提交 `9d719fc8cca362f7b56d294a2ad056ae99cfeba6`。
- implementation: `http://127.0.0.1:8765/`；截图 `runtime/verification/console-layout/overview-cpamp-layout.png`。
- viewport: 1917 × 992，Chromium，浅色主题，中文，概览页，侧栏展开。
- full-view comparison: `runtime/verification/console-layout/comparison.png`。
- focused region comparison: `runtime/verification/console-layout/shell-comparison.png`，两侧各 360 × 650，核对侧栏、顶栏、导航和标题。
- 本轮检查页面布局、信息层级与密度。

## Findings

当前对照无待修复 P0/P1/P2 项。

| 检查面 | 结论与依据 |
| --- | --- |
| 字体 | 系统无衬线字体；CDP 读回中文标题和导航实际为 Microsoft YaHei UI。导航 14px、卡片标题 16px，信息层级清楚。 |
| 间距与布局 | 210px 侧栏、54px 折叠态、50px 顶栏、20px 桌面留白；分隔导航、紧凑工具栏和卡片与参考布局对应。 |
| 颜色 | 使用 统一表面与主题色彩变量，当前为蓝色浅色方案。十套主题、极端自定义强调色的抽样文字对比度均达到 4.5:1。 |
| 图像与图标 | 复用当前 SVG 功能图标；PurpleMagic 使用文字名称。GM 概览使用服务、任务和运营数据，卡片数量按实际内容安排。 |
| 文案 | 区服装饰卡、侧栏介绍和首页宣传块已移除。演示在顶栏标注一次；表单保留输入错误和操作结果。 |

## Comparison history

1. 用户指出布局沿用旧 GM、装饰性区服卡没有用途，且文案反复解释演示限制。按参考截图重排公共页面框架，删除相应组件与段落。
2. 浏览器发现窗口切到 390px 时，内容区域的 margin-left 动画造成短暂横向溢出。取消框架宽度动画；同一场景复验通过。
3. 在 1917 × 992 捕获修订稿，与参考图合成全页及局部对照后检查上述五项。完整截图与局部对照均已打开检查；全页与侧栏局部无额外修订。补查移动端发现表格沿用 18px 负边距，与新的 14px 卡片内边距不一致；统一为 14px 后，375px 视口下表格右缘 362px、卡片右缘 363px。修订证据为 `runtime/verification/console-layout/overview-mobile-final.png`，已打开检查；七页四档宽度复验通过。

账号页复查：将表格下方独占一行的“重置查询”移到查询工具栏，删除空说明容器。`players-cpamp-layout.png` 已重新捕获并打开检查；筛选后重置恢复五条当前页记录，七页回归通过。

## Implementation checklist

- [x] 七页布局与 375/768/1440/1920px 响应式检查。
- [x] 侧栏 210/54px 折叠、展开和移动抽屉。
- [x] 账号查询、分页、选择角色、邮件草稿与更新源草稿。
- [x] 十套主题配色、明暗模式、偏好保存与键盘焦点。
- [x] 查询接口浏览器场景与真实 Go HTTP/一次性 MariaDB 联调。
- [x] 浏览器 pageerror 为零；完整主题/演示回归无失败 HTTP 响应。


## 主题修正复验

用户已确认布局。后续截图指出暖色主题的表头与次级底色残留深蓝，本轮修正主题适配。

- 来源：用户 `账号与角色 · So.png`，1873 × 927，暖色深色主题；原次级底色为 rgb(30, 48, 71)。
- 实现：`runtime/verification/console-theme-fix/ember-after.png`，1873 × 927，ember-dusk 深色模式。
- 全页对照：`runtime/verification/console-theme-fix/user-comparison.png`；局部：`header-comparison.png`。
- [P2 已修复] 固定的次级底色改为主题表面变量，表头及普通物品图标跟随配色。暖色表头现为约 rgb(46, 37, 33)。
- [P2 已修复] 扩大表格检查后发现浅色选中行辅助文字和部分浅色表头不足 4.5:1；使用当前主题的正文文字层级，十套配色及极端自定义颜色均通过。
- 布局、字体、图标、页面文案保持已确认方案。用户截图中的底部独占重置区域在上一轮已合并进查询工具栏。
- 主题选择、重载持久化及动效结束后的颜色已实测；与来源组合的全页和局部截图已打开检查。
- GM list/test/vet、独立依赖边界、Windows/Linux 构建与新旧页面浏览器验证通过。

final result: passed


## 邮件、福利与任务接入

- 新工作台沿用已确认的 页面框架，邮件表单、福利规则表、任务摘要与分页回执统一使用现有卡片、工具栏和主题变量。
- 真实 Go/MariaDB 浏览器截图位于 `runtime/verification/console-backend/`，桌面 1440 px、移动 390 px。已检查邮件/规则表、任务列表和回执长页；移动页面无横向溢出，宽表格在卡片内滚动。
- 首轮检查发现“自动新角色”复选框继承纵向 label 样式，已调整为横向排列。暖色深色截图补验新表单及任务表。
- 交互验证覆盖取消确认、发送后回执、重复保存规则、启用规则、55 个目标分页、10 个一批的继续操作，以及空闲零轮询。

## 登录器公告接入

- “网关与更新”沿用现有页面框架，左侧公告列表、右侧编辑表单，窄屏排列为单列；更新源管理链接保留。
- 已打开检查 `runtime/verification/announcements-baa146258a/browser/` 中的浅色、暖色深色与 390 px 截图：文字和控件可读，配色随主题，移动端无横向溢出。
- 回归补查发现预览按钮默认触发表单提交；显式设置普通按钮类型后，实际 Hub 验证预览及取消删除均保持发布内容和序号。最终操作与截图回执为 `runtime/verification/announcements-02f1730c58/browser/`。

## 真实 PVF 物品详情

- 物品目录沿用现有列表/详情双栏，接入长描述、基础属性、完整职业、套装与分层字段。长说明保留换行；简要效果折叠，详细效果直接展开。
- 已打开检查 `runtime/verification/item-details/browser/` 下的装备浅色、幽魂套装暖色、390 px 移动页面与真实样本预览。长文、属性值和操作可读，移动页面无横向溢出；条件规则与原始字段使用折叠区。
- 真实 Go HTTP/PVF 浏览器覆盖装备、套装、消耗品、同源样本预览、切换页面清空详情和空闲无轮询。原七页演示及新旧 GM 页面回归通过。
