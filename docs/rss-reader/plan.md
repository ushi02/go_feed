# 实施路线图

> 2026-10-27 制定，替换 Codex 最初写的计划。设计决策见根目录 `CLAUDE.md`，架构讲解见 `docs/learning/project-overview.html`。
> 原则（karpathy-guidelines）：先写测试兜底再重构；每步只做一件事；修 bug 先写复现测试；不做预先设计。

## 进度

- [x] 第 0 步：安装 skill，更新文档
- [x] M0 安全网（2026-10-27 完成，CI 通过）
- [ ] M1 重构（2026-10-27：第 1、3 步完成，第 4 步待浏览器检查；第 2 步无需操作，代码里已没有 Redis；2026-10-28：第 5 步完成，待浏览器检查）
- [ ] M2 博客池和搜索（2026-10-28：第 1～4 步完成；M1-3 漏删的「跟随重定向改 URL」也在这一步删掉了）
- [ ] M3 chatbot
- [ ] M4 上线和包装

## 路线图

每一步的格式是「做什么 → 怎样算完成（验证方法）」。

### 第 0 步：安装 skill，更新文档
1. 把 `SKILL.md` 安装到 `~/.claude/skills/karpathy-guidelines/SKILL.md` → 验证：重启后出现在 skill 列表里
2. 在 `CLAUDE.md` 的「与作者协作」一节加一行：写代码时遵循 karpathy-guidelines → 验证：人工检查
3. 用本路线图替换 `docs/rss-reader/plan.md` 中 Codex 写的旧计划 → 验证：人工检查

### M0 安全网（约 3 天）
1. 引入 testcontainers-go 的 MySQL 模块，写一条集成测试，覆盖「注册 → 订阅 → 抓取（用 `httptest` 模拟 RSS 服务器）→ 列出未读 → 标为已读」→ 验证：`go test ./...` 通过
2. 加上 `.github/workflows/ci.yml`，执行 `go vet` 和 `go test -race` → 验证：push 到分支后 CI 显示通过
3. 补上单元测试：`allowedIP` 和 `Canonical` 的表驱动测试（`internal/feedfetch/fetch.go`），以及抓取的 304、大小上限、重定向到内网（扩展现有的 `fetch_test.go`）→ 验证：测试通过
   - 需要做的最小改动：让测试可以放行 127.0.0.1，因为 httptest 服务器运行在本机。具体做法是把 IP 检查函数做成可注入的。

### M1 重构（约 2 周，分成独立的小步骤）
1. **只搬目录**：Go 代码移到 `backend/`，`internal/reader/` 按业务拆成 `account/ subscription/ entry/ worker/ middleware/ http/`；`feedfetch` 改名为 `fetcher`；同步修改 Dockerfile 和 compose 中的路径。**不改任何行为** → 验证：M0 的测试全部通过，`docker compose up` 正常运行
2. **删除 Redis**：只保留 `ratelimit.go` 里的进程内限流部分，从 compose 中去掉 redis 服务 → 验证：测试通过
3. **改为写入扩散模型**：先写一个会失败的测试，比如「订阅一个有 30 篇文章的源，未读的应该是**发布时间最新**的 20 篇」，以及「两个用户的已读状态互不影响」。然后修改 `00001_init.sql`：新增 `user_entries`，删除 `read_through_entry_id`、`pending_baseline`、`entry_states`；改写 worker 的入库逻辑、`listEntries`、`markRead`、`updateEntryState`。同时删掉重定向合并源的逻辑（`worker.go` 里 `FinalURL != current.URL` 那一段）。**重写时顺便按 handler / service / repo 分层**（entry、worker 入库逻辑；account、subscription 也一起拆），因为这部分代码反正要重写，先分层再重写等于做两遍 → 验证：新测试通过，原有测试也通过；`repo.go` 以外的文件不再出现 `*gorm.DB` 的查询
4. **修复小 bug，每个都先写复现测试**：
   - 文章列表接口不再返回正文，新增文章详情接口
   - 正文里的链接加上 `target="_blank"`（`content.go`）
   - 侧栏点击「全部文章」时重置筛选条件，手动刷新后自动轮询（`App.tsx`）
   - 新增未读数接口，侧栏显示未读数
   - 取消订阅时保留已收藏的文章（2026-10-27 决定）

   → 验证：测试通过，并在浏览器中手动检查一遍
5. **前端同源**：生产环境由 Go 提供 `frontend/dist`，Origin 校验和限流用的 IP 判断随之修正 → 验证：用 `127.0.0.1` 打开页面也能登录

### M2 博客池和搜索（约 3 周）
1. 导入 `timqian/chinese-independent-blogs` 的 CSV，写入 `feeds`，设置 `in_pool=true`；README 注明数据来源 → 验证：导入后约 1570 行，重复导入不会产生重复数据（实际 1426 行：CSV 共 1569 行，其中 143 行没有 RSS 地址，不导入）
2. Worker 去掉「没有订阅者就不抓取」的限制，按「有没有订阅者」决定抓取间隔（6 小时或 24 小时）；改为 goroutine 池，用 `context` 处理优雅退出 → 验证：一轮全量抓取的耗时和失败率写进日志；用 `-race` 跑测试
3. **停止抓取失效的源**：返回 410 Gone 立刻停止；其他失败连续 10 次（按现有退避约一周）才停止。新增 `feeds.disabled_at` 字段（不能改 `in_pool`，否则重新导入博客池时会被改回来）；用户手动刷新或新订阅时清除这个标记，重新抓一次；侧栏显示「已失效」。之后的搜索和推荐都排除失效的源（2026-10-28 决定）→ 验证：410 立刻停止、第 10 次失败才停止、停止后不再被认领、手动刷新可以恢复，都有对应的测试
4. 新增 `entries.search_text` 字段和 `FULLTEXT ... WITH PARSER ngram` 索引，加上搜索 API 和一个简单的搜索页 → 验证：用一组中文关键词测试，能返回相关文章
5. 定期清理：没有订阅者的源只保留最近 30 篇文章，被收藏的文章不删 → 验证：有对应的测试

### M3 chatbot（约 3 周）
1. 实现 DeepSeek 客户端，以及一个只用于测试的假实现 → 验证：用假实现跑通一次工具调用循环
2. 实现 4 个工具：`list_my_subscriptions`、`search_my_entries`（同时搜博客池）、`discover_feed`、`subscribe`（需要用户在页面确认）→ 验证：测试「大模型推荐了一个不存在的博客，最终不会展示给用户」
3. SSE 流式输出，加上 `conversations` 和 `messages` 两张表的持久化（只发送最近 20 条消息；中途断开时保存已生成的部分）→ 验证：刷新页面后对话还在；流式输出中途关闭页面，已生成的部分被保存
4. 每个用户每天限 50 条消息 → 验证：有对应的测试

### M4 上线和包装（约 2 周）
1. 编写 `docker-compose.prod.yml`：包括 Caddy、HTTPS、MySQL 不对外、`.env` 加 `.env.example`、每天 mysqldump → 验证：在 DigitalOcean 的 Droplet 上能通过域名访问
2. 写英文 README（附日文简介），整理 `docs/interview-notes.md`，写 AWS 扩展设计文档 → 验证：人工检查
3. 缓冲约 2 周

## 可复用的现有代码（不要重写）

- `backend/internal/fetcher/fetch.go`：`Canonical`、`allowedIP`、带 IP 检查的 `client()`、`Discover`、`Hash`、`ResolvePublicURL`
- `backend/internal/fetcher/content.go`：`CleanHTML`
- `backend/internal/worker/worker.go`：`claim`，基于 `SKIP LOCKED` 和租约认领任务，M2 并发化时直接复用
- `backend/internal/entry/handler.go`：游标的编码和解码
- `backend/internal/subscription/opml.go`：OPML 导入和导出
- `backend/internal/middleware/ratelimit.go`：进程内限流
- `backend/internal/db/db.go`：goose 迁移，使用 embed 打包 SQL 文件

## 整体验证方式

- 每一步完成后在 `backend/` 下执行 `go vet ./... && go test -race ./...`，CI 必须通过
- 每个里程碑结束时，执行 `docker compose down -v && docker compose up --build`，从空数据库开始，在浏览器里走一遍完整流程
- 每一步完成后，用中文向作者说明改了什么、为什么这样设计、面试可能被问什么，并更新 `docs/interview-notes.md`
- 不自行 commit，每一步由作者确认后再提交
