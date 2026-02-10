# 测试 (Testing)

运行测试套件（需要服务器正在运行）：

```bash
# 在一个终端启动服务器
npm start

# 在另一个终端运行测试
npm test
```

单独运行测试：

```bash
npm run test:signatures    # Thinking signatures (思维签名)
npm run test:multiturn     # Multi-turn with tools (多轮对话与工具)
npm run test:streaming     # Streaming SSE events (流式 SSE 事件)
npm run test:interleaved   # Interleaved thinking (交错思维)
npm run test:images        # Image processing (图像处理)
npm run test:caching       # Prompt caching (Prompt 缓存)
npm run test:strategies    # Account selection strategies (账户选择策略)
npm run test:cache-control # Cache control field stripping (缓存控制字段剥离)
```
