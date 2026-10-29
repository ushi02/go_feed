# 部署到 AWS EC2（操作手册）

> 2026-10-29 写。账号情况：Free plan，剩余额度 $140，167 天后（约 2027-04-14）到期。
> 每一步后面的「检查」都通过了再做下一步。命令里的 `<...>` 换成你自己的值。

## 选型

| 项目 | 选择 | 理由 |
|---|---|---|
| 区域 | 东京 `ap-northeast-1` | 离你近；面试时说「部署在东京区域」也自然 |
| 机型 | `t4g.small`（ARM，2 vCPU，2GB 内存） | Free plan 账号允许的机型里，2GB 内存档中最便宜的。第三方报告说 t4g.small 的免费试用延长到 2026-12-31（未经 AWS 官方确认），如果属实，这之前的计算费用为 0 |
| 系统 | Ubuntu 24.04 LTS（arm64） | 资料最多 |
| 硬盘 | 20GB gp3 | 镜像、MySQL 数据和 7 天备份足够 |
| 公网 IP | Elastic IP | 机器停止再启动后 IP 不变，DNS 不用改 |

**费用估算：** 公网 IPv4 约 $3.6/月，硬盘约 $2/月，2027 年起计算约 $12～15/月。到期前总共大约 $50～90，在 $140 额度以内。实际花费以 Billing 里的数字为准。

**ARM 不影响这个项目：** `golang`、`node:22-alpine`、`alpine`、`mysql:8.0`、`caddy` 都有 arm64 镜像。Go 用 `CGO_ENABLED=0` 编译，没有 C 依赖。镜像是在服务器上直接构建的，构建出来的就是 ARM 版本。

## 第 0 步：开始之前

1. **把要部署的代码合并到 `main` 并 push。** 生产环境只跑 `main` 分支。现在的工作在 `refactor` 分支上，而且本地比远端多 3 个 commit。
2. **设预算提醒：** Billing → Budgets → 创建一个月预算，比如 $20，超过 80% 时发邮件。Free plan 不会扣你的钱，但额度花得比预想快时，你能早点知道。
3. **域名：** 用 `gugu.ushi02.live`，DNS 在 Cloudflare 里配置，见第 3 步。

## 第 1 步：创建 EC2 实例

控制台切到 **东京区域** → EC2 → Launch instance：

- Name：`go-feed`
- AMI：Ubuntu Server 24.04 LTS，架构选 **64-bit (Arm)**
- Instance type：`t4g.small`
- Key pair：新建一个，下载 `.pem` 文件，放到 `~/.ssh/go-feed.pem`，然后执行 `chmod 600 ~/.ssh/go-feed.pem`
- Network settings → 创建安全组，入站规则只开这三条：
  - SSH (22)：来源选 **My IP**（只允许你自己的 IP 连接）
  - HTTP (80)：来源 `0.0.0.0/0`（Caddy 申请证书和跳转 HTTPS 要用）
  - HTTPS (443)：来源 `0.0.0.0/0`
- Storage：20 GiB，gp3

**不要开 3306。** MySQL 在 compose 里本来就没有对外端口，安全组再挡一层。

## 第 2 步：绑定 Elastic IP

EC2 → Elastic IPs → Allocate → Associate，选择 `go-feed` 实例。

**检查：** 在本机执行 `ssh -i ~/.ssh/go-feed.pem ubuntu@<Elastic IP>`，能登录。

> SSH 突然连不上时，先检查你的 IP 是不是变了（比如换了网络）。到安全组里把 SSH 的来源重新改成 My IP 就行。

## 第 3 步：配置 DNS

域名用 `gugu.ushi02.live`。`ushi02.live` 是在 name.com 买的，但 DNS 交给了 Cloudflare 管理（域名服务器是 `*.ns.cloudflare.com`），所以**记录要在 Cloudflare 里加，在 name.com 里加不会生效**。

Cloudflare 控制台 → 选 `ushi02.live` → DNS → Records → Add record：

- Type：`A`
- Name：`gugu`
- IPv4 address：`<Elastic IP>`
- Proxy status：**DNS only（灰色云朵）**，不要用橙色的 Proxied
- TTL：Auto

为什么要选 DNS only：HTTPS 证书由 Caddy 自己申请和续期，Cloudflare 的代理挡在中间时，申请可能失败；Cloudflare 的代理还可能把咕咕的流式输出攒起来再一次发出。选 DNS only，Cloudflare 只负责把域名解析成 IP，其他和本手册的设计一致。个人网站的记录不用动。

**检查：** 在本机执行 `dig +short gugu.ushi02.live`，返回的是 Elastic IP，**不是** `104.x` 或 `172.6x` 开头的 Cloudflare 地址。**这一步通过之前不要启动 Caddy。** 否则 Caddy 会不停地申请证书并失败，可能碰到 Let's Encrypt 的频率限制，被暂时禁止申请。

**以后想改名字**（比如改成 `feed.ushi02.live`）：在 Cloudflare 里加一条新名字的 A 记录 → 把服务器上 `.env` 的 `DOMAIN` 改掉 → 执行 `docker compose -f docker-compose.prod.yml up -d`，Caddy 会自动为新名字申请证书。旧地址会打不开（除非保留旧记录，再在 Caddyfile 里加一条跳转），用户要在新地址重新登录一次，因为 cookie 是按域名区分的。

## 第 4 步：在服务器上安装 Docker，加 swap

以下命令都在服务器上执行：

```sh
sudo apt update && sudo apt upgrade -y
curl -fsSL https://get.docker.com | sudo sh
sudo usermod -aG docker ubuntu     # 之后不用 sudo 就能执行 docker
exit                               # 重新登录，组权限才会生效
```

重新 SSH 登录，然后加 2GB swap。在 2GB 内存的机器上构建镜像时（npm 和 Go 编译），内存可能不够用，swap 用来兜底：

```sh
sudo fallocate -l 2G /swapfile && sudo chmod 600 /swapfile
sudo mkswap /swapfile && sudo swapon /swapfile
echo '/swapfile none swap sw 0 0' | sudo tee -a /etc/fstab
```

**检查：** `docker run --rm hello-world` 能成功，`free -h` 里显示 Swap 是 2.0Gi。

## 第 5 步：拉取代码

仓库是私有的，服务器需要一个**只读的 Deploy key** 才能 clone：

```sh
ssh-keygen -t ed25519 -f ~/.ssh/github_deploy -N ""
cat ~/.ssh/github_deploy.pub
```

把输出的公钥添加到 GitHub 仓库 → Settings → Deploy keys → Add deploy key，**不要勾选** Allow write access。然后执行：

```sh
cat >> ~/.ssh/config <<'EOF'
Host github.com
  IdentityFile ~/.ssh/github_deploy
EOF
git clone git@github.com:ushi02/go_feed.git && cd go_feed
```

**检查：** `git log --oneline -1` 显示的是 `main` 分支上最新的 commit。

> 为什么用 Deploy key：它只能读这一个仓库。就算服务器被攻破，攻击者也拿不到你 GitHub 账号的其他权限。

## 第 6 步：填写 `.env`

```sh
cp .env.example .env
openssl rand -hex 16    # 执行两次，生成两个密码（只包含字母和数字，可以放进 DSN）
nano .env
```

需要填写：`DOMAIN=<你的域名>`、`MYSQL_ROOT_PASSWORD`、`MYSQL_PASSWORD`。`DEEPSEEK_API_KEY` 先空着。

```sh
chmod 600 .env
```

密码另外保存在你的密码管理器里。

## 第 7 步：启动

```sh
docker compose -f docker-compose.prod.yml up -d --build
```

第一次构建大约要 5～10 分钟。

**检查：** `docker compose -f docker-compose.prod.yml ps`，5 个容器都是 Up，并且 api 和 mysql 显示 `(healthy)`。

## 第 8 步：上线检查

在**本机**执行：

| 检查什么 | 怎么做 | 期望结果 |
|---|---|---|
| HTTPS 证书正常 | 浏览器打开 `https://<你的域名>` | 有锁标志，页面能打开 |
| HTTP 会跳转 | `curl -sI http://<你的域名>` | `308`，`Location` 是 https 地址 |
| MySQL 不对外 | `nc -z -w3 <Elastic IP> 3306 && echo OPEN \|\| echo closed` | `closed` |
| cookie 是 Secure | 浏览器里注册、登录，在开发者工具 → Application → Cookies 里看 | `go_feed_session` 勾选了 Secure 和 HttpOnly |
| 完整流程 | 注册 → 订阅一个博客 → 等几秒 → 文章出现 → 搜索一个关键词 | 都正常 |
| 咕咕的流式输出 | 填好 `DEEPSEEK_API_KEY` 后和咕咕聊一句 | 回答是一段一段出现的，不是等全部生成完才一下子出现。如果是一下子出现，说明 Caddy 的压缩把流式输出攒起来了，要在 `Caddyfile` 里让 `text/event-stream` 不压缩 |

在**服务器上**执行：

| 检查什么 | 命令 | 期望结果 |
|---|---|---|
| Worker 在抓取 | `docker compose -f docker-compose.prod.yml logs worker \| tail` | 有抓取轮次的日志，没有持续报错 |
| 备份生成了 | `ls -lh backups/` | 有当天的 `.sql.gz` 文件，大小是 MB 级别 |
| 内存够用 | `free -h` | available 还有余量 |

都通过以后，把 `plan.md` 里 M4-1 的「待在 Droplet 上验证」改成完成。

## 日常操作

```sh
cd ~/go_feed
git pull && docker compose -f docker-compose.prod.yml up -d --build    # 更新到最新代码
docker compose -f docker-compose.prod.yml logs -f --tail=100 api        # 查看日志
```

**备份放在机器之外：** `backups/` 和数据库在同一块硬盘上，硬盘坏了会一起丢。最简单的办法是定期下载到本机：

```sh
scp -i ~/.ssh/go-feed.pem 'ubuntu@<Elastic IP>:go_feed/backups/*' ~/go_feed-backups/
```

更省心的做法是用 EC2 的 Data Lifecycle Manager，每天给硬盘做一个快照，保留 7 个，费用很低。上线稳定后再做。

## 到期之前（2027 年 4 月初之前）

Free plan 到期，或者额度用完时，账号不会自动转成付费，按我的理解会被关闭，上面的资源也会停掉。这一点要到 AWS 的 Billing 文档里确认。**提前两周决定：**

- **继续用 AWS：** 在 Billing 里升级成 Paid plan，之后按月付费，大约 $18/月。
- **换到 Azure for Students：** 用最新的备份恢复到新机器，再把 DNS 改过去。这一次迁移本身就是一次真实的「备份恢复演练」，面试时可以讲。

在日历上设一个 2027-03-31 的提醒。
