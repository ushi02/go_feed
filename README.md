# Go Feed Reader

一个 Go + React 的多人 RSS 阅读器。共享抓取 RSS、Atom 和 JSON Feed；每个用户独立管理订阅、文件夹、已读和收藏。

## 本地运行

需要 Docker Compose。首次启动运行：

```bash
docker compose up --build
```

打开 http://localhost:15173。API 健康检查位于 http://localhost:18080/healthz。可用 `WEB_PORT` 和 `API_PORT` 覆盖主机端口。Go API 和 Worker 启动时自动执行 `backend/internal/db/migrations` 中的 goose SQL 迁移。新订阅通常在几秒内开始抓取；前端会自动刷新等待中的订阅。正常更新间隔为六小时。

不使用 Docker 开发前端时：

```bash
docker compose up -d mysql
cd backend && go run ./cmd/api
cd backend && go run ./cmd/worker
cd frontend && npm install && npm run dev
```

后两条 Go 命令需要在不同终端运行。Vite 会把 `/api` 代理到本机 `:8080`。

配置：`MYSQL_DSN` 覆盖数据库连接；`APP_ORIGIN` 设置允许的浏览器来源（本地默认 `http://localhost:5173`）；HTTPS 部署时设置 `COOKIE_SECURE=1`。不要把生产密码放进 Compose 示例。

验证代码：`cd backend && go test ./...`、`cd backend && go vet ./...`、`cd frontend && npm run build`。Compose 启动后可运行 `python3 scripts/smoke.py` 验证 API 与 MySQL；`API_URL=http://127.0.0.1:15173 python3 scripts/live_feed_smoke.py` 会访问公开的 Go 博客，验证自动发现与实际抓取。集成脚本会临时创建账号，并可能受本地注册限流影响。

项目的架构图、完整计划、独立 HTML 解说和两种 AWS 部署方案见 [docs/rss-reader/README.md](docs/rss-reader/README.md)。
