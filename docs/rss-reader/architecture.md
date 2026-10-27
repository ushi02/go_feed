# 架构与数据流

## 组件

```mermaid
flowchart LR
  U[浏览器] --> UI[React + Vite]
  UI --> API[Go API]
  API --> DB[(MySQL)]
  W[Go Worker] --> DB
  W --> SITE[外部 RSS / Atom / JSON Feed]
```

MySQL 保存用户、会话、订阅源、文章和用户阅读状态。一个源只抓取一次；用户通过订阅关系看到共享文章。API 不承担定时抓取，Worker 不直接响应浏览器。登录限流目前保存在 API 进程内。

## 添加订阅

```mermaid
sequenceDiagram
  participant B as 浏览器
  participant A as Go API
  participant D as MySQL
  participant W as Go Worker
  participant S as 外部网站
  B->>A: 输入博客首页，发现 Feed 候选
  A->>S: 安全请求，解析 alternate 链接
  A-->>B: 返回候选地址
  B->>A: 选择地址并订阅
  A->>D: 建立共享源与用户订阅，立即到期
  W->>D: 认领到期源并获得租约
  W->>S: 条件请求 RSS/Atom/JSON Feed
  W->>D: 去重入库，设置首次未读基线
  B->>A: 查询文章
  A->>D: 按订阅和用户状态查询
  A-->>B: 文章、已读状态和游标
```

多个 Worker 用 MySQL `FOR UPDATE SKIP LOCKED` 和租约协作。抓取在事务外完成，写回前核对租约；文章唯一索引使重试安全。源站请求有大小、时间和并发上限，并在每次连接时拒绝私网地址。

## 阅读状态

每条用户订阅保存 `read_through_entry_id`。小于等于水位的文章默认已读，大于水位的文章默认未读；`entry_states` 只保存用户主动改变的例外和收藏。新订阅已有源时，最近 20 篇默认未读；新源首次抓取后设定同样的基线。批量标已读推进水位，不为每篇文章插入一行。
