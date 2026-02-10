# Antigravity Claude Proxy

[![npm version](https://img.shields.io/npm/v/antigravity-claude-proxy.svg)](https://www.npmjs.com/package/antigravity-claude-proxy)
[![npm downloads](https://img.shields.io/npm/dm/antigravity-claude-proxy.svg)](https://www.npmjs.com/package/antigravity-claude-proxy)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)

> **🚀 Beta 分支说明**
>
> 此分支包含 **Go 语言重写的高性能后端**，使用 Redis 替代 JSON 文件存储。
>
> - 📦 **Go 后端**: 单二进制部署，更低内存占用 (~50MB vs ~200MB)
> - 🗄️ **Redis 存储**: 可选的数据持久化，支持分布式部署
> - 🔄 **完全兼容**: API 与原版 100% 兼容，前端无需修改
> - 📖 **文档**: 详见 [部署指南](docs/DEPLOYMENT.md) 和 [Go 后端说明](docs/GO-BACKEND.md)
>
> ```bash
> # 快速启动 Go 版本
> make build && ./build/antigravity-proxy
> ```

一个代理服务器，暴露 **Anthropic 兼容的 API**，由 **Antigravity 的 Cloud Code** 驱动，让你可以通过 **Claude Code CLI** 和 **OpenClaw / ClawdBot** 使用 Claude 和 Gemini 模型。

![Antigravity Claude Proxy Banner](images/banner.png)

<details>
<summary><strong>⚠️ 服务条款警告 — 安装前请阅读</strong></summary>

> [!CAUTION]
> 使用此代理可能违反 Google 的服务条款。少数用户反馈其 Google 账户被**封禁**或**影子封禁**（在未明确通知的情况下限制访问）。
>
> **高风险场景：**
> - 🚨 **新注册的 Google 账户** 被封禁的概率非常高
> - 🚨 **新账户订阅 Pro/Ultra** 很容易被标记并封禁
>
> **使用此代理即表示你了解：**
> - 这是一个非官方工具，未得到 Google 的认可
> - 你的账户可能被暂停或永久封禁
> - 你自行承担使用此代理的所有风险
>
> **建议：** 使用一个已有的 Google 账户，且不依赖该账户进行关键服务。避免专门为此代理创建新账户。

</details>

---

## 工作原理

```
┌──────────────────┐     ┌─────────────────────┐     ┌────────────────────────────┐
│   Claude Code    │────▶│  此代理服务器         │────▶│  Antigravity Cloud Code    │
│   (Anthropic     │     │  (Anthropic → Google│     │  (daily-cloudcode-pa.      │
│    API 格式)     │     │   Generative AI)    │     │   sandbox.googleapis.com)  │
└──────────────────┘     └─────────────────────┘     └────────────────────────────┘
```

1. 接收 **Anthropic Messages API 格式** 的请求
2. 使用已添加的 Google 账户的 OAuth 令牌（或 Antigravity 的本地数据库）
3. 转换为 **Google Generative AI 格式**，包装为 Cloud Code 请求
4. 发送到 Antigravity 的 Cloud Code API
5. 将响应转换回 **Anthropic 格式**，完整支持 thinking/streaming

## 前置要求

- **Go** 1.24 或更高版本（用于编译后端）
- **Node.js** 18 或更高版本（仅用于前端 CSS 构建）
- **Redis** 7 或更高版本（数据持久化）
- **Antigravity** 已安装（单账户模式）或 Google 账户（多账户模式）

---

## 安装

### 方式 1: Docker（推荐）

```bash
# 克隆仓库
git clone https://github.com/SX2000CN/antigravity-claude-proxy.git -b beta
cd antigravity-claude-proxy

# 使用 Docker Compose 启动
docker-compose up -d
```

### 方式 2: 源码编译

```bash
git clone https://github.com/SX2000CN/antigravity-claude-proxy.git -b beta
cd antigravity-claude-proxy

# 编译 Go 后端
make build

# 构建前端 CSS
npm install && npm run build:css

# 启动服务
./build/antigravity-proxy
```

---

## 快速开始

### 1. 启动代理服务器

```bash
# 如果使用 Docker
docker-compose up -d

# 如果源码编译
make build && ./build/antigravity-proxy
```

服务器默认运行在 `http://localhost:8080`。

### 2. 关联账户

选择以下方式之一来授权代理：

#### **方式 A: Web 控制台（推荐）**

1. 代理运行后，在浏览器中打开 `http://localhost:8080`。
2. 导航到 **Accounts** 选项卡，点击 **Add Account**。
3. 在弹出窗口中完成 Google OAuth 授权。

> **无头/远程服务器**: 如果在没有浏览器的服务器上运行，WebUI 支持「手动授权」模式。点击「Add Account」后，可以复制 OAuth URL，在本地机器上完成授权，然后粘贴授权码。

#### **方式 B: 命令行（桌面或无头环境）**

如果你偏好终端或在远程服务器上：

```bash
# 桌面（打开浏览器）
go run cmd/accounts/main.go add

# 无头环境（Docker/SSH）
go run cmd/accounts/main.go add --no-browser
```

#### **方式 C: 自动检测（Antigravity 用户）**

如果你已安装 **Antigravity** 应用并已登录，代理会自动检测你的本地会话，无需额外设置。

自定义端口：

```bash
PORT=3001 ./build/antigravity-proxy
```

### 3. 验证是否正常工作

```bash
# 健康检查
curl http://localhost:8080/health

# 检查账户状态和配额
curl "http://localhost:8080/account-limits?format=table"
```

---

## 配合 Claude Code CLI 使用

### 配置 Claude Code

你可以通过以下两种方式配置：

#### **通过 Web 控制台（推荐）**

1. 在浏览器中打开 `http://localhost:8080`。
2. 进入 **Settings** → **Claude CLI**。
3. 使用 **Connection Mode** 开关切换：
   - **Proxy Mode**: 使用本地代理服务器（Antigravity Cloud Code）。在此配置模型、Base URL 和预设。
   - **Paid Mode**: 直接使用官方 Anthropic Credits（需要你自己的订阅）。此模式会隐藏代理设置以避免误配置。
4. 点击 **Apply to Claude CLI** 保存更改。

> [!TIP] **配置优先级**: 系统环境变量（在 shell profile 如 `.zshrc` 中设置）优先于 `settings.json` 文件。如果你使用 Web 控制台管理设置，请确保没有在终端中手动导出冲突的变量。

#### **手动配置**

创建或编辑 Claude Code 的设置文件：

**macOS:** `~/.claude/settings.json`
**Linux:** `~/.claude/settings.json`
**Windows:** `%USERPROFILE%\.claude\settings.json`

添加以下配置：

```json
{
  "env": {
    "ANTHROPIC_AUTH_TOKEN": "test",
    "ANTHROPIC_BASE_URL": "http://localhost:8080",
    "ANTHROPIC_MODEL": "claude-opus-4-5-thinking",
    "ANTHROPIC_DEFAULT_OPUS_MODEL": "claude-opus-4-5-thinking",
    "ANTHROPIC_DEFAULT_SONNET_MODEL": "claude-sonnet-4-5-thinking",
    "ANTHROPIC_DEFAULT_HAIKU_MODEL": "claude-sonnet-4-5",
    "CLAUDE_CODE_SUBAGENT_MODEL": "claude-sonnet-4-5-thinking",
    "ENABLE_EXPERIMENTAL_MCP_CLI": "true"
  }
}
```

或者使用 Gemini 模型：

```json
{
  "env": {
    "ANTHROPIC_AUTH_TOKEN": "test",
    "ANTHROPIC_BASE_URL": "http://localhost:8080",
    "ANTHROPIC_MODEL": "gemini-3-pro-high[1m]",
    "ANTHROPIC_DEFAULT_OPUS_MODEL": "gemini-3-pro-high[1m]",
    "ANTHROPIC_DEFAULT_SONNET_MODEL": "gemini-3-flash[1m]",
    "ANTHROPIC_DEFAULT_HAIKU_MODEL": "gemini-3-flash[1m]",
    "CLAUDE_CODE_SUBAGENT_MODEL": "gemini-3-flash[1m]",
    "ENABLE_EXPERIMENTAL_MCP_CLI": "true"
  }
}
```

### 加载环境变量

将代理设置添加到你的 shell 配置文件：

**macOS / Linux:**

```bash
echo 'export ANTHROPIC_BASE_URL="http://localhost:8080"' >> ~/.zshrc
echo 'export ANTHROPIC_AUTH_TOKEN="test"' >> ~/.zshrc
source ~/.zshrc
```

> Bash 用户请将 `~/.zshrc` 替换为 `~/.bashrc`

**Windows (PowerShell):**

```powershell
Add-Content $PROFILE "`n`$env:ANTHROPIC_BASE_URL = 'http://localhost:8080'"
Add-Content $PROFILE "`$env:ANTHROPIC_AUTH_TOKEN = 'test'"
. $PROFILE
```

**Windows (命令提示符):**

```cmd
setx ANTHROPIC_BASE_URL "http://localhost:8080"
setx ANTHROPIC_AUTH_TOKEN "test"
```

重启终端使更改生效。

### 运行 Claude Code

```bash
# 确保代理正在运行
# 在另一个终端中运行 Claude Code
claude
```

> **注意：** 如果 Claude Code 要求你选择登录方式，请在 `~/.claude.json`（macOS/Linux）或 `%USERPROFILE%\.claude.json`（Windows）中添加 `"hasCompletedOnboarding": true`，然后重启终端重试。

### 代理模式 vs 付费模式

在 **Settings** → **Claude CLI** 中切换：

| 功能 | 🔌 代理模式 | 💳 付费模式 |
| :--- | :--- | :--- |
| **后端** | 本地服务器 (Antigravity) | 官方 Anthropic Credits |
| **费用** | 免费 (Google Cloud) | 付费 (Anthropic Credits) |
| **模型** | Claude + Gemini | 仅 Claude |

**付费模式** 会自动清除代理设置，以便你直接使用官方 Anthropic 账户。

### 多个 Claude Code 实例（可选）

要同时运行官方 Claude Code 和 Antigravity 版本，请添加以下别名：

**macOS / Linux:**

```bash
# 添加到 ~/.zshrc 或 ~/.bashrc
alias claude-antigravity='CLAUDE_CONFIG_DIR=~/.claude-account-antigravity ANTHROPIC_BASE_URL="http://localhost:8080" ANTHROPIC_AUTH_TOKEN="test" command claude'
```

**Windows (PowerShell):**

```powershell
# 添加到 $PROFILE
function claude-antigravity {
    $env:CLAUDE_CONFIG_DIR = "$env:USERPROFILE\.claude-account-antigravity"
    $env:ANTHROPIC_BASE_URL = "http://localhost:8080"
    $env:ANTHROPIC_AUTH_TOKEN = "test"
    claude
}
```

然后运行 `claude` 使用官方 API，或 `claude-antigravity` 使用此代理。

---

## 文档

- [可用模型](docs/models.md)
- [多账户负载均衡](docs/load-balancing.md)
- [Web 管理控制台](docs/web-console.md)
- [高级配置](docs/configuration.md)
- [macOS 菜单栏应用](docs/menubar-app.md)
- [OpenClaw / ClawdBot 集成](docs/openclaw.md)
- [API 端点](docs/api-endpoints.md)
- [测试](docs/testing.md)
- [故障排除](docs/troubleshooting.md)
- [安全、使用及风险声明](docs/safety-notices.md)
- [法律声明](docs/legal.md)
- [开发指南](docs/development.md)
- [部署指南](docs/DEPLOYMENT.md)
- [Go 后端说明](docs/GO-BACKEND.md)

---

## 致谢

本项目基于以下项目的见解和代码：

- [opencode-antigravity-auth](https://github.com/NoeFabris/opencode-antigravity-auth) - Antigravity OAuth 插件（用于 OpenCode）
- [claude-code-proxy](https://github.com/1rgs/claude-code-proxy) - 使用 LiteLLM 的 Anthropic API 代理

---

## 许可证

MIT

---

<a href="https://buymeacoffee.com/badrinarayanans" target="_blank"><img src="https://cdn.buymeacoffee.com/buttons/v2/default-yellow.png" alt="Buy Me A Coffee" height="50"></a>

## Star 历史

[![Star History Chart](https://api.star-history.com/svg?repos=badrisnarayanan/antigravity-claude-proxy&type=date&legend=top-left&cache-control=no-cache)](https://www.star-history.com/#badrisnarayanan/antigravity-claude-proxy&type=date&legend=top-left)
