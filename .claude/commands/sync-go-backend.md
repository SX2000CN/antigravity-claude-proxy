---
name: Sync Upstream Changes
description: 同步上游更新到 beta 分支（前端自动合并，后端手动同步到 Go）
category: Development
tags: [sync, go, backend, upstream]
---

## 任务目标

同步上游 main 分支的更新到 beta 分支：

- **前端**：`public/` 目录通过 Git 自动合并
- **后端**：`src/` 目录需要手动同步到 `go-backend/internal/`

## 工作流程

Track these steps as TODOs and complete them one by one.

### 1. 合并 main 到 beta

```bash
git checkout beta
git merge main
```

前端变更会自动合并。

### 2. 检测后端变更

```bash
git diff HEAD~1...HEAD --name-only -- src/
```

如果有输出，说明 `src/` 目录有变更，需要同步到 Go。

### 3. 读取 Node.js 源代码

```bash
git show main:src/path/to/file.js
```

### 4. 对比并同步到 Go

对比 Node.js 实现与 Go 实现的差异：

- API 端点：路由、请求/响应格式
- 业务逻辑：算法、验证规则、边界条件
- 常量值：枚举值、配置范围、默认值
- 错误处理：错误类型、错误消息

### 5. 验证

```bash
cd go-backend && go build ./... && go vet ./...
```

## 目录映射表

| Node.js | Go | 说明 |
|---------|-----|------|
| `src/constants.js` | `internal/config/constants.go` | 常量 |
| `src/config.js` | `internal/config/config.go` | 配置 |
| `src/cloudcode/*.js` | `internal/cloudcode/*.go` | API 客户端 |
| `src/account-manager/*.js` | `internal/accountmanager/*.go` | 账号管理 |
| `src/webui/index.js` | `internal/webui/*.go` | WebUI API |
| `src/auth/*.js` | `internal/auth/*.go` | 认证 |
| `src/format/*.js` | `internal/format/*.go` | 格式转换 |
| `src/utils/*.js` | `internal/utils/*.go` | 工具函数 |

## 常见同步点

1. **Client Metadata** (`constants.go`) - ideType, platform, pluginType 枚举
2. **验证规则** (`handlers/*.go`) - 数值范围、字符串长度
3. **API 响应格式** - 状态字段、错误消息
4. **默认配置值** - DEFAULT_* 常量

## 注意事项

- 不要修改 main 分支（上游镜像）
- 前端 `public/` 自动合并，无需手动操作
- 后端 `src/` 需要手动转换到 Go
- 同步后运行 `go build` 和 `go vet` 验证
