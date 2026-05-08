# xcodex

Codex CLI 数据管理工具。从会话压缩到全文搜索，一站式管理你的 Codex 数据。

## 安装

### Homebrew

```bash
brew tap liangzhiyu/xcodex
brew install xcodex
```

### Go 安装

```bash
go install github.com/liangzhiyu/xcodex/cmd/xcodex@latest
```

### 本地构建

```bash
go build -o xcodex ./cmd/xcodex/
```

## 命令

### `xcodex compress` — 会话压缩

将 Codex 会话压缩为语义剪枝后的 Markdown，用于跨工具上下文转发（如 Codex → Claude Code）。

```bash
xcodex compress                  # 压缩最新会话，64k token 预算
xcodex compress --tokens 32k     # 指定 token 预算（8k/16k/32k/64k）
xcodex compress 3                # 压缩 list 输出中的第 3 条会话
xcodex compress abc123           # 按 session id 压缩指定会话
xcodex compress --cwd /path 3    # 在指定项目窗口里按序号选择
xcodex compress --file PATH      # 指定会话文件
xcodex compress --copy           # 复制到剪贴板
```

压缩输出包含：任务描述、当前状态、待办、文件变更、关键决策、已完成工作、按优先级排列的对话历史。Token 预算不足时自动压缩低优先级轮次为单行摘要。

### `xcodex list` — 列出会话

```bash
xcodex list                      # 最近 15 个会话
xcodex list --limit 30           # 显示更多
xcodex list --cwd /path/to/proj  # 按项目过滤
```

显示会话标题、时间、项目路径和 token 用量。

### `xcodex search` — 全文搜索

```bash
xcodex search "bind-repo"        # 搜索所有会话
xcodex search "deploy" --cwd /path  # 限定项目
xcodex search "egumo" --after 7d    # 最近 7 天
```

搜索用户消息、助手回复和工具调用参数，输出匹配摘要。

### `xcodex stats` — 使用统计

```bash
xcodex stats                     # 总览
xcodex stats --by project        # 按项目分组
xcodex stats --by model          # 按模型分组
xcodex stats --by day            # 按天趋势
xcodex stats --since 30d         # 最近 30 天
```

### `xcodex diff` — 文件变更摘要

```bash
xcodex diff                      # 最新会话的文件变更
xcodex diff --file PATH          # 指定会话
xcodex diff --verbose            # 含变更行数概要
```

从 JSONL 中提取 apply_patch、write_file、exec_command 等工具调用，汇总文件操作（新建/修改）。

### `xcodex clean` — 清理旧数据

```bash
xcodex clean --dry-run           # 预览将清理的文件
xcodex clean --older-than 30d    # 清理 30 天前的数据
xcodex clean --older-than 7d --archived-only  # 仅归档文件
```

删除过期 JSONL 文件并清理 SQLite 中对应的 thread 记录。

## 数据源

xcodex 读取 Codex CLI 本地数据：

- **SQLite**: `~/.codex/state_5.sqlite`（会话索引、token 用量、项目路径）
- **JSONL**: `~/.codex/sessions/` 和 `~/.codex/archived_sessions/`（会话详情）

SQLite 查询带 5 秒超时，避免与 Codex Desktop 冲突。查询失败时自动回退到文件系统扫描。

## 项目结构

```
cmd/xcodex/main.go           # CLI 入口，子命令路由
internal/codex/
├── types.go                  # 数据类型
├── parser.go                 # JSONL 解析
├── db.go                     # SQLite 查询
└── commands.go               # compress/search/stats/diff/clean 逻辑
```

## License

MIT

## 发布流程

发布新版本时执行：

```bash
git tag v0.1.1
git push origin v0.1.1
```

推送 `v*` tag 后，GitHub Actions 会自动：

- 运行 `go test ./...`
- 构建 `./cmd/xcodex`
- 创建或更新对应 GitHub Release
- 计算源码 tarball 的 `sha256`
- 更新 `liangzhiyu/homebrew-xcodex` 中的 `Formula/xcodex.rb`
