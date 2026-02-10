# API 端点

| 端点               | 方法   | 描述                                                                 |
| ----------------- | ------ | -------------------------------------------------------------------- |
| `/health`         | GET    | 健康检查                                                             |
| `/account-limits` | GET    | 账户状态和配额限制（添加 `?format=table` 以获取 ASCII 表格）           |
| `/v1/messages`    | POST   | Anthropic Messages API                                               |
| `/v1/models`      | GET    | 列出可用模型                                                         |
| `/refresh-token`  | POST   | 强制刷新令牌                                                         |
