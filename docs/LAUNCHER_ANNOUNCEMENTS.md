# 登录器公告

在私有 Hub 的 `/gm/console/#gateway` 打开“网关与更新 → 登录器公告”。可新增、编辑、预览、删除和调整顺序，分类沿用登录器的“综合、更新、指南”。标题、显示日期和正文均为文本；正文保留换行。

编辑后点击“发布公告”保存。上移、下移会立即发布新的顺序，删除需要确认；预览与取消删除保持已发布内容。多个窗口同时编辑时，旧窗口收到冲突提示并保留草稿，通过“重新载入”获取最新公告。

登录器在检查更新时读取签名元数据中的公告，验签后更新列表和详情，并保存本地签名缓存。重启可显示已有缓存；下次成功检查更新后获取最新内容。页面加载与主动重新载入触发管理端读取，空闲时没有公告轮询。

## 接口

| 入口 | 用途 |
| --- | --- |
| `GET /admin/api/state` | 私有 Hub 会话读取 `published.news`、`newsRevision` 和 `csrf` |
| `PUT /admin/api/news` | 私有 Hub 发布完整公告数组，保留现有接口格式 |
| `GET /launcher/v2/bootstrap.json` | 玩家读取 RSA-PSS 签名元数据，其中包含 `news` |

管理页面使用 Hub 会话及 `X-CSRF-Token`。发布时同时发送 `If-Match: "<newsRevision>"`，修订值由公告内容和顺序计算，与客户端版本发布的序号独立。

请求体示例：

```json
[
  {
    "title": "版本更新",
    "body": "本次更新内容\n第二行说明",
    "category": "更新",
    "date": "10 / 15"
  }
]
```

- 最多 30 条公告；标题必填，最多 160 个 UTF-8 字节；正文最多 12000 字节，日期最多 32 字节。完整 JSON 请求体上限为 128 KiB。
- 内容变化且修订匹配时，持久保存数组并递增签名发布序号，返回 `{"ok":true,"newsRevision":"..."}`。
- 重复提交当前内容返回成功，序号保持，包括响应丢失后携带旧修订值重试的情况。
- 修订已过期且提交内容不同，返回 HTTP 409；保存失败返回 HTTP 500，并恢复当前内存中的发布内容。
- 空数组 `[]` 清空公告；`null`、未知字段、非法分类和超限内容返回 HTTP 400。

此候选的 GM 页面与 Hub 配套使用，Hub 提供 `newsRevision`。原 `/admin/` 公告编辑器也已发送修订值；既有调用方仍可沿用不带 `If-Match` 的完整数组接口。公开监听提供签名读取，管理操作位于私有监听。

## 验证

从项目根目录执行：

```text
python scripts/verify_component.py gateway
python scripts/verify_component.py launcher --dotnet <dotnet8>
python scripts/verify_integration.py announcements
python scripts/verify_integration.py news-interop --dotnet <dotnet8>
```

浏览器测试使用实际 Hub 和内嵌工作台，验证发布、多窗口冲突、排序、删除、预览不提交、暖色主题、移动布局与空闲请求数。跨语言测试从真实 Go 公告接口发布，经 TLS 固定证书和 RSA-PSS 验签进入 C# 登录器状态，并验证重启后的缓存读取。结果与截图保存在 `runtime/verification/`。
