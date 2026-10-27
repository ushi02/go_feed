# RSS Reader 文档

从 [架构图](architecture.md) 了解组件与数据流，再读 [实施计划](plan.md) 查看行为规则和接口。浏览器中可直接打开 [HTML 解说页](architecture.html)；[部署说明](deployment.md) 列出本地环境和两种 AWS 方案。

这些文档以当前 Go API、Worker、React 前端和 MySQL 迁移为对象。登录限流在 API 进程内完成；多实例部署时需要重新设计共享限流。
