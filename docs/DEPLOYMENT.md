# 部署指南

本文档涵盖了小红书 MCP 服务的各种部署方式，包括 Docker、macOS 后台运行等。

## 🐳 Docker 部署（推荐）

### 快速开始

#### 1. 获取 Docker 镜像

**从 Docker Hub 拉取（推荐）**

```bash
# 拉取最新镜像
docker pull xpzouying/xiaohongshu-mcp
```

Docker Hub 地址：[https://hub.docker.com/r/xpzouying/xiaohongshu-mcp](https://hub.docker.com/r/xpzouying/xiaohongshu-mcp)

**自己构建镜像（可选）**

```bash
# 在项目根目录运行
docker build -t xpzouying/xiaohongshu-mcp .
```

#### 2. 使用 Docker Compose

```bash
# 启动服务
docker compose up -d

# 查看日志
docker compose logs -f

# 停止服务
docker compose stop

# 更新服务
docker compose pull && docker compose up -d
```

#### 3. 重要注意事项

- 启动后会产生 `images/` 目录，用于存储发布的图片
- 如果要使用本地图片发布，请确保图片拷贝到 `./images/` 目录下
- 在 MCP 发布时，指定图片路径为：`/app/images/图片名`

#### 4. 验证部署

使用 MCP-Inspector 连接测试：

1. 打开 MCP-Inspector
2. 输入服务器地址（注意替换为你的实际 IP）
3. 验证连接成功

#### 5. 登录配置

1. **重要**：提前打开小红书 App，准备扫码登录
2. 尽快扫码，二维码可能会过期
3. 扫码成功后，再次扫码会提示已登录

## 🍎 macOS 后台运行

### 系统服务管理

通过 macOS 的 LaunchAgent 系统管理小红书 MCP 服务。

#### 1. 安装配置

1. **编辑配置文件**
   - 打开 `deploy/macos/xhsmcp.plist`
   - 替换 `{二进制路径}` 为你的小红书 MCP 二进制路径
   - 替换 `{工作路径}` 为你的小红书 MCP 工作路径（必须包含 cookies.json 文件）
   - 可选：修改日志路径 `StandardOutPath`
   - 可选：修改错误日志路径 `StandardErrorPath`
   - 可选：修改错误退出行为 `KeepAlive`
   - 可选：修改开机自动启动 `RunAtLoad`

2. **安装配置**
   ```bash
   # 创建软链接
   ln -s {你编辑后的 plist} ~/Library/LaunchAgents/xhsmcp.plist
   
   # 加载配置
   launchctl load ~/Library/LaunchAgents/xhsmcp.plist
   ```

#### 2. 服务管理

**启动服务**
```bash
launchctl start xhsmcp
```

**停止服务**
```bash
launchctl stop xhsmcp
```

**查看状态**
```bash
# 查看服务状态（有进程 ID 则为运行中）
launchctl list | grep xhsmcp

# 或使用 curl 检查服务
curl http://localhost:18060/health
```

#### 3. 高级管理（Fish Shell）

如果你使用 Fish Shell，可以安装 `deploy/macos/xhsmcp.fish` 脚本：

```fish
# 安装后可以使用
xhsmcp_status

# 输出示例：
# ✗ xhsmcp 未运行
# 是否启动服务? (yes/其他): yes
# ✓ 服务启动成功 (PID: 76061)
```

## 🪟 Windows 部署

详细的 Windows 安装指南请参考：[Windows 安装指南](./windows_guide.md)

## 🔧 通用部署选项

### 命令行参数

```bash
# 基本启动
./xiaohongshu-mcp

# 指定端口
./xiaohongshu-mcp -port :8080

# 无头模式（生产环境推荐）
./xiaohongshu-mcp -headless=true

# 指定浏览器路径
./xiaohongshu-mcp -bin /path/to/chrome
```

### 环境变量

```bash
# 浏览器路径
export ROD_BROWSER_BIN=/path/to/chrome

# Cookies 路径
export COOKIES_PATH=/path/to/cookies.json
```

## 📊 监控和日志

### 健康检查

```bash
# HTTP 健康检查
curl http://localhost:18060/health

# 或使用 MCP-Inspector 连接测试
```

### 日志管理

**Docker 环境**
```bash
# 查看实时日志
docker logs -f xiaohongshu-mcp

# 查看历史日志
docker logs xiaohongshu-mcp
```

**macOS 后台服务**
```bash
# 查看系统日志
tail -f /var/log/xhsmcp.log

# 查看错误日志
tail -f /var/log/xhsmcp-error.log
```

## 🚨 故障排查

### 常见问题

1. **服务无法启动**
   - 检查端口是否被占用
   - 确认二进制文件权限
   - 查看错误日志

2. **登录失败**
   - 确认 cookies.json 文件存在
   - 检查网络连接
   - 重新扫码登录

3. **图片上传失败**
   - 确认图片路径正确
   - 检查文件权限
   - 验证图片格式

### 调试模式

```bash
# 启用详细日志
./xiaohongshu-mcp -headless=false

# 查看浏览器操作过程
./xiaohongshu-mcp -headless=false -bin /path/to/chrome
```

## 🔄 更新和维护

### 更新服务

**Docker 环境**
```bash
docker compose pull
docker compose up -d
```

**macOS 后台服务**
```bash
# 停止服务
launchctl stop xhsmcp

# 替换二进制文件
cp new-xiaohongshu-mcp /path/to/binary

# 重启服务
launchctl start xhsmcp
```

### 备份重要数据

- `cookies.json` - 登录状态
- `images/` - 图片文件
- 配置文件

---

**提示**：生产环境建议使用 Docker 部署，开发环境可以使用直接运行方式。
