# 集成示例

本文档展示了如何将 xiaohongshu-mcp 服务集成到各种 AI 客户端和工作流平台中。

## 🎯 支持的客户端

### 1. Cherry Studio（推荐）

Cherry Studio 是目前最热门的 AI 客户端之一，简单易用且支持多种开源和闭源大模型。

#### 优势
- ✅ 免费开源模型支持
- ✅ 无需 API key
- ✅ 图形化配置界面
- ✅ 简单易用

#### 快速开始

1. **下载 Cherry Studio**
   - 访问 [Cherry Studio 下载页面](https://www.cherry-ai.com/download)
   - 下载适合您操作系统的安装包

2. **启动 xiaohongshu-mcp 服务**
   ```bash
   # 登录小红书账号
   go run cmd/login/main.go
   
   # 启动 MCP 服务
   go run .
   ```

3. **配置 MCP 服务器**
   - 打开 Cherry Studio 设置
   - 选择 "MCP" 标签页
   - 添加新服务器：
     - 名称: xiaohongshu-mcp
     - 类型: streamableHttp
     - URL: http://localhost:18060/mcp

4. **开始使用**
   - 创建新对话
   - 选择模型（推荐 GLM-4.5-Flash）
   - 启用 xiaohongshu-mcp 工具
   - 通过自然语言使用功能

#### 功能演示
- ✅ 检查登录状态
- ✅ 小红书站内搜索
- ✅ 发布图文内容
- ✅ 浏览推荐页

### 2. AnythingLLM

AnythingLLM 是一款 all-in-one 多模态 AI 客户端，支持 workflow 定义和多种大模型。

#### 优势
- ✅ 支持本地笔记 → 润色 → 批量发布
- ✅ 节省 token 成本
- ✅ 支持免费开源模型
- ✅ 工作流自动化

#### 快速开始

1. **下载 AnythingLLM**
   - 访问 [AnythingLLM 下载页面](https://anythingllm.com/desktop)

2. **配置 MCP 服务器**
   - 编辑配置文件：`~/Library/Application Support/anythingllm-desktop/storage/plugins/anythingllm_mcp_servers.json`
   - 添加配置：
     ```json
     {
       "mcpServers": {
         "xiaohongshu-mcp": {
           "type": "streamable",
           "url": "http://127.0.0.1:18060/mcp"
         }
       }
     }
     ```

3. **使用方式**
   - **直接调用**：在对话中输入 `@agent 使用xiaohongshu-mcp 检查登录状态`
   - **工作流自动化**：创建 Agent Workflow 实现本地笔记自动发布

#### 工作流示例
```
1. 读取本地笔记文件
2. 使用 LLM 润色内容
3. 调用 xiaohongshu-mcp 发布到小红书
4. 返回发布结果
```

### 3. Claude Code + Kimi-K2

使用国内 Kimi-K2 模型替代 Claude Code，降低使用门槛。

#### 优势
- ✅ 国内模型，访问稳定
- ✅ 成本更低
- ✅ 功能完整

#### 快速开始

1. **申请 Kimi API Key**
   - 访问 [Kimi 开放平台](https://platform.moonshot.cn/)
   - 创建 API Key

2. **一键安装**
   ```bash
   bash -c "$(curl -fsSL https://raw.githubusercontent.com/LLM-Red-Team/kimi-cc/refs/heads/main/install.sh)"
   ```

3. **配置 MCP**
   - 下载 xiaohongshu-mcp 二进制文件
   - 按照 README 文档配置 MCP 连接

### 4. N8N 工作流平台

N8N 是强大的工作流自动化平台，支持汉化界面。

#### 优势
- ✅ 可视化工作流设计
- ✅ 支持多种数据源
- ✅ 汉化界面
- ✅ 强大的自动化能力

#### 快速开始

1. **部署 N8N**
   ```yaml
   # docker-compose.yml
   version: '3'
   services:
     n8n:
       image: n8nio/n8n
       container_name: n8n
       restart: unless-stopped
       ports:
         - "5678:5678"
       volumes:
         - ./n8n_data:/home/node/.n8n
         - ./editor-ui/dist:/usr/local/lib/node_modules/n8n/node_modules/n8n-editor-ui/dist
       environment:
         - N8N_DEFAULT_LOCALE=zh-CN
         - N8N_SECURE_COOKIE=false
   ```

2. **导入工作流**
   - 下载汉化包
   - 导入 `自动发布笔记到小红书.json` 工作流
   - 配置大模型节点（DeepSeek 等）
   - 配置 MCP 服务连接

3. **使用示例**
   ```
   给我发布一篇关于重庆旅游的小红书爆款笔记，配图找"重庆打卡"点赞最高的一张
   ```

## 🔧 通用配置

### MCP 服务配置

所有客户端都需要配置 MCP 服务器：

```json
{
  "name": "xiaohongshu-mcp",
  "type": "streamableHttp",
  "url": "http://localhost:18060/mcp"
}
```

### 前置条件

1. **启动 xiaohongshu-mcp 服务**
   ```bash
   # 登录小红书
   go run cmd/login/main.go
   
   # 启动服务
   go run .
   ```

2. **确保服务正常运行**
   ```bash
   # 检查服务状态
   curl http://localhost:18060/health
   ```

## 🎯 使用场景

### 内容创作场景

1. **本地笔记发布**
   - 读取本地 Markdown 文件
   - AI 润色优化内容
   - 自动发布到小红书

2. **批量内容管理**
   - 搜索相关内容
   - 批量点赞收藏
   - 智能浏览推荐页

3. **数据分析**
   - 获取用户主页信息
   - 分析内容表现
   - 生成运营报告

### 工作流自动化

1. **定时发布**
   - 设置定时任务
   - 自动生成内容
   - 定时发布到小红书

2. **内容监控**
   - 监控关键词
   - 自动互动
   - 数据收集分析

## 🛠️ 故障排查

### 常见问题

1. **连接失败**
   - 检查 xiaohongshu-mcp 服务是否启动
   - 确认端口 18060 未被占用
   - 验证网络连接

2. **登录问题**
   - 重新运行登录程序
   - 检查 cookies.json 文件
   - 确认登录状态

3. **功能异常**
   - 查看服务日志
   - 检查浏览器环境
   - 验证权限设置

### 获取帮助

- 查看 [疑难杂症](https://github.com/xpzouying/xiaohongshu-mcp/issues/56)
- 参考 [API 文档](./API.md)
- 检查 [技术实现细节](./TECHNICAL_DETAILS.md)

## 📁 项目文件

```
examples/
├── README.md                    # 示例索引
├── cherrystudio/                # Cherry Studio 集成
│   ├── README.md
│   └── images/
├── anythingLLM/                 # AnythingLLM 集成
│   ├── readme.md
│   └── images/
├── claude-code/                 # Claude Code 集成
│   └── claude-code-kimi-k2.md
└── n8n/                        # N8N 集成
    ├── README.md
    ├── docker-compose.yml
    ├── 自动发布笔记到小红书.json
    └── images/
```

---

**提示**：选择适合您需求的客户端，推荐新手使用 Cherry Studio，高级用户可以选择 N8N 进行复杂的工作流设计。
