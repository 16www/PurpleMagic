"""Refresh the source-derived current GM route catalog; no network or writes to services."""
import json
from pathlib import Path
import re

ROOT = Path(__file__).resolve().parents[1]
SOURCES = [
    'gateway/internal/services/gmtool/managed.go',
    'gateway/internal/services/gmtool/api_v1.go',
    'gateway/internal/services/gmtool/api_v1_operations.go',
]

def main():
    routes = []
    for source in SOURCES:
        for number, line in enumerate((ROOT/source).read_text(encoding='utf-8').splitlines(), 1):
            match = re.search(r'HandleFunc\("(GET|POST|PUT|DELETE|PATCH|HEAD) ([^\"]+)"\s*,\s*(.*)', line)
            if match:
                method, path, handler = match.groups()
                routes.append({'method':method,'path':path,'handler':handler.rstrip(')'),
                               'source':source,'line':number,'surface':'private-management'})
    routes.sort(key=lambda r:(r['path'],r['method']))
    assert len({(r['method'],r['path']) for r in routes}) == len(routes)
    payload={'schemaVersion':1,'scope':'Literal current ManagementHandler and v1 registrations; excludes legacy gmtool routes and runtime handler conditions.','routes':routes}
    (ROOT/'docs/management-routes.json').write_text(json.dumps(payload,ensure_ascii=False,indent=2)+'\n',encoding='utf-8')
    body=['# 当前接口目录','','由`python scripts/catalog_management_routes.py`从ManagementHandler、v1Routes、v1OperationRoutes提取。仅登记当前注册的HTTP方法和路径；字段schema、资格和部署状态另见对应功能文档。','',
          '当前私有管理注册共'+str(len(routes))+'条。副职业路由存在，验收仍未完成。旧gmtool.go中的旧接口未纳入当前管理入口。','',
          '## 私有GM接口','','| 方法 | 路径 | 处理器 | 来源 |','| --- | --- | --- | --- |']
    for r in routes:
        handler=r['handler'].replace('|','&#124;')
        body.append(f"| {r['method']} | `{r['path']}` | `{handler}` | [{Path(r['source']).name}:{r['line']}](../{r['source']}#L{r['line']}) |")
    body+=['','## 玩家入口与Hub','','以下依据`playergateway/main.go`、`launcherhub/admin.go`、`hub.go`和`bundle.go`的条件分派人工核对；不是上述Go注册扫描器的输出。','',
    '| 表面 | 方法/路径 | 用途 |','| --- | --- | --- |',
    '| 玩家 | POST `/api/account/login`、`/api/account/register` | 白名单代理到游戏账号API |',
    '| 玩家 | GET `/api/status` | 游戏状态白名单代理 |',
    '| 玩家 | GET/HEAD `/launcher/v2/bootstrap.json` | 签名更新与公告元数据 |',
    '| 玩家 | GET/HEAD `/launcher/v2/bundles/{sha256}.zip` | 已缓存安装包 |',
    '| 玩家 | GET/HEAD `/launcher/*`白名单文件 | 兼容旧签名更新文件；精确文件正则见main.go |',
    '| Hub私有 | POST `/admin/api/login`、`/admin/api/logout` | Hub管理会话 |',
    '| Hub私有 | GET `/admin/api/state` | 发布/源/公告状态及修订 |',
    '| Hub私有 | PUT `/admin/api/source` | 更新源配置 |',
    '| Hub私有 | POST `/admin/api/check`、`/admin/api/publish` | 检查并发布 |',
    '| Hub私有 | PUT `/admin/api/news` | If-Match+CSRF公告更新 |',
    '| Hub私有 | POST `/admin/api/bundle/{version}` | 安装包上传 |','',
    'Hub与GM有独立会话处理；`/console/`公告页面整合尚未完成。公开玩家路径与私有管理路径分属不同监听。','',
    '## 网关到游戏服务','',
    '`POST /api/game/admin`为网关内部配置的运行适配器，版本1 JSON，Bearer鉴权。现有管理使用online、online_snapshot和catalog；详细字段见[协议](../gateway/internal/modules/dnf/adminapi/protocol.go)。跨进程编辑锁、通关采集、会话释放等新动作尚需定义并实现。','',
    '## API完善工作','',
    '本目录是当前实际路由清单，不是完整OpenAPI规范。下一轮统一请求/响应schema、错误码、权限、能力协商、批次限制及幂等重放语义，并以实际接口生成客户端契约。']
    (ROOT/'docs/API_CATALOG.md').write_text('\n'.join(body)+'\n',encoding='utf-8')
    print(f'{len(routes)} current private GM routes cataloged')

if __name__=='__main__':
    main()
