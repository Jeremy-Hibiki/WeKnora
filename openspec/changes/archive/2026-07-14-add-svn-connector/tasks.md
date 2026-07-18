## 1. 类型常量与元数据注册

- [x] 1.1 在 `internal/types/datasource.go` 新增 `ConnectorTypeSVN = "svn"` 常量
- [x] 1.2 在 `internal/types/knowledge.go` 新增 `ChannelSVN = "svn"` 常量
- [x] 1.3 在 `internal/datasource/connector.go` 的 `ConnectorMetadataRegistry` 注册 SVN 元数据（type: "svn", name: "SVN Repository", capabilities: ["incremental", "deletion_sync"], auth_type: "custom", priority: 8）

## 2. SVN CLI Wrapper (client.go)

- [x] 2.1 创建 `internal/datasource/connector/svn/` 包目录
- [x] 2.2 创建 `types.go`：定义 `Config` 结构体（`RepoURL`, `Username`, `Password`, `FileExtensions`, `ExcludePaths`, `MaxFileSize`）、`svnCursor` 结构体（`LastRevision`, `RepoUUID`）、SVN XML 响应类型（`infoXML`, `listXML`, `diffSummarizeXML`）、`parseSvnConfig()` 函数（从 `DataSourceConfig` 提取并校验配置）
- [x] 2.3 创建 `client.go`：定义 `svnCLI` 接口（`Info`/`List`/`ListRecursive`/`Cat`/`DiffSummarize` 方法）和 `cliImpl` 实现（使用 `exec.Command("svn", args...)` 无 shell 调用，统一加 `--non-interactive --trust-server-cert --username --password`）
- [x] 2.4 实现 `cliImpl.Info(ctx, url) → (*repoInfo, error)`：执行 `svn info --xml <url>`，解析返回 revision + UUID + URL
- [x] 2.5 实现 `cliImpl.List(ctx, url, path) → ([]listEntry, error)`：执行 `svn list --xml <url>/<path>`，解析返回 entries（name, kind, size）
- [x] 2.6 实现 `cliImpl.ListRecursive(ctx, url, path) → ([]listEntry, error)`：执行 `svn list -R --xml <url>/<path>`，递归返回所有文件（含相对路径和 size）
- [x] 2.7 实现 `cliImpl.Cat(ctx, url, path, revision) → ([]byte, error)`：执行 `svn cat -r <revision> <url>/<path>`，返回原始文件字节
- [x] 2.8 实现 `cliImpl.DiffSummarize(ctx, url, path, oldRev, newRev) → ([]diffEntry, error)`：执行 `svn diff --summarize -r <old>:<new> --xml <url>/<path>`，解析返回 path + 变更类型（A/M/D）

## 3. Connector 接口实现 (connector.go)

- [x] 3.1 创建 `connector.go`：定义 `Connector` 结构体 + `NewConnector()` 构造函数 + 编译时接口检查 `var _ datasource.Connector = (*Connector)(nil)`
- [x] 3.2 实现 `Type() → "svn"`
- [x] 3.3 实现 `Validate(ctx, config)`：解析配置 → 检查 `repo_url` 非空 → 校验 URL（SSRF 防护，复用 `datasource.ValidateConnectorBaseURL`）→ 调用 `svn info` 测试连通性
- [x] 3.4 实现 `ListResources(ctx, config, parentID)`：`parentID == ""` 列根目录，否则列子目录；目录设 `HasChildren: true`，文件设 `HasChildren: false`
- [x] 3.5 实现 `ResolveResourceAncestors(ctx, config, resourceIDs)`：返回各选中路径的所有祖先目录 ID（用于前端懒加载回显）
- [x] 3.6 实现 `FetchAll(ctx, config, resourceIDs)`：对每个 resourceID 调用 `ListRecursive` 发现文件 → 过滤（扩展名/排除路径/大小）→ 调用 `Cat` 取内容 → 组装 `[]FetchedItem`
- [x] 3.7 实现 `FetchIncremental(ctx, config, cursor)`：调用 `Info` 取 HEAD + UUID → UUID 变化则降级 `FetchAll` → 对每个 resourceID 调用 `DiffSummarize` → A/M 项过滤+取内容 → D 项设 `IsDeleted: true` → 组装 items + 新 cursor
- [x] 3.8 实现文件过滤辅助函数 `shouldInclude(path, size, cfg) bool`：扩展名白名单匹配 + exclude_paths glob 匹配 + max_file_size 检查

## 4. 错误处理与健壮性

- [x] 4.1 在 `FetchAll`/`FetchIncremental` 中实现 `PartialFetchError` 返回：单个文件 `svn cat` 失败时记录详情、继续处理其余文件
- [x] 4.2 实现 `depInstalled()` 检查：`Validate` 时检测 `svn` 是否在 PATH 中，不存在则返回清晰错误
- [x] 4.3 `svn` 命令超时处理：每次 `exec.Command` 设置 `context.WithTimeout`（默认 5 分钟），防止网络挂起

## 5. 容器注册

- [x] 5.1 在 `internal/container/container.go` 的 import 块添加 `svnConnector \"github.com/Tencent/WeKnora/internal/datasource/connector/svn\"`
- [x] 5.2 在 `initConnectorRegistry()` 中添加 `registry.Register(svnConnector.NewConnector())`

## 6. 单元测试

- [x] 6.1 创建 `types_test.go`：测试 `parseSvnConfig` 正常/缺失 repo_url/空配置场景
- [x] 6.2 创建 `client_test.go`：定义 `mockSVNCLI` 实现 `svnCLI` 接口，返回预设的 XML 响应；测试各命令的 XML 解析正确性
- [x] 6.3 创建 `connector_test.go`：使用 mock CLI 测试 `Validate`/`ListResources`/`FetchAll`/`FetchIncremental`（含增量检测 A/M/D、UUID 变化降级、文件过滤、PartialFetchError）
- [x] 6.4 验证：`go test -count=1 -v ./internal/datasource/connector/svn/...`

## 7. 前端

- [x] 7.1 在 `frontend/src/api/knowledge-base/` 或 datasource API 模块中确认 SVN 类型已通过后端 `ListAvailableConnectors` 自动暴露（无需前端硬编码类型列表）
- [x] 7.2 在连接器配置表单组件中增加 SVN 配置字段渲染：`repo_url`、`username`、`password`（Credentials 区）、`file_extensions`、`exclude_paths`、`max_file_size`（Settings 区）
- [x] 7.3 确认资源选择器（目录树懒加载）能正确渲染 SVN 返回的 `Resource` 列表（复用现有的层级选择器组件，`HasChildren` 控制展开箭头）
- [x] 7.4 添加 i18n 词条（zh-CN, en-US, ru-RU, ko-KR）：连接器名称、描述、配置字段标签、占位符

## 8. 部署适配

- [x] 8.1 在 `docker/Dockerfile.app` 的构建阶段和运行阶段安装 `subversion` 包（`apt-get install -y subversion`）
- [x] 8.2 在 `.env.example` 中补充 SVN 相关说明（如有环境变量需求）
- [x] 8.3 验证 Lite 模式（无 Redis）下 SVN 同步路径正常（`SyncTaskExecutor` inline goroutine）

## 9. 集成测试

- [x] 9.1 编写集成测试：使用 `svnadmin create` + `svn import` 创建本地测试仓库 + `svnserve -d` 启动服务，验证全流程（Validate → ListResources → FetchAll → FetchIncremental）
- [x] 9.2 测试增量场景：首次 sync → svn commit 新文件 → 再次 sync 验证只检测到新增
- [x] 9.3 测试删除场景：svn delete 文件 → sync 验证 `IsDeleted: true` 正确发出
- [x] 9.4 测试仓库迁移场景：更换底层仓库 UUID → sync 验证降级 FetchAll

## 10. 验收检查

- [x] 10.1 `go test -count=1 ./...` 全部通过
- [x] 10.2 `go vet ./...` 无告警
- [x] 10.3 `cd frontend && pnpm run type-check` 通过
- [x] 10.4 `openspec validate --change add-svn-connector` 通过
