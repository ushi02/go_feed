# 本地与云部署

## 本地

根目录 `docker-compose.yml` 运行 MySQL、Go API、Go Worker 和 React 开发服务器。浏览器默认通过 `http://localhost:15173` 访问，API 健康检查位于 `http://localhost:18080/healthz`；Vite 在容器内把 `/api` 代理到 Go。可用 `WEB_PORT` 和 `API_PORT` 改主机端口。数据库迁移随 API/Worker 启动执行。`MYSQL_DSN`、`APP_ORIGIN` 和 `COOKIE_SECURE` 是主要配置项。

## 托管 AWS 架构（设计方案）

React 静态构建产物放入私有 S3 Bucket，经 CloudFront 提供 HTTPS；`/api` 路径转发到 Application Load Balancer 后的 ECS Fargate API 服务。Worker 运行在独立 ECS 服务中，从 RDS for MySQL 认领待抓取源。数据库放在私有网络；API 与 Worker 使用最小权限的任务角色，密钥通过托管密钥服务注入，日志进入 CloudWatch。需要为 Worker 的外部 RSS 请求规划受控出站网络。数据库开启备份，并对抓取失败、任务退出和 API 错误设置告警。部署多个 API 实例时，还需要将登录限流改为跨实例共享的方案。

此方案适合展示服务分离、托管数据库与扩容能力。ALB、NAT、Fargate、RDS 都有持续费用；金额依赖地区、规格和流量，应在实际部署前按目标地区估算。

## 个人低成本架构（设计方案）

一台 Lightsail 或 EC2 虚拟机运行 Compose 中的 API、Worker 和 MySQL，前端仍可由 S3 + CloudFront 提供。可以按需省去负载均衡器；代价是单机故障会影响所有服务，数据库备份、系统升级和恢复演练需自行负责。该方案适合个人试用和作品展示，不声称高可用。

两套 AWS 方案目前都是文档设计，没有创建云资源。源站 URL 属于不受信任输入；无论采用哪套部署，都必须保留应用层的私网地址拦截、请求超时及响应大小限制。
