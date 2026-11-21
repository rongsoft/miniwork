# Miniwork 数据服务端

给桌面端 `GET /data/hotspots` 接口的实现。当前用**内置语料**跑通全链路，本地已验证通过；部署后把数据源换成易撰 API 即可拿到真榜单。

## 它解决什么

原客户端默认 `source=mock`，数据来自 exe 内置的 30 条语料，刷几次就抽完，内容还和用户人设对不上。本服务端：

- 8 个赛道各有独立语料，且随时间**逐步放量**（默认每 2 分钟露 3 条），刷新能持续看到新内容
- `id` 由「赛道+平台+标题」哈希生成，**同一条内容 id 恒定**，客户端据此幂等去重，不会重复入库
- 完全遵循 `数据接口约定.md`：Bearer token 鉴权、401/402/429 错误码、stale 标志、顶层数组兼容

## 本地运行

`config.json` 是公开模板（token 为占位符）。先复制一份本地配置，把 token 换成自己的随机字符串：

```bash
cd server
cp config.json config.local.json
# 编辑 config.local.json 里的 token
# Go 1.24+ 即可，零第三方依赖
go build -o miniwork-server.exe .
./miniwork-server.exe -config config.local.json
```

`config.local.json` 已被 `.gitignore` 排除，不会误提交。验证（token 换成你设的值）：

```bash
curl http://127.0.0.1:18080/health
curl -H "Authorization: Bearer <你设的token>" \
  "http://127.0.0.1:18080/data/hotspots?niche=美食&limit=10"
```

## 配置（config.json）

| 字段 | 当前值 | 说明 |
|---|---|---|
| `port` | 18080 | 监听端口 |
| `token` | CHANGE_ME_BEFORE_DEPLOY | Bearer token 占位符。**运行前必须改成随机强 token** |
| `refreshMinutes` | 2 | 放量间隔。本地演示用 2 分钟；**生产建议 60**（对齐易撰每小时抓取） |
| `baseItems` | 12 | 每赛道初始露出条数 |
| `stepItems` | 3 | 每轮新增露出条数 |
| `quotaDay` | 0 | 每日请求配额，0=不限。生产按需设置，超限返回 429 |
| `staleAfterMin` | 120 | 预留给易撰适配器的陈旧阈值 |

## 部署到服务器

1. 编译 Linux 版（在开发机上交叉编译，无需安装 Go 到服务器）：

   ```bash
   cd server
   GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -o miniwork-server .
   ```

2. 上传到服务器，例如 `/rongsoft/work/miniwork/data-server/`：

   ```bash
   scp miniwork-server config.json root@<服务器IP>:/rongsoft/work/miniwork/data-server/
   ```

3. 改服务器上的 `config.json`：`token` 换成强随机值（替换占位符 `CHANGE_ME_BEFORE_DEPLOY`），`refreshMinutes` 改 60。

4. 用 systemd 托管（`/etc/systemd/system/miniwork-data.service`）：

   ```ini
   [Unit]
   Description=Miniwork Data Server
   After=network.target

   [Service]
   Type=simple
   WorkingDirectory=/rongsoft/work/miniwork/data-server
   ExecStart=/rongsoft/work/miniwork/data-server/miniwork-server -config config.json
   Restart=on-failure
   RestartSec=5

   [Install]
   WantedBy=multi-user.target
   ```

   ```bash
   systemctl daemon-reload
   systemctl enable --now miniwork-data
   systemctl status miniwork-data
   curl http://127.0.0.1:18080/health
   ```

5. 客户端配置 `%APPDATA%\miniwork\config.json`（Windows）/ `~/.config/miniwork/config.json`（macOS）：

   ```json
   {
     "source": "remote",
     "remote": {
       "baseUrl": "http://<服务器IP>:18080",
       "token": "<服务器上配的 token>"
     }
   }
   ```

   若前面有 nginx 反代，用 HTTPS 域名。改完重启客户端，左下角应显示「线上数据」。

## 换成真数据（易撰 API）

当前 `corpus.go` 的 `buildSnapshot()` 是内置语料。接易撰时只需替换这一个函数：

- 按 8 个赛道定时拉易撰榜单 → 落本地库 → 按 `heat` 排序
- 保持 `id` 稳定（用易撰返回的内容 id，或 sha256(标题)）
- 上游失败时返回 72 小时内的缓存，并把响应里的 `stale` 置 true
- `handler.go` / `store.go` / 客户端都不用改

**商务前置**：易撰 API 的商业授权、按量计费、SLA 三件事谈成之前，不要把它接进商用分发版本（见 `数据接口约定.md`）。

## 测试

```bash
go test ./...    # 4 个用例：放量+幂等、id 稳定、8 赛道非空、排序
go vet ./...
```

## 本次已验证

- `go test` / `go vet` 全过；服务端编译零告警
- 真实 HTTP：正常取数、401 错误 token、400 缺参、关键词搜索、since 过滤、id 幂等
- 随时间放量：美食赛道 21 → 24 条（2 分钟周期）
- 客户端端到端：配置指向本地服务端 → 点「刷新灵感」→ 13 条美食选题落库 → GUI 显示「线上数据」+ 美食卡片
