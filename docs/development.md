# 开发指南

## 开发者与贡献者

本项目使用本地 Tailwind CSS 构建系统。CSS 已预编译并包含在仓库中，因此克隆后即可立即运行项目。

### 快速开始

```bash
git clone https://github.com/badri-s2001/antigravity-claude-proxy.git
cd antigravity-claude-proxy
npm install  # 通过 prepare 钩子自动构建 CSS
npm start    # 启动服务器（无需重新构建）
```

### 前端开发

如果你需要修改 `public/css/src/input.css` 中的样式：

```bash
# 选项 1: 构建一次
npm run build:css

# 选项 2: 监听变更（自动重新构建）
npm run watch:css

# 选项 3: 同时监听 CSS 和服务器（推荐）
npm run dev:full
```

**文件结构：**
- `public/css/src/input.css` - 包含 Tailwind `@apply` 指令的源 CSS（编辑此文件）
- `public/css/style.css` - 编译并压缩后的 CSS（自动生成，请勿编辑）
- `tailwind.config.js` - Tailwind 配置
- `postcss.config.js` - PostCSS 配置

### 仅后端开发

如果你只处理后端代码，不需要前端开发工具：

```bash
npm install --production  # 跳过 devDependencies（节省约 20MB）
npm start
```

**注意：** 预编译的 CSS 已提交到仓库，除非修改样式，否则无需重新构建。

### 项目结构

详见 [CLAUDE.md](../CLAUDE.md) 获取详细的架构文档，包括：
- 请求流程和模块组织
- 前端架构 (Alpine.js + Tailwind)
- 服务层模式 (`ErrorHandler.withLoading`, `AccountActions`)
- 仪表盘模块文档
