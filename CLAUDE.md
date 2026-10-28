# go_feed

中文独立博客的**发现与阅读**工具：一个多用户 RSS 阅读器，加上「中文独立博客池」搜索和 chatbot 推荐。这是作者用来申请日本 Go 后端实习的作品集项目，也是作者自己日常会用的工具。

## 当前状态与目标

- 现状：目标结构的 account、subscription、entry、worker 已完成（handler/service/repo 分层 + 写入扩散）；search、chat、博客池还没做。
- 目标：按下方「目标结构」继续实现剩下的模块，**以本文件为准**。
- 架构讲解（给作者复习用）：`docs/learning/project-overview.html`

## 技术栈（已定，不要替换）

- 后端：Go + Gin + GORM + MySQL 8，迁移使用 goose SQL（`db/migrations/`），不使用 AutoMigrate
- 前端：React + Vite + TypeScript，保持简单，不追求功能多
- 大模型：通过 `LLMClient` 接口调用，默认实现为 DeepSeek（兼容 OpenAI 格式）
- 部署：一台 VPS + Docker Compose + Caddy；不使用 Redis、消息队列或 Elasticsearch

## 目标结构

```
frontend/                 React 页面
backend/
  cmd/api/  cmd/worker/   两个程序入口
  internal/
    <模块>/               account、subscription、entry、search、chat
      entity.go handler.go service.go repo.go
    llm/                  LLMClient 接口、DeepSeek 实现、测试用的 Fake
    fetcher/              抓取 RSS、Discover、HTML 清理、SSRF 防护
    worker/               调度、有界并发、按域名限流
    middleware/           登录校验、Origin 校验、限流
    httpx/                handler 共用的小工具（Fail、ParseID、UserID）
    http/router.go        路由
    db/                   连接和迁移
docs/
```

分层规则：handler 只负责解析请求和返回响应；业务规则写在 service；**只有 repo 能访问数据库**。

## 数据模型要点

- `feeds`：一个博客只存一行。`in_pool` 表示属于博客池；有订阅者的每 6 小时抓一次，只在博客池里的每天抓一次。
- `entries`：所有人共享，完整保存正文。全文索引（ngram）只建在 `search_text`（标题 + 摘要）上。
- `user_entries(user_id, entry_id, read_at, starred_at)`：写入扩散。新文章写入时，为每个订阅者插入一行；新订阅时，按**发布时间**取最近 20 篇。
- 博客池数据来自 `timqian/chinese-independent-blogs`（MIT），README 中须注明出处。
- chatbot 对话持久化：`conversations(id, user_id, title, …)` 和 `messages(id, conversation_id, role, content, tool_calls JSON, …)`。工具调用消息必须原样保存；每次只把最近 20 条消息发给模型；流式输出中途断开时，保存已生成的部分并标记为未完成。

## 必须遵守

- 所有读写用户数据的查询都必须带 `user_id` 条件，防止越权。
- 抓取外部 URL 一律经过 `fetcher`（SSRF 防护、超时、大小上限），不要另外直接发 HTTP 请求。
- 大模型推荐的博客必须先经过 `Discover` 验证才能展示；`subscribe` 这类会修改数据的工具，必须由用户确认后才执行。
- 密钥只从环境变量读取，不写进代码，也不提交 `.env`。

## 第一版不做

文件夹、抓取原网页全文、向量检索、真正部署到 AWS（只写扩展设计文档）。

## 常用命令

```sh
docker compose up --build        # 本地启动全部服务：页面和 API 都在 :18080（Go 提供前端），MySQL :3308
cd backend && go test -race ./...   # 测试（集成测试需要 Docker；-short 跳过）
cd backend && go vet ./...
```

## 与作者协作

- 作者的开发经验有限，目标是**能在面试中讲清楚这个项目**。每次改动完成后，用中文简要说明：改了什么、为什么这样设计、面试可能被问什么。
- 面试要点追加到 `docs/interview-notes.md`；重要概念更新到 `docs/learning/` 里的讲解页面。
- 开发中遇到的真实问题（bug、踩坑、和预期不符的数据）记到 `docs/dev-problems.md`，格式：现象 → 原因 → 解决 → 怎么验证 → 学到了什么，最新的写在最前面。
- 不要自行 commit 或 push，由作者确认后再提交。
- 做设计决策时，给出一个推荐方案和理由，不要罗列一堆选项。
- 写代码时遵循 karpathy-guidelines：先说出假设、只写必要的代码、改动只限于任务本身、每一步都要有验证方法（修 bug 先写复现测试，重构前后测试都要通过）。
- 路线图和当前进度见 `docs/rss-reader/plan.md`。
