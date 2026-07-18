## Context

WeKnora 已有一套成熟的文档同步连接器框架（`internal/datasource/connector/`），支持 Feishu、Notion、Yuque、RSS 四种外部数据源。框架核心包括：

- `Connector` 接口（6 个方法：`Type`/`Validate`/`ListResources`/`ResolveResourceAncestors`/`FetchAll`/`FetchIncremental`）
- `Scheduler`（基于 robfig/cron 的定时调度，双层去重）
- `DataSourceService.ProcessSync`（asynq 任务处理，调用连接器 → `ingestItem` → 知识库写入）
- 游标机制（`SyncCursor`，各连接器自定义 `ConnectorCursor`）

现有连接器使用 **编辑时间对比** 做增量检测（如 Yuque 的 `map[bookID]map[docID]timestamp`）。SVN 作为版本控制系统，有原生的 revision 号和 `svn diff --summarize` 命令，可以提供更精确、更简洁的增量能力。

## Goals / Non-Goals

**Goals:**
- 实现 SVN 连接器，完整支持 `Connector` 接口的 6 个方法
- 支持 `svn://`、`http(s)://`、`svn+ssh://` 协议（通过 svn CLI 统一处理）
- 基于 revision 号的精确增量同步，无需本地 checkout
- 支持文件扩展名白名单、路径排除、文件大小限制
- 复用现有的调度器、ProcessSync、ingestItem pipeline，零框架改动
- 支持用户名/密码认证

**Non-Goals:**
- 不支持本地 `svn checkout` 模式（远程操作 only，避免磁盘和带宽浪费）
- 不引入 Masterminds/vcs 等第三方库（见决策 D1）
- 不在本期修改服务层的删除逻辑（`ingestItem` 不执行真删除 — 保持与现有连接器一致，作为后续独立变更）
- 不实现 SVN webhook 推送（SVN 无原生 webhook 支持）
- 不支持 SSL 客户端证书认证（Phase 2 可扩展）

## Decisions

### D1: svn CLI wrapper，不引入 Masterminds/vcs

**决策：** 自行封装 `svn` CLI wrapper（~150 行），不引入 `github.com/Masterminds/vcs`。

**理由：**

| 我们需要的 svn 命令 | Masterminds/vcs | 我们自写 |
|---|---|---|
| `svn info <url>` → UUID + revision | `Ping()` 只返回 bool | 解析 XML 提取 UUID + revision |
| `svn list <url>/<path>` | **无** | `ListResources` 核心 |
| `svn list -R <url>` | **无** | `FetchAll` 文件发现 |
| `svn cat -r HEAD <url>/<file>` | **无** | 取文件内容 |
| `svn diff --summarize -r OLD:HEAD` | **无** | **增量同步核心** |

5 个核心命令缺 4 个。Masterminds/vcs 是**本地工作副本模型**（先 checkout 到本地目录，再 `RunFromDir` 操作），与我们的**远程无检出模型**冲突。引入后仍需自写 90% 的 svn 调用逻辑。此外该库最后发版为 2022 年 v1.13.3，不活跃。

**替代方案否决：**
- CGo 绑定 libsvn — 编译复杂，CGO_ENABLED=1 已是生产要求，再绑 libsvn 进一步增加交叉编译难度
- 纯 Go WebDAV 客户端 — 仅支持 http(s):// 协议，且无 `diff --summarize` 等高级操作
- `golang.org/x/tools/go/vcs` — 仅做 VCS 类型检测，不含任何操作

### D2: 游标设计 — revision 号 + repo UUID

**决策：** 游标只存储一个 revision 整数和 repo UUID。

```go
type svnCursor struct {
    LastSyncTime time.Time `json:"last_sync_time"`
    LastRevision int64     `json:"last_revision"`
    RepoUUID     string    `json:"repo_uuid"`
}
```

**理由：** 对比现有连接器的游标复杂度：

| 连接器 | 游标结构 | 淘汰变更检测方式 |
|---|---|---|
| Yuque | `map[bookID]map[docID]string` | 逐项对比 contentUpdatedAt |
| Feishu | `map[spaceID]map[nodeToken]string` | 逐项对比 ObjEditTime |
| RSS | `map[feedURL]map[itemID]string` | 双层指纹 |
| **SVN** | `int64` revision | `svn diff --summarize -r OLD:HEAD` |

`svn diff --summarize` 直接返回精确的 A/M/D 变更列表，无需逐项对比。`RepoUUID` 用于检测仓库迁移（URL 不变但底层换了仓库）——UUID 变化时自动降级为 FetchAll。

### D3: 增量同步流程

```
FetchIncremental:
  1. svn info <url> → current HEAD revision + repo UUID
  2. if cursor.RepoUUID != current UUID → 降级 FetchAll（仓库已迁移）
  3. for each selected_path:
       svn diff --summarize -r cursor.LastRevision:HEAD <url>/<path> --xml
       → 解析 A/M/D 路径列表
  4. for A, M items:
       if matches(file_extensions) && !matches(exclude_paths) && size <= max:
         svn cat -r HEAD <url>/<path> → raw bytes
         emit FetchedItem{Content: bytes, ExternalID: filepath}
  5. for D items:
       emit FetchedItem{ExternalID: filepath, IsDeleted: true}
  6. return items + cursor{LastRevision: HEAD, RepoUUID: UUID}
```

### D4: ListResources 懒加载模式

**决策：** `parentID == ""` 返回仓库根目录列表；`parentID == "/docs"` 返回该子目录列表。每个资源项设 `HasChildren` 标志（目录 = true，文件 = false），供前端展开。

**理由：** SVN 仓库可能很大，一次性 `svn list -R` 可能返回上万条目。懒加载逐层浏览（类似 Feishu wiki space 模式），前端体验好，网络开销低。

### D5: 凭证传递 — CLI 参数方式

**决策：** 通过 `--username`/`--password` CLI 参数传递凭证，配合 `--non-interactive` 和 `--trust-server-cert`。

**理由：**
- SVN CLI 不支持 stdin 传密码
- `--config-dir` 临时目录方案更安全（不暴露在进程列表）但实现复杂
- 本系统是服务端进程，运行在受控环境，`ps` 可见性风险可控
- 所有 svn 调用使用 `exec.Command("svn", args...)`，参数作为独立字符串传递，无 shell 拼接，杜绝命令注入

### D6: ExternalID = 仓库内文件路径

**决策：** `FetchedItem.ExternalID` 使用仓库内文件路径（如 `/docs/architecture/overview.md`）。

**理由：**
- 路径在仓库内唯一，天然适合作为 dedup key
- `ingestItem` 通过 `metadata.external_id` 匹配已有 Knowledge 做更新
- SVN 路径是稳定的（不像 Git 的 hash 会变），跨 revision 持续有效

## Risks / Trade-offs

| 风险 | 缓解 |
|---|---|
| `svn` CLI 未安装在部署环境 | Dockerfile.app 安装 `subversion`；`Validate` 时检测 `svn` 是否存在，给出清晰错误 |
| `--password` 在进程列表可见 | 文档说明；后续可切换到 `--config-dir` 临时方案 |
| 大文件 `svn cat` 消耗内存 | `max_file_size` 配置项（默认 50MB），`svn list --xml` 预检文件大小 |
| 超大仓库增量 diff 耗时 | asynq 任务 Timeout 已设为 2h；`svn diff --summarize` 本身很高效（只比对 revision 元数据） |
| SSRF（恶意仓库 URL 指向内网） | `Validate` 阶段复用 `datasource.ValidateConnectorBaseURL` 校验 URL |
| 删除同步只计数不执行 | 本期保持与现有连接器一致；SVN 检测可靠性高，后续可单独提案在服务层开启真删除 |
| SVN 服务器不可达导致同步失败 | `ProcessSync` 已有错误处理：cursor 仍持久化（即使 fetch 失败），下次恢复不需全量重同步 |

## Open Questions

1. **文件编码检测**：SVN 文件可能是 GBK/GB2312 编码（中国常见）。是否需要自动检测并转为 UTF-8？Phase 1 暂不处理，交给 docreader pipeline（已有编码检测能力），Phase 2 评估。
2. **SVN 外部引用（svn:externals）**：外部引用的仓库是否跟随同步？Phase 1 忽略 externals（`--ignore-externals` 标志），Phase 2 评估是否需要。
