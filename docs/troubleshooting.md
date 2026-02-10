# 故障排除 (Troubleshooting)

## 快速链接

- [Windows: OAuth 端口错误 (EACCES)](#windows-oauth-port-error-eacces)
- ["Could not extract token from Antigravity"](#could-not-extract-token-from-antigravity)
- [401 认证错误](#401-authentication-errors)
- [速率限制 (Rate Limiting 429)](#rate-limiting-429)
- [账户显示为 "Invalid" (无效)](#account-shows-as-invalid)
- [403 权限被拒绝 (Permission Denied)](#403-permission-denied)

---

## Windows: OAuth 端口错误 (EACCES)

在 Windows 上，默认的 OAuth 回调端口 (51121) 可能被 Hyper-V、WSL2 或 Docker 保留。如果你看到：

```
Error: listen EACCES: permission denied 0.0.0.0:51121
```

代理会自动尝试备用端口 (51122-51126)。如果所有端口都失败，请尝试以下解决方案：

### 选项 1: 使用自定义端口（推荐）

设置一个保留范围之外的自定义端口：

```bash
# Windows PowerShell
$env:OAUTH_CALLBACK_PORT = "3456"
antigravity-claude-proxy start

# Windows CMD
set OAUTH_CALLBACK_PORT=3456
antigravity-claude-proxy start

# 或者添加到你的 .env 文件
OAUTH_CALLBACK_PORT=3456
```

### 选项 2: 重置 Windows NAT

以管理员身份运行：

```powershell
net stop winnat
net start winnat
```

### 选项 3: 检查保留端口

查看哪些端口被保留：

```powershell
netsh interface ipv4 show excludedportrange protocol=tcp
```

如果 51121 在保留范围内，请使用选项 1 设置该范围之外的端口。

### 选项 4: 永久排除端口（管理员）

在 Hyper-V 占用端口之前保留它（以管理员身份运行）：

```powershell
netsh int ipv4 add excludedportrange protocol=tcp startport=51121 numberofports=1
```

> **注意：** 如果主端口失败，服务器会自动尝试备用端口 (51122-51126)。

---

## "Could not extract token from Antigravity"

如果使用 Antigravity 单账户模式：

1. 确保 Antigravity 应用程序已安装并正在运行
2. 确保你已登录 Antigravity

或者通过 OAuth 添加账户：`antigravity-claude-proxy accounts add`

## 401 认证错误 (Authentication Errors)

Token 可能已过期。尝试：

```bash
curl -X POST http://localhost:8080/refresh-token
```

或者重新认证该账户：

```bash
antigravity-claude-proxy accounts
```

## 速率限制 (Rate Limiting 429)

如果有多个账户，代理会自动切换到下一个可用账户。如果是单账户，你需要等待速率限制重置。

## 账户显示为 "Invalid" (无效)

重新认证该账户：

```bash
antigravity-claude-proxy accounts
# 为无效账户选择 "Re-authenticate"
```

## 403 权限被拒绝 (Permission Denied)

如果你看到：

```
403 permission_error - Permission denied
```

这通常意味着你的 Google 账户需要电话号码验证：

1. 从 https://antigravity.google/download 下载 Antigravity 应用
2. 登录受影响的账户
3. 根据提示完成电话号码验证（或在 Android 上使用二维码）
4. 验证后，该账户应能正常配合代理使用

> **注意：** 此验证是 Google 要求的，无法通过代理绕过。
