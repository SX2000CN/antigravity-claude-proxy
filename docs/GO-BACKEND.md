# Antigravity Claude Proxy - Go Backend

Go 语言重写版本的 Antigravity Claude Proxy 后端，提供更高的性能和更低的内存占用。

> **📌 Beta 分支说明**
>
> 此分支的目标是将原版 Node.js 后端完全迁移到 Go + Redis，同时：
>
> - **保持前端代码不变** (`public/` 目录)
> - **保持 API 100% 兼容**，确保 Claude Code CLI 和 WebUI 正常工作
> - **维护与上游相同的模块结构**，方便后期同步原版更新

## 特性

- 🚀 **高性能**: 基于 Gin 框架，支持高并发
- 💾 **Redis 支持**: 使用 Redis 进行数据持久化和缓存
- 🔄 **完全兼容**: 与 Node.js 版本 API 完全兼容
- 📊 **多账户负载均衡**: 支持 Sticky、Round-Robin、Hybrid 三种策略
- 🌊 **流式响应**: 完整的 SSE 流式响应支持
- 🔐 **OAuth 认证**: 支持 Google OAuth PKCE 流程

## 目录结构

```
go-backend/
├── cmd/
│   ├── server/          # 主服务器入口
│   ├── accounts/        # 账户管理 CLI
│   └── migrate/         # 数据迁移工具
│
├── internal/
│   ├── account/         # 账户管理
│   │   ├── manager.go   # 账户管理器
│   │   ├── credentials.go
│   │   └── strategies/  # 选择策略
│   │       ├── sticky.go
│   │       ├── round_robin.go
│   │       ├── hybrid.go
│   │       └── trackers/
│   │
│   ├── auth/            # 认证模块
│   │   ├── oauth.go     # OAuth PKCE
│   │   ├── database.go  # SQLite 读取
│   │   └── token_extractor.go
│   │
│   ├── cloudcode/       # Cloud Code API 客户端
│   │   ├── client.go
│   │   ├── message_handler.go
│   │   ├── streaming_handler.go
│   │   ├── sse_parser.go
│   │   └── model_api.go
│   │
│   ├── config/          # 配置管理
│   │   ├── config.go
│   │   └── constants.go
│   │
│   ├── format/          # 格式转换
│   │   ├── request_converter.go
│   │   ├── response_converter.go
│   │   └── signature_cache.go
│   │
│   ├── server/          # HTTP 服务器
│   │   ├── server.go
│   │   ├── middleware.go
│   │   ├── handlers/
│   │   └── sse/
│   │
│   ├── webui/           # WebUI 后端
│   │   ├── router.go
│   │   ├── auth.go
│   │   └── handlers/
│   │
│   ├── modules/         # 功能模块
│   │   └── usage_stats.go
│   │
│   └── utils/           # 工具函数
│       ├── helpers.go
│       └── logger.go
│
└── pkg/
    ├── anthropic/       # Anthropic API 类型
    │   └── types.go
    └── redis/           # Redis 客户端
        ├── client.go
        ├── accounts.go
        └── signatures.go
```

## 依赖

- Go 1.21+
- Redis 6.0+ (可选，用于持久化)
- Gin Web Framework
- go-redis/redis

## 快速开始

### 方式 A: Docker Hub 部署 (推荐)

最简单的部署方式，使用预构建的 Docker 镜像：

```bash
cd go-backend

# 启动服务 (包含 Redis 和 Proxy)
docker-compose up -d

# 查看日志
docker-compose logs -f proxy

# 停止服务
docker-compose down
```

服务启动后访问 http://localhost:8080 即可使用 WebUI。

**Docker Hub 镜像**: [`sx2000/antigravity-proxy-go:latest`](https://hub.docker.com/r/sx2000/antigravity-proxy-go)

### 方式 B: 从源码编译

如果需要自定义修改，可以从源码编译：

```bash
cd go-backend

# 编译 (Linux/macOS)
go build -ldflags="-s -w" -o build/antigravity-proxy ./cmd/server

# 编译 (Windows)
go build -ldflags="-s -w" -o build/antigravity-proxy.exe ./cmd/server

# 交叉编译 Linux (在 Windows 上)
$env:GOOS="linux"; $env:GOARCH="amd64"; go build -ldflags="-s -w" -o build/antigravity-proxy-linux ./cmd/server
```

运行：

```bash
# 从项目根目录运行（自动检测 public 目录）
cd antigravity-claude-proxy
./go-backend/build/antigravity-proxy

# 带参数运行
./go-backend/build/antigravity-proxy --dev-mode --fallback --strategy=hybrid
```

### 3. 命令行参数

| 参数 | 说明 | 默认值 |
|------|------|--------|
| `--dev-mode` | 启用开发者模式（详细日志） | false |
| `--debug` | 同 --dev-mode（兼容旧版） | false |
| `--fallback` | 启用模型回退 | false |
| `--strategy` | 账户选择策略 | hybrid |
| `--port` | 服务器端口 | 8080 |
| `--host` | 绑定地址 | 0.0.0.0 |

### 4. 环境变量

| 变量 | 说明 |
|------|------|
| `PORT` | 服务器端口 |
| `HOST` | 绑定地址 |
| `DEBUG` / `DEV_MODE` | 开发者模式 |
| `FALLBACK` | 模型回退 |
| `REDIS_ADDR` | Redis 地址 |
| `REDIS_PASSWORD` | Redis 密码 |
| `API_KEY` | API 访问密钥 |
| `WEBUI_PASSWORD` | WebUI 密码 |

## 配置文件

配置文件位置: `~/.config/antigravity-proxy/config.json`

```json
{
  "apiKey": "your-api-key",
  "webuiPassword": "your-password",
  "devMode": false,
  "maxRetries": 5,
  "maxAccounts": 100,
  "accountSelection": {
    "strategy": "hybrid"
  },
  "redisAddr": "localhost:6379",
  "port": 8080,
  "host": "0.0.0.0"
}
```

## API 端点

### 核心 API

| 端点 | 方法 | 说明 |
|------|------|------|
| `/v1/messages` | POST | Anthropic Messages API |
| `/v1/models` | GET | 列出可用模型 |
| `/health` | GET | 健康检查 |
| `/account-limits` | GET | 账户配额状态 |

### WebUI API

| 端点 | 方法 | 说明 |
|------|------|------|
| `/api/accounts` | GET | 账户列表 |
| `/api/accounts/:email` | DELETE | 删除账户 |
| `/api/accounts/:email/refresh` | POST | 刷新账户 |
| `/api/config` | GET/POST | 配置管理 |
| `/api/logs/stream` | GET | 日志 SSE 流 |

## 与 Node.js 版本的区别

1. **无需 Node.js**: 独立二进制，无运行时依赖
2. **更低内存**: 典型运行内存 ~50MB vs Node.js ~200MB
3. **Redis 支持**: 原生 Redis 集成用于持久化
4. **静态编译**: 单文件部署，无需安装依赖

## 开发

```bash
# 运行测试
go test ./...

# 格式化代码
go fmt ./...

# 检查代码
go vet ./...
```

## License

MIT

---

## 迁移指南：从 Node.js 版本切换到 Go 版本

### 最终项目结构

当 Go 后端测试稳定后，项目将精简为以下结构：

```text
antigravity-claude-proxy/
├── go-backend/              # Go 后端（保留）
│   ├── cmd/
│   ├── internal/
│   ├── pkg/
│   ├── deploy/
│   ├── Dockerfile
│   ├── docker-compose.yml
│   ├── Makefile
│   ├── README.md
│   └── DEPLOYMENT.md
│
├── public/                  # 前端静态文件（保留，两个版本共用）
│   ├── index.html
│   ├── css/
│   ├── js/
│   └── views/
│
├── docs/                    # 文档（保留）
├── images/                  # 图片资源（保留）
├── LICENSE                  # 许可证（保留）
├── README.md                # 主 README（更新）
└── .gitignore               # Git 配置（更新）
```

### 需要删除的 Node.js 相关文件

以下文件/目录在 Go 版本稳定后可以删除：

#### 源代码目录

```bash
# Node.js 后端代码
rm -rf src/

# Node.js 测试
rm -rf tests/

# CLI 入口
rm -rf bin/
```

#### 包管理和工具配置

```bash
# Node.js 包管理
rm -f package.json
rm -f package-lock.json
rm -rf node_modules/

# Node.js 工具配置
rm -f postcss.config.js
rm -f tailwind.config.js
rm -f .npmignore

# 示例配置（Go 版本有自己的配置格式）
rm -f config.example.json
```

#### 项目配置

```bash
# Claude Code 配置（可选保留，但需要更新）
rm -rf .claude/

# Node.js 版本的 CLAUDE.md（需要为 Go 版本重写）
rm -f CLAUDE.md
```

### 一键清理脚本

在项目根目录创建并运行（请先备份！）：

**Linux/macOS:**

```bash
#!/bin/bash
# cleanup-nodejs.sh - 删除 Node.js 相关文件

echo "⚠️  警告: 此操作将删除所有 Node.js 相关文件！"
read -p "确认继续? (y/N) " confirm
if [[ "$confirm" != "y" && "$confirm" != "Y" ]]; then
    echo "已取消"
    exit 0
fi

# 源代码
rm -rf src/
rm -rf tests/
rm -rf bin/

# 包管理
rm -f package.json
rm -f package-lock.json
rm -rf node_modules/

# 工具配置
rm -f postcss.config.js
rm -f tailwind.config.js
rm -f .npmignore
rm -f config.example.json

echo "✅ Node.js 文件已删除"
echo "📌 请手动更新 README.md 和 .gitignore"
```

**Windows (PowerShell):**

```powershell
# cleanup-nodejs.ps1 - 删除 Node.js 相关文件

Write-Host "⚠️  警告: 此操作将删除所有 Node.js 相关文件！" -ForegroundColor Yellow
$confirm = Read-Host "确认继续? (y/N)"
if ($confirm -ne "y" -and $confirm -ne "Y") {
    Write-Host "已取消"
    exit
}

# 源代码
Remove-Item -Recurse -Force -ErrorAction SilentlyContinue src/
Remove-Item -Recurse -Force -ErrorAction SilentlyContinue tests/
Remove-Item -Recurse -Force -ErrorAction SilentlyContinue bin/

# 包管理
Remove-Item -Force -ErrorAction SilentlyContinue package.json
Remove-Item -Force -ErrorAction SilentlyContinue package-lock.json
Remove-Item -Recurse -Force -ErrorAction SilentlyContinue node_modules/

# 工具配置
Remove-Item -Force -ErrorAction SilentlyContinue postcss.config.js
Remove-Item -Force -ErrorAction SilentlyContinue tailwind.config.js
Remove-Item -Force -ErrorAction SilentlyContinue .npmignore
Remove-Item -Force -ErrorAction SilentlyContinue config.example.json

Write-Host "✅ Node.js 文件已删除" -ForegroundColor Green
Write-Host "📌 请手动更新 README.md 和 .gitignore" -ForegroundColor Cyan
```

### 迁移后的 .gitignore 更新

删除 Node.js 相关条目，保留以下内容：

```gitignore
# Go
go-backend/build/
*.exe

# 配置文件（包含敏感信息）
config.json
accounts.json

# IDE
.idea/
.vscode/
*.swp

# OS
.DS_Store
Thumbs.db

# 日志
*.log
```

### 迁移后的 README.md 更新

主 README.md 应该更新为直接指向 Go 版本，删除所有 Node.js 相关的安装和使用说明。可以参考本文档的内容进行更新。
