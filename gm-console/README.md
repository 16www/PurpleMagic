# PurpleMagic GM Console

轻量 React + TypeScript + Vite 前端。视觉基于 dforigin.online 实际页面的 CSS 变量、玻璃材质与悬浮导航；功能依据背包编辑器截图 1–8（含 2-1）和 86jp 操作语义重新组织。

真实API联调：<http://127.0.0.1:8779/>。独立视觉演示：<http://127.0.0.1:8778/>。

当前主题为稍偏粉的兰紫（Orchid）：深色「兰紫夜幕」、浅色「兰紫晨光」。用户已认可现有布局，品牌配色集中在 `src/purple.css`，与原站 CSS 来源层分开维护。

```bat
npm ci
npm run dev
npm run build
npm test
```

Node.js 24.15.0 已验证。Node 只承担开发与构建，`dist/` 是静态资源。运行 `../preview-gm-console.bat` 也可启动。

七个入口：查询、背包、邮件、任务、GM、账号与角色、公告与下载源。背包包含物品栏、穿戴栏、宠物栏、仓库、账号仓库、宠物、时装、徽章。

development演示模式使用独立本地演示状态，存储键 `purplemagic-glass-preview-v1`；外观偏好为 `purplemagic-glass-appearance-v1`。演示包括批量账号解析、单次确认、背包属性保存、邮件与回执、任务、成长/货币、福利启用发放、公告与源配置。可在外观设置中重置演示数据。

生产构建默认接入同源 Go v1/Hub API，API客户端位于 `src/api/`。背包、邮件、任务、成长、资产、自动福利及公告/下载源/游戏连接的接入范围、启动方式与恢复语义见[真实接入说明](../docs/GM_CONSOLE_INTEGRATION_20261019.md)。演示执行器 `src/domain.ts` 只用于8778独立演示。

- [检查与验证](design-qa.md)
- [来源](THIRD_PARTY_NOTICES.md)
- [原始资源摘要](reference-manifest.json)

第一版 `gm-web/` 已封存，禁止作为新界面的样式、布局或组件来源。`gm-web-next/` 为历史 86jp 移植候选，不是本版视觉模板。
