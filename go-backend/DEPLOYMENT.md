# Antigravity Claude Proxy - 部署指南

本文档介绍如何在各种环境中部署 Go 版本的 Antigravity Claude Proxy。

## 目录

- [快速部署](#快速部署)
- [Linux 服务器部署](#linux-服务器部署)
- [Windows 部署](#windows-部署)
- [Docker 部署](#docker-部署)
- [反向代理配置](#反向代理配置)
- [故障排除](#故障排除)

---

## 快速部署

### 1. 下载或编译

**方式 A: 从源码编译**

```bash
# 克隆仓库
git clone https://github.com/badri-s2001/antigravity-claude-proxy.git
cd antigravity-claude-proxy/go-backend

# 编译
go build -ldflags="-s -w" -o build/antigravity-proxy ./cmd/server
```

**方式 B: 交叉编译**

```bash
# 在 Windows 上编译 Linux 版本
$env:GOOS="linux"; $env:GOARCH="amd64"
go build -ldflags="-s -w" -o build/antigravity-proxy-linux ./cmd/server

# 在 Linux/macOS 上编译 Windows 版本
GOOS=windows GOARCH=amd64 go build -ldflags="-s -w" -o build/antigravity-proxy.exe ./cmd/server
```

### 2. 运行

```bash
# 从项目根目录运行（确保 public 目录可访问）
cd antigravity-claude-proxy
./go-backend/build/antigravity-proxy --dev-mode
```

---

## Linux 服务器部署

### 使用 systemd (推荐)

#### 1. 复制文件

```bash
# 创建目录
sudo mkdir -p /opt/antigravity-proxy
sudo mkdir -p /opt/antigravity-proxy/public

# 复制二进制和前端文件
sudo cp go-backend/build/antigravity-proxy /opt/antigravity-proxy/
sudo cp -r public/* /opt/antigravity-proxy/public/

# 设置权限
sudo chmod +x /opt/antigravity-proxy/antigravity-proxy
```

#### 2. 创建 systemd 服务

```bash
sudo tee /etc/systemd/system/antigravity-proxy.service << 'EOF'
[Unit]
Description=Antigravity Claude Proxy
After=network.target redis.service
Wants=redis.service

[Service]
Type=simple
User=root
WorkingDirectory=/opt/antigravity-proxy
ExecStart=/opt/antigravity-proxy/antigravity-proxy --strategy=hybrid
Restart=always
RestartSec=5
StandardOutput=journal
StandardError=journal

# 环境变量
Environment=PORT=8080
Environment=HOST=0.0.0.0
Environment=REDIS_ADDR=localhost:6379
# Environment=API_KEY=your-api-key
# Environment=WEBUI_PASSWORD=your-password

[Install]
WantedBy=multi-user.target
EOF
```

#### 3. 启动服务

```bash
# 重载 systemd
sudo systemctl daemon-reload

# 启动服务
sudo systemctl start antigravity-proxy

# 设置开机启动
sudo systemctl enable antigravity-proxy

# 查看状态
sudo systemctl status antigravity-proxy

# 查看日志
sudo journalctl -u antigravity-proxy -f
```

#### 4. 管理命令

```bash
# 停止服务
sudo systemctl stop antigravity-proxy

# 重启服务
sudo systemctl restart antigravity-proxy

# 禁用开机启动
sudo systemctl disable antigravity-proxy
```

### Redis 安装 (可选)

```bash
# Ubuntu/Debian
sudo apt update
sudo apt install redis-server
sudo systemctl enable redis-server
sudo systemctl start redis-server

# CentOS/RHEL
sudo yum install redis
sudo systemctl enable redis
sudo systemctl start redis

# 验证
redis-cli ping  # 应返回 PONG
```

---

## Windows 部署

### 方式 A: 直接运行

```powershell
# 编译
cd antigravity-claude-proxy\go-backend
go build -ldflags="-s -w" -o build\antigravity-proxy.exe .\cmd\server

# 运行（从项目根目录）
cd ..
.\go-backend\build\antigravity-proxy.exe --dev-mode
```

### 方式 B: 作为 Windows 服务

使用 [NSSM](https://nssm.cc/) (Non-Sucking Service Manager):

```powershell
# 1. 下载 NSSM
# https://nssm.cc/download

# 2. 安装服务
nssm install AntigravityProxy "C:\path\to\antigravity-proxy.exe"
nssm set AntigravityProxy AppDirectory "C:\path\to\antigravity-claude-proxy"
nssm set AntigravityProxy AppParameters "--strategy=hybrid"

# 3. 配置环境变量
nssm set AntigravityProxy AppEnvironmentExtra "PORT=8080" "HOST=0.0.0.0"

# 4. 启动服务
nssm start AntigravityProxy

# 管理
nssm status AntigravityProxy
nssm stop AntigravityProxy
nssm restart AntigravityProxy
nssm remove AntigravityProxy confirm
```

### 方式 C: 任务计划程序

1. 打开 "任务计划程序"
2. 创建基本任务
3. 设置触发器为 "计算机启动时"
4. 操作选择 "启动程序"
5. 程序路径: `C:\path\to\antigravity-proxy.exe`
6. 起始目录: `C:\path\to\antigravity-claude-proxy`
7. 添加参数: `--strategy=hybrid`

---

## Docker 部署

### 方式 A: 使用 Docker Hub 镜像 (推荐)

最简单的部署方式，使用预构建镜像，无需本地编译：

```bash
cd go-backend

# 启动服务（自动拉取镜像并启动 Redis + Proxy）
docker-compose up -d

# 查看日志
docker-compose logs -f proxy

# 停止服务
docker-compose down

# 更新到最新版本
docker-compose pull && docker-compose up -d
```

**Docker Hub 镜像**: [`sx2000/antigravity-proxy-go:latest`](https://hub.docker.com/r/sx2000/antigravity-proxy-go)

#### 配置说明

`docker-compose.yml` 中的关键配置：

```yaml
services:
  proxy:
    image: sx2000/antigravity-proxy-go:latest  # 预构建镜像
    environment:
      - REDIS_ADDR=redis:6379
      - PORT=8080
    volumes:
      # 前端静态文件（必需）
      - ../public:/app/public:ro
      # 配置文件持久化（Windows）
      - ${USERPROFILE}/.config/antigravity-proxy:/home/appuser/.config/antigravity-proxy
```

#### Windows 配置文件位置

配置文件位于 `%USERPROFILE%\.config\antigravity-proxy\`：
- `config.json` - 服务器配置
- `accounts.json` - 账户数据

### 方式 B: 本地构建镜像

如果需要自定义修改，可以使用本地 Dockerfile 构建：

#### Dockerfile

```dockerfile
# 构建阶段
FROM golang:1.21-alpine AS builder

WORKDIR /app
COPY go-backend/go.mod go-backend/go.sum ./
RUN go mod download

COPY go-backend/ ./
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o antigravity-proxy ./cmd/server

# 运行阶段
FROM alpine:latest

RUN apk --no-cache add ca-certificates tzdata

WORKDIR /app
COPY --from=builder /app/antigravity-proxy .
COPY public/ ./public/

EXPOSE 8080

ENV PORT=8080
ENV HOST=0.0.0.0

ENTRYPOINT ["./antigravity-proxy"]
CMD ["--strategy=hybrid"]
```

### docker-compose.yml (本地构建版本)

如果使用本地构建而非 Docker Hub 镜像：

```yaml
version: '3.8'

services:
  proxy:
    build:
      context: .
      dockerfile: Dockerfile
    container_name: antigravity-proxy
    ports:
      - "8080:8080"
    environment:
      - PORT=8080
      - HOST=0.0.0.0
      - REDIS_ADDR=redis:6379
      - API_KEY=${API_KEY:-}
      - WEBUI_PASSWORD=${WEBUI_PASSWORD:-}
    volumes:
      - config-data:/root/.config/antigravity-proxy
    depends_on:
      - redis
    restart: unless-stopped

  redis:
    image: redis:7-alpine
    container_name: antigravity-redis
    volumes:
      - redis-data:/data
    command: redis-server --appendonly yes
    restart: unless-stopped

volumes:
  config-data:
  redis-data:
```

### 本地构建运行

```bash
# 构建并启动
docker-compose up -d

# 查看日志
docker-compose logs -f proxy

# 停止
docker-compose down
```

---

## 反向代理配置

### Nginx

```nginx
upstream antigravity {
    server 127.0.0.1:8080;
    keepalive 32;
}

server {
    listen 80;
    server_name proxy.example.com;
    return 301 https://$server_name$request_uri;
}

server {
    listen 443 ssl http2;
    server_name proxy.example.com;

    ssl_certificate /etc/letsencrypt/live/proxy.example.com/fullchain.pem;
    ssl_certificate_key /etc/letsencrypt/live/proxy.example.com/privkey.pem;

    # SSE 支持
    proxy_buffering off;
    proxy_cache off;

    location / {
        proxy_pass http://antigravity;
        proxy_http_version 1.1;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;

        # SSE 必需
        proxy_set_header Connection '';
        proxy_read_timeout 86400s;
        chunked_transfer_encoding on;
    }

    # WebSocket 支持 (如需要)
    location /ws {
        proxy_pass http://antigravity;
        proxy_http_version 1.1;
        proxy_set_header Upgrade $http_upgrade;
        proxy_set_header Connection "upgrade";
    }
}
```

### Caddy

```caddyfile
proxy.example.com {
    reverse_proxy localhost:8080 {
        flush_interval -1
        transport http {
            keepalive 30s
            keepalive_idle_conns 10
        }
    }
}
```

### Cloudflare Tunnel

```bash
# 安装 cloudflared
# https://developers.cloudflare.com/cloudflare-one/connections/connect-apps/install-and-setup/

# 创建隧道
cloudflared tunnel create antigravity-proxy

# 配置 ~/.cloudflared/config.yml
tunnel: <TUNNEL-ID>
credentials-file: /root/.cloudflared/<TUNNEL-ID>.json

ingress:
  - hostname: proxy.example.com
    service: http://localhost:8080
  - service: http_status:404

# 运行
cloudflared tunnel run antigravity-proxy
```

---

## 配置持久化

### 账户数据

账户数据保存在 `~/.config/antigravity-proxy/accounts.json`

```bash
# 备份
cp ~/.config/antigravity-proxy/accounts.json ~/backup/

# 恢复
cp ~/backup/accounts.json ~/.config/antigravity-proxy/
```

### 配置文件

```bash
# 配置文件位置
~/.config/antigravity-proxy/config.json

# 示例配置
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
  "port": 8080
}
```

---

## 健康检查

### 基本检查

```bash
# 健康状态
curl http://localhost:8080/health

# 账户配额
curl http://localhost:8080/account-limits

# 获取配置
curl http://localhost:8080/api/config
```

### 监控脚本

```bash
#!/bin/bash
# healthcheck.sh

ENDPOINT="http://localhost:8080/health"
TIMEOUT=10

if curl -sf --max-time $TIMEOUT "$ENDPOINT" > /dev/null; then
    echo "OK"
    exit 0
else
    echo "FAIL"
    exit 1
fi
```

---

## 故障排除

### 常见问题

#### 1. 无法连接到 Redis

```bash
# 检查 Redis 状态
redis-cli ping

# 检查端口
netstat -tlnp | grep 6379

# 允许远程连接 (如需要)
# 编辑 /etc/redis/redis.conf
# 将 bind 127.0.0.1 改为 bind 0.0.0.0
```

#### 2. 端口被占用

```bash
# 查找占用端口的进程
lsof -i :8080
netstat -tlnp | grep 8080

# 更换端口
./antigravity-proxy --port=3000
```

#### 3. 权限问题

```bash
# 确保二进制可执行
chmod +x /opt/antigravity-proxy/antigravity-proxy

# 确保配置目录可写
mkdir -p ~/.config/antigravity-proxy
chmod 755 ~/.config/antigravity-proxy
```

#### 4. SSE 流中断

- 确保反向代理禁用了 buffering
- 检查超时设置 (建议 > 10 分钟)
- 确保 `proxy_http_version 1.1`

#### 5. 内存使用高

```bash
# 检查内存使用
ps aux | grep antigravity

# 正常情况下应在 50-100MB
# 如超过 500MB，可能存在泄漏，建议重启服务
```

### 日志排查

```bash
# systemd 日志
journalctl -u antigravity-proxy -f

# 启用详细日志
./antigravity-proxy --dev-mode

# Docker 日志
docker logs -f antigravity-proxy
```

---

## 安全建议

1. **设置 API Key**: 通过 `API_KEY` 环境变量或配置文件
2. **设置 WebUI 密码**: 通过 `WEBUI_PASSWORD` 保护管理界面
3. **使用 HTTPS**: 通过 Nginx/Caddy 反向代理启用 SSL
4. **防火墙**: 限制 8080 端口的访问来源
5. **定期更新**: 关注仓库更新，及时升级

---

## 升级

```bash
# 1. 停止服务
sudo systemctl stop antigravity-proxy

# 2. 备份配置
cp -r ~/.config/antigravity-proxy ~/backup/

# 3. 更新代码
cd antigravity-claude-proxy
git pull origin main

# 4. 重新编译
cd go-backend
go build -ldflags="-s -w" -o build/antigravity-proxy ./cmd/server

# 5. 复制新二进制
sudo cp build/antigravity-proxy /opt/antigravity-proxy/

# 6. 重启服务
sudo systemctl start antigravity-proxy
```
