# 面试要点

按里程碑追加。每条写成「可能被问的问题 → 我的回答要点」。

## M0 测试安全网

**Q：为什么重构之前先写测试？**
重构的定义是不改变行为，只改结构。没有测试，就无法证明行为没变。所以先写一条端到端的集成测试，覆盖核心流程（注册 → 订阅 → 抓取 → 列出未读 → 标为已读），重构前后都必须通过。

**Q：集成测试为什么用真实的 MySQL，而不是 mock 或 SQLite？**
业务依赖 MySQL 特有的语法，比如 `SELECT ... FOR UPDATE SKIP LOCKED`、`ON DUPLICATE KEY`，以及多表 `DELETE ... JOIN`。换成 SQLite 或 mock，这些行为就测不到了。testcontainers-go 会在测试开始时启动一个临时的 MySQL 8 容器，测试结束后自动删除，每次都从空库开始，测试之间互不影响。

**Q：集成测试怎么测 Worker？Worker 是一个死循环。**
在 goroutine 里启动真正的 `Worker.Run(ctx)`，测试端每 200ms 轮询一次 `/api/entries`，最多等 15 秒；等到文章出现后 `cancel()` 掉 context，再等 goroutine 退出。没有为了测试给生产代码加 `RunOnce` 之类的方法。

**Q：SSRF 防护会拦住 127.0.0.1，测试怎么连本机的假 RSS 服务器？**
把 IP 检查函数做成了包级变量 `feedfetch.IPAllowed`，测试里替换成放行本机的版本，用 `t.Cleanup` 恢复原值。生产代码行为不变。取舍：可以修改的全局变量不算优雅，但这是改动最小的做法；如果以后需要并行测试，再改成 `Fetcher` 结构体，通过字段注入。

**Q：怎么证明重定向到内网会被拦截？**
测试里的服务器返回 302，跳转到 `http://169.254.169.254/`（云服务器的元数据地址，SSRF 最常见的攻击目标）。IP 检查发生在拨号（`DialContext`）阶段，每次建立连接都会检查，所以重定向之后的新连接同样会被拦截。这也能防 DNS rebinding，因为检查的是解析后的实际 IP，而不是 URL 里的主机名。

**Q：怎么知道测试本身是有效的？**
故意把代码改坏（比如让 mark-read 什么都不做），确认测试会失败，然后再恢复代码。

## M1-1 按模块拆包

**Q：怎么保证「只搬目录」真的没改行为？**
M0 的集成测试只通过 HTTP 接口操作，所以拆包前后可以一字不改地运行（只改了 import 路径）。拆完之后，`go test -race`、`docker compose up --build` 和旧的 `scripts/smoke.py` 全部通过。

**Q：Go 的包之间不能循环引用，你是怎么拆的？**
先确定依赖方向：`http`（路由）→ 各业务模块 → `httpx`（共用小工具），`worker` → `subscription`、`entry`、`fetcher`，`entry` → `subscription`。原来 `subscription` 计算 baseline 时引用了 `Entry` 类型，会形成 `entry ↔ subscription` 循环，于是改成直接写表名 `Table("entries")`，生成的 SQL 完全一样。`Fail`、`UserID` 这类每个 handler 都要用的函数，放进一个不依赖任何业务包的「叶子包」`httpx`。

**Q：为什么 `go.mod` 放进 `backend/`，模块路径变成 `.../go_feed/backend`？**
前端和后端在同一个仓库里（monorepo），Go 代码只在 `backend/` 下。Go 规定模块路径要和所在目录对应，所以改成 `github.com/ushi02/go_feed/backend`。CI 和 Docker 构建都以 `backend/` 为工作目录。

**Q：为什么这一步不顺便拆 service 和 repo？**
一次只做一件事。如果搬目录和改分层混在一起，测试一旦失败，就很难判断是哪一步出的问题。先搬目录，测试通过后单独提交，再做下一步。
