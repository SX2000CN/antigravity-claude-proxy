# 多账户负载均衡

当你添加多个账户时，代理会使用可配置的选择策略在它们之间智能地分配请求。

## 账户选择策略

根据你的需求选择策略：

| 策略 | 适用场景 | 描述 |
| --- | --- | --- |
| **Hybrid** (混合模式，默认) | 大多数用户 | 结合健康评分、令牌桶速率限制、配额感知和 LRU 新鲜度的智能选择 |
| **Sticky** (粘性模式) | Prompt 缓存 | 停留在同一账户以最大化缓存命中率，仅在受限流时切换 |
| **Round-Robin** (轮询模式) | 均匀分布 | 按顺序循环使用账户以实现负载均衡 |

**通过 CLI 配置：**

```bash
antigravity-claude-proxy start --strategy=hybrid    # 默认：智能分配
antigravity-claude-proxy start --strategy=sticky    # 缓存优化
antigravity-claude-proxy start --strategy=round-robin  # 负载均衡
```

**或通过 WebUI 配置：** 设置 (Settings) → 服务器 (Server) → 账户选择策略 (Account Selection Strategy)

## 工作原理

- **健康评分跟踪**：账户因成功请求获得积分，因失败/限流失去积分
- **令牌桶速率限制**：客户端节流，令牌自动再生（最大 50 个，每分钟 6 个）
- **配额感知**：低于可配置配额阈值的账户优先级降低；耗尽的账户触发紧急回退
- **配额保护**：设置全局、单账户或单模型的最低配额水平，以便在配额耗尽前切换账户
- **紧急回退**：当所有账户看似耗尽时，绕过检查并增加节流延迟（250-500ms）
- **自动冷却**：受限流账户在重置时间到期后自动恢复
- **无效账户检测**：需要重新认证的账户会被标记并跳过
- **Prompt 缓存支持**：从对话派生的会话 ID 支持跨轮次的缓存命中

## 监控

随时检查账户状态、订阅层级和配额：

```bash
# Web UI: http://localhost:8080/ (Accounts 标签页 - 显示层级徽章和配额进度)
# CLI 表格:
curl "http://localhost:8080/account-limits?format=table"
```

### CLI 管理参考

如果你更喜欢使用终端进行管理：

```bash
# 列出所有账户
antigravity-claude-proxy accounts list

# 验证账户健康状况
antigravity-claude-proxy accounts verify

# 交互式 CLI 菜单
antigravity-claude-proxy accounts
```
