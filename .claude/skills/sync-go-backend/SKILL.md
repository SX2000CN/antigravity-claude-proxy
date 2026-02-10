---
description: 同步上游更新到 beta 分支（前端自动合并，后端手动同步到 Go）
disable-model-invocation: true
argument-hint: "[模块名，如 cloudcode/format/webui，留空则全量同步]"
---

## 上下文（自动注入）

当前分支: !`git branch --show-current`
与 main 的后端差异: !`git diff main...HEAD --name-only -- src/ 2>/dev/null || echo "无法获取差异，请先 git fetch"`

## 任务目标

同步上游 main 分支的更新到 beta 分支：

- **前端** (`public/`)：Git 自动合并，仅需处理冲突和 CSS 重编译
- **后端** (`src/`)：手动同步到 `go-backend/internal/`，逐模块对比转换

如果指定了模块名 `$ARGUMENTS`，则仅同步该模块。否则全量检测。

## 工作流程

Track these steps as TODOs and complete them one by one.

### 1. 合并 main 到 beta

```bash
git checkout beta
git merge main
```

如果有冲突，优先处理：
- `public/js/translations/*.js` — 翻译文件容易出现引号转义冲突
- `package.json` / `package-lock.json` — 接受上游版本

### 2. 检测后端变更

```bash
# 查看本次合并引入的 src/ 变更
git diff HEAD~1...HEAD --name-only -- src/
```

如果无输出，后端无需同步，跳到第 5 步检查前端。

### 3. 逐文件对比同步

对每个变更的 Node.js 文件：

```bash
# 读取上游 Node.js 源码
git show main:src/<path>

# 读取对应的 Go 文件
cat go-backend/internal/<mapped-path>
```

对比时重点关注：
- 常量值和枚举（数值必须完全一致）
- 验证规则的范围边界（min/max）
- API 路由和响应结构
- 错误消息文本

### 4. 验证后端

```bash
cd go-backend && go build ./... && go vet ./...
```

### 5. 验证前端

如果 `public/css/src/input.css` 有变更：

```bash
npm run build:css
```

如果 `public/js/translations/*.js` 有变更，检查语法是否正确（注意引号转义）。

## 目录映射表

| Node.js | Go | 说明 |
|---------|-----|------|
| `src/index.js` | `cmd/server/main.go` | 入口点 |
| `src/server.js` | `internal/server/server.go` | HTTP 服务器 |
| `src/constants.js` | `internal/config/constants.go` | 常量定义 |
| `src/config.js` | `internal/config/config.go` | 运行时配置 |
| `src/errors.js` | `internal/config/errors.go` | 错误类型 |
| `src/fallback-config.js` | `internal/config/fallback.go` | 模型降级 |
| `src/cloudcode/*.js` | `internal/cloudcode/*.go` | Cloud Code 客户端 |
| `src/account-manager/*.js` | `internal/account/*.go` | 账号管理 |
| `src/account-manager/strategies/*.js` | `internal/account/strategies/*.go` | 选择策略 |
| `src/webui/index.js` | `internal/webui/*.go` | WebUI API |
| `src/auth/*.js` | `internal/auth/*.go` | 认证 |
| `src/format/*.js` | `internal/format/*.go` | 格式转换 |
| `src/utils/*.js` | `internal/utils/*.go` | 工具函数 |
| `src/modules/*.js` | `internal/modules/*.go` | 功能模块 |
| N/A | `pkg/anthropic/` | Anthropic 类型定义（Go 独有） |
| N/A | `pkg/redis/` | Redis 客户端（Go 独有，替代 SQLite） |

## 已知的坑

- **翻译文件引号**：`zh.js` 等文件中 `"xxx"` 嵌套在双引号字符串内会导致语法错误，用 `「」` 替代
- **Go 包名差异**：Node.js 的 `account-manager` 在 Go 中是 `account`（无连字符）
- **Client Metadata 枚举**：必须用数值（0/1/2/...），不能用字符串
- **CSS 编译**：前端 CSS 变更后必须 `npm run build:css`，否则样式不生效

## 注意事项

- 不要修改 main 分支（上游镜像）
- 前端 `public/` 自动合并，仅处理冲突和 CSS 重编译
- 后端 `src/` 需要手动转换到 Go
- 同步后运行 `go build` 和 `go vet` 验证
- 提交时用 `git add -f` 添加 `.claude/` 下的文件（被 .gitignore 忽略）
