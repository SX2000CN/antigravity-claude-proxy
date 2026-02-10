# 与 OpenClaw / ClawdBot 一起使用

[OpenClaw](https://docs.openclaw.ai/)（前身为 ClawdBot/Moltbot）是一个 AI 代理网关，可连接 Telegram、WhatsApp、Discord、Slack 和 iMessage 等消息应用。你可以将其配置为使用此代理来访问 Claude 和 Gemini 模型。

## 先决条件

- 已安装 OpenClaw (`npm install -g openclaw@latest`)
- Antigravity Claude Proxy 正在运行（端口 8080）
- 至少有一个 Google 账号已链接到代理

## 配置 OpenClaw

编辑你的 OpenClaw 配置文件：
- **macOS/Linux**: `~/.openclaw/openclaw.json`
- **Windows**: `%USERPROFILE%\.openclaw\openclaw.json`

添加以下配置：

```json
{
  "models": {
    "mode": "merge",
    "providers": {
      "antigravity-proxy": {
        "baseUrl": "http://127.0.0.1:8080",
        "apiKey": "test",
        "api": "anthropic-messages",
        "models": [
          {
            "id": "gemini-3-flash",
            "name": "Gemini 3 Flash",
            "reasoning": true,
            "input": ["text", "image"],
            "cost": { "input": 0, "output": 0, "cacheRead": 0, "cacheWrite": 0 },
            "contextWindow": 1048576,
            "maxTokens": 65536
          },
          {
            "id": "gemini-3-pro-high",
            "name": "Gemini 3 Pro High",
            "reasoning": true,
            "input": ["text", "image"],
            "cost": { "input": 0, "output": 0, "cacheRead": 0, "cacheWrite": 0 },
            "contextWindow": 1048576,
            "maxTokens": 65536
          },
          {
            "id": "claude-sonnet-4-5",
            "name": "Claude Sonnet 4.5",
            "reasoning": false,
            "input": ["text", "image"],
            "cost": { "input": 0, "output": 0, "cacheRead": 0, "cacheWrite": 0 },
            "contextWindow": 200000,
            "maxTokens": 16384
          },
          {
            "id": "claude-sonnet-4-5-thinking",
            "name": "Claude Sonnet 4.5 Thinking",
            "reasoning": true,
            "input": ["text", "image"],
            "cost": { "input": 0, "output": 0, "cacheRead": 0, "cacheWrite": 0 },
            "contextWindow": 200000,
            "maxTokens": 16384
          },
          {
            "id": "claude-opus-4-5-thinking",
            "name": "Claude Opus 4.5 Thinking",
            "reasoning": true,
            "input": ["text", "image"],
            "cost": { "input": 0, "output": 0, "cacheRead": 0, "cacheWrite": 0 },
            "contextWindow": 200000,
            "maxTokens": 32000
          }
        ]
      }
    }
  },
  "agents": {
    "defaults": {
      "model": {
        "primary": "antigravity-proxy/gemini-3-flash",
        "fallbacks": ["antigravity-proxy/gemini-3-pro-high"]
      },
      "models": {
        "antigravity-proxy/gemini-3-flash": {}
      }
    }
  }
}
```

> **重要提示**：在 `baseUrl` 中使用 `127.0.0.1` 而不是 `localhost`。这可以确保连接保持在环回接口上。如果你在 VPS 上运行，并且意外启动了绑定到 `0.0.0.0` 的代理，在客户端配置中使用 `localhost` 可能仍然有效，但 `127.0.0.1` 明确了意图并避免了潜在的 DNS 解析问题。

## 启动两个服务

```bash
# 终端 1：启动代理（默认仅绑定到 localhost）
antigravity-claude-proxy start

# 终端 2：启动 OpenClaw 网关
openclaw gateway
```

## 验证配置

```bash
# 检查可用模型
openclaw models list

# 检查网关状态
openclaw status
```

你应该在列表中看到以 `antigravity-proxy/` 为前缀的模型。

## 切换模型

要更改默认模型：

```bash
openclaw models set antigravity-proxy/claude-opus-4-5-thinking
```

或者编辑配置文件中的 `model.primary` 字段。

## 故障排除

### 连接被拒绝 (Connection Refused)

在启动 OpenClaw 之前，请确保代理正在运行：
```bash
curl http://127.0.0.1:8080/health
```

### 模型未显示

1. 验证配置文件是否为有效的 JSON
2. 检查 `mode` 是否设置为 `"merge"`（不要设置为 `"replace"`，除非你想覆盖所有内置模型）
3. 更改配置后重启 OpenClaw 网关

### VPS 安全

如果在 VPS 上运行，请确保代理仅绑定到 localhost：
```bash
# 默认绑定到 0.0.0.0（所有接口）- 暴露在网络中！
antigravity-claude-proxy start

# 显式绑定到 localhost（VPS 推荐）
HOST=127.0.0.1 antigravity-claude-proxy start
```

默认情况下，代理绑定到 `0.0.0.0`，这会将其暴露给所有网络接口。在 VPS 上，务必使用 `HOST=127.0.0.1` 将访问限制为仅限 localhost，或者确保你有适当的身份验证（`API_KEY` 环境变量）和防火墙规则。

## 延伸阅读

- [OpenClaw 文档](https://docs.openclaw.ai/)
- [OpenClaw 配置参考](https://docs.openclaw.ai/gateway/configuration)
- [代理负载均衡](./load-balancing.md)
- [代理配置](./configuration.md)
