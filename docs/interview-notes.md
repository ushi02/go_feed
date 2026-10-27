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

## M1-3 写入扩散 + 分层

**Q：什么是写入扩散（fan-out on write）？为什么选它？**
新文章入库时，就为每个订阅者在 `user_entries` 里插入一行。读的时候只查自己的行：`WHERE user_id = ? AND read_at IS NULL`。另一种做法是读取扩散：读的时候再去 join 所有订阅源，现算状态。RSS 阅读器读远多于写（一篇文章写一次，会被打开很多次），而且每个用户的已读、收藏状态本来就需要一行来存，所以选写入扩散。代价是订阅人数很多时，一篇文章要插入很多行。这个项目的用户量下没问题；如果真有百万订阅者的源，可以改成对大源用读取扩散（推拉结合）。

**Q：原来的水位线模型有什么问题？**
原来只给每个订阅记一个 `read_through_entry_id`：id 小于等于它的算已读，再用 `entry_states` 记录例外情况。问题有三个：一是规则复杂，「已读」要靠两张表一起推算；二是 id 的顺序不等于发布时间的顺序，所以「最新 20 篇」实际取的是 id 最大的 20 篇，测试复现了这个 bug；三是之后做「未读数」「按条件搜索我的文章」时，每个查询都要重复这套推算逻辑。

**Q：新订阅为什么只给 20 篇？第一次抓取怎么处理？**
订阅一个写了十年的博客，不应该一下子多出 500 篇未读。规则是：
- 源已经抓取过：订阅时立刻按发布时间（`sort_at`）补最新 20 篇。
- 源还没抓取过：等第一次抓取完成后，给当时所有订阅者各补 20 篇。
- 之后每次抓取：只把**新插入**的文章发给所有订阅者。
用 `feeds.initialized` 区分「第一次」和「之后」。

**Q：订阅和第一次抓取同时发生，会不会漏掉人？**
可能会，所以两边都先 `SELECT ... FOR UPDATE` 锁住 feeds 里的同一行，让它们按顺序执行。InnoDB 在 `INSERT ... SELECT` 时，对源表用的是加锁读，读到的是最新已提交的数据，所以 Worker 拿到锁之后一定能看到先提交的订阅。两边都是先锁 feed 行，加锁顺序一致，所以不会死锁。

**Q：为什么用 `ON DUPLICATE KEY UPDATE`，而不用 `INSERT IGNORE`？**
`INSERT IGNORE` 会把**所有**错误降级成警告，比如外键约束失败也会被悄悄吞掉。`ON DUPLICATE KEY UPDATE read_at = user_entries.read_at` 只对主键冲突做空操作，其他错误照常报出来。

**Q：handler / service / repo 怎么分？事务跨了两个模块怎么办？**
handler 只处理 HTTP（解析参数、把错误映射成状态码），service 写业务规则（「必须是自己的订阅」「已抓取过的源才补 20 篇」），repo 只写 SQL。验收标准是：`repo.go` 以外的文件里不出现任何数据库查询，用 grep 就能检查。订阅时要在同一个事务里写 subscriptions（subscription 模块）和 user_entries（entry 模块），做法是 service 开启事务，再用 `repo.WithTx(tx)` 让两个模块的 repo 都在这个事务里执行。service 只是把 tx 传下去，自己不写 SQL。

**Q：怎么证明新测试真能抓到 bug？**
先在旧代码上跑，「最新 20 篇」那个测试失败了，说明测试能复现 bug。改完以后再做两次变异：把排序改回按 id、让 Deliver 什么都不做，对应的测试都失败了。

## M1-4 小 bug 修复

**Q：为什么文章列表不返回正文？**
一篇正文可能有几十 KB，列表一页 20 篇，可能就有 1MB，而用户只会点开其中几篇。所以列表只返回标题这类元数据，点开时再请求 `GET /api/entries/:id`。详情接口同样只能查到发给自己的文章（`user_entries` 里有自己的行），别人的文章返回 404，防止越权读取。

**Q：取消订阅后，收藏怎么保留？**
取消订阅时，只删除 `starred_at IS NULL` 的 `user_entries`。列表查询原来用 `JOIN subscriptions`，没有订阅就查不到这些行，所以改成 `LEFT JOIN`。按订阅或文件夹筛选时，`s.id = ?` 会自然排除掉这些孤立的行。

**Q：未读数怎么算？**
`SELECT s.id, COUNT(*) FROM user_entries ue JOIN entries e … JOIN subscriptions s … WHERE ue.user_id = ? AND ue.read_at IS NULL GROUP BY s.id`。写入扩散模型的好处在这里体现出来：未读就是「自己的行里 `read_at` 为空」，一条 GROUP BY 就能算出来，不用推算水位线。用户量变大以后，可以给 `(user_id, read_at)` 加索引，或者在订阅表上缓存计数。

**Q：链接为什么要加 `target="_blank"`？会不会有安全问题？**
加上它，点正文里的链接时会在新标签页打开，不会离开阅读器。新标签页可以通过 `window.opener` 反过来操控原页面（这叫 tabnabbing），所以同时加上 `rel="noopener noreferrer"`，由 bluemonday 的 `AddTargetBlankToFullyQualifiedLinks` 负责处理。正文是**入库时**清理好再存的，所以这个修复只对之后新抓取的文章生效，这是「存储时清理」的一个代价。

## M1-5 前端同源

**Q：为什么让 Go 直接提供前端页面，而不是单独起一个前端服务？**
页面和 API 同源（协议、域名、端口都相同）以后，浏览器不需要 CORS，Cookie 也不用设置 `SameSite=None`。部署也更简单：一个镜像里既有 API 又有 `frontend/dist`，Caddy 只需要转发到一个地方。具体做法是 Dockerfile 多阶段构建：Node 阶段 `npm run build`，Go 阶段编译，最后把两者拷进同一个 alpine 镜像。路由里没匹配上的 GET 请求交给 `http.FileServer`；`/api/` 开头的未知路径仍然返回 JSON 404，不会被当成页面。本地开发时仍然用 Vite（有热更新），由 Vite 把 `/api` 代理给 Go，浏览器看到的也是同源。

**Q：Origin 校验原来有什么问题？现在怎么做？**
原来是和环境变量 `APP_ORIGIN`（写死 `http://localhost:15173`）逐字比较，所以用 `127.0.0.1` 打开页面就无法登录，换一个域名也要改配置。现在比较 `Origin` 里的 host 和请求的 `Host` 头：同源请求两者一定相同，CSRF 攻击时浏览器发出的 `Origin` 是攻击者的网站，`Host` 是我们的网站，两者不同就拒绝。不比较协议，因为在 Caddy 后面浏览器用的是 https，而 Go 收到的是 http。

**Q：攻击者能伪造 Host 头吗？**
用 curl 当然可以随便写，但那样的请求不会带上受害者的 Cookie，CSRF 的前提是「浏览器自动带上受害者的 Cookie」，而浏览器发出的 Origin 和 Host 都由浏览器决定，网页脚本改不了。DNS rebinding 的情况下 Host 和 Origin 都是攻击者的域名，但浏览器不会把我们网站的 Cookie 发给那个域名，所以也拿不到登录态。

**Q：限流按 IP 计算，放在反向代理后面会出什么问题？**
在 Caddy 后面，Go 看到的 TCP 对端永远是 Caddy，所有用户会共用一个限流桶，一个人刷登录就会让所有人被限流。真实 IP 在 `X-Forwarded-For` 里，但这个头客户端可以随便写，如果无条件相信，攻击者每次换一个值就能绕过限流。所以用 Gin 的 `SetTrustedProxies`：只有请求来自 `TRUSTED_PROXIES` 里列出的代理时，才读取 `X-Forwarded-For`；默认谁都不信任。两种情况都有测试：经过可信代理时不同客户端各自计数；没配置时伪造的头不起作用。
