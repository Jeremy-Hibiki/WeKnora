## Why

WeKnora 已有 Feishu、Notion、Yuque、RSS 四个文档同步连接器，但大量企业内部文档仍托管在 SVN 仓库中（技术文档、规范文件、设计文档等）。这些文档需要手动导出后上传到知识库，无法持续同步。SVN 作为版本控制系统，其原生的 revision + diff 机制可以提供比现有连接器更精确、更简洁的增量同步能力。

## What Changes

- 新增 SVN 连接器（`internal/datasource/connector/svn/`），实现 `datasource.Connector` 接口，通过 shell out 调用 `svn` CLI 与远程仓库交互
- 新增类型常量 `ConnectorTypeSVN = "svn"` 和 `ChannelSVN = "svn"`
- 在 `ConnectorMetadataRegistry` 注册 SVN 连接器元数据（capabilities: `incremental`, `deletion_sync`）
- 在 `initConnectorRegistry()` 注册 SVN 连接器实例
- 新增前端连接器配置表单（仓库 URL、认证、文件过滤）和资源选择器（目录树懒加载）
- Dockerfile.app 增加 `subversion` 系统依赖安装

## Capabilities

### New Capabilities

- `svn-connector`: SVN 仓库文档同步连接器，支持 svn:///http(s):// 协议、增量/全量同步、基于 revision 的精确变更检测、目录树浏览、文件类型过滤

### Modified Capabilities

（无现有 capability 的需求变更）

## Impact

**后端**
- 新增 `internal/datasource/connector/svn/` 包（~5 个文件，~600 行含测试）
- 修改 `internal/types/datasource.go` — 新增 `ConnectorTypeSVN` 常量
- 修改 `internal/types/knowledge.go` — 新增 `ChannelSVN` 常量
- 修改 `internal/datasource/connector.go` — `ConnectorMetadataRegistry` 注册 SVN 元数据
- 修改 `internal/container/container.go` — `initConnectorRegistry()` 注册 SVN 连接器

**前端**
- 新增 SVN 连接器配置组件（仓库 URL、认证、文件扩展名过滤、排除路径）
- 新增 SVN 资源选择器（目录树懒加载，复用现有资源选择器框架）

**部署**
- `docker/Dockerfile.app` 需增加 `apt-get install -y subversion`
- Helm chart 文档需注明 SVN CLI 依赖

**依赖**
- 无新 Go 依赖（不引入 Masterminds/vcs，自行封装 svn CLI wrapper）
- 系统依赖：`subversion` 包（`svn` CLI 必须安装）
- 无数据库迁移（复用现有 `data_sources` 表 schema）

**不受影响**
- `scheduler.go` — cron 调度完全复用
- `datasource_service.go` — `ProcessSync`/`ingestItem` 完全复用
- 向量引擎、文档解析 pipeline — 完全复用
