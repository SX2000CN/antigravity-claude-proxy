# 高级配置 (Advanced Configuration)

虽然大多数用户可以使用默认设置，但你也可以通过 WebUI 中的 **Settings → Server** 选项卡或创建一个 `config.json` 文件来调整代理行为。

## 环境变量 (Environment Variables)

代理支持以下环境变量：

| 变量 | 描述 | 默认值 |
|----------|-------------|---------|
| `PORT` | 服务器端口 | `8080` |
| `HOST` | 绑定地址 | `0.0.0.0` |
| `HTTP_PROXY` | 通过代理路由出站请求 | - |
| `HTTPS_PROXY` | 同 HTTP_PROXY (用于 HTTPS 请求) | - |
| `API_KEY` | 保护 `/v1/*` API 端点 | - |
| `WEBUI_PASSWORD` | 密码保护 Web 仪表盘 | - |
| `DEBUG` | 启用调试日志 (`true`/`false`) | `false` |
| `DEV_MODE` | 启用开发者模式 (`true`/`false`) | `false` |
| `FALLBACK` | 启用模型回退 (`true`/`false`) | `false` |

### 设置环境变量

#### 内联 (单次命令)

仅为一次命令设置变量。适用于 macOS、Linux 以及带有 Git Bash/WSL 的 Windows：

```bash
PORT=3000 HTTP_PROXY=http://proxy:8080 npm start
```

#### macOS / Linux (持久化)

添加到你的 shell 配置文件 (`~/.zshrc` 或 `~/.bashrc`)：

```bash
export PORT=3000
export HTTP_PROXY=http://proxy:8080
```

然后重新加载：`source ~/.zshrc`

#### Windows Command Prompt (持久化)

```cmd
setx PORT 3000
setx HTTP_PROXY http://proxy:8080
```

重启终端以使更改生效。

#### Windows PowerShell (持久化)

```powershell
[Environment]::SetEnvironmentVariable("PORT", "3000", "User")
[Environment]::SetEnvironmentVariable("HTTP_PROXY", "http://proxy:8080", "User")
```

重启终端以使更改生效。

### HTTP 代理支持

如果你在公司防火墙或 VPN 后面，你可以通过代理服务器路由所有出站 API 请求：

```bash
# 通过本地代理路由 (例如，用于使用 mitmproxy 调试)
HTTP_PROXY=http://127.0.0.1:8888 npm start

# 通过公司代理路由
HTTP_PROXY=http://proxy.company.com:3128 npm start

# 带认证的代理
HTTP_PROXY=http://user:password@proxy.company.com:3128 npm start
```

代理支持 `http_proxy`, `HTTP_PROXY`, `https_proxy`, 和 `HTTPS_PROXY` (不区分大小写)。

## 可配置选项

- **API Key 认证**：使用 `API_KEY` 环境变量或配置中的 `apiKey` 保护 `/v1/*` API 端点。
- **WebUI 密码**：使用 `WEBUI_PASSWORD` 环境变量或配置保护你的仪表盘。
- **自定义端口**：更改默认的 `8080` 端口。
- **重试逻辑**：配置 `maxRetries`, `retryBaseMs`, 和 `retryMaxMs`。
- **速率限制处理**：从头部和错误消息中进行全面的速率限制检测，支持智能的 retry-after 解析。
- **负载均衡**：调整 `defaultCooldownMs` 和 `maxWaitBeforeErrorMs`。
- **持久化**：启用 `persistTokenCache` 以在重启后保存 OAuth 会话。
- **最大账户数**：设置 `maxAccounts` (1-100) 以限制 Google 账户的数量。默认值：10。
- **配额阈值**：设置 `globalQuotaThreshold` (0-0.99) 以在配额降至最低水平之前切换账户。支持每账户和每模型覆盖。
- **端点回退**：自动 403/404 端点回退以实现 API 兼容性。

有关字段和文档的完整列表，请参阅 `config.example.json`。
