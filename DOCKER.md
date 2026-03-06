# Docker 部署指南

## 快速启动

### 1. 开发环境

```bash
# 启动开发环境（包含数据库、Redis和应用）
docker-compose -f docker-compose.yaml -f docker-compose.dev.yaml up -d

# 查看服务状态
docker-compose -f docker-compose.yaml -f docker-compose.dev.yaml ps

# 停止服务
docker-compose -f docker-compose.yaml -f docker-compose.dev.yaml down
```

### 2. 生产环境

```bash
# 构建并启动生产环境
docker-compose -f docker-compose.prod.yaml up -d --build

# 查看服务状态
docker-compose -f docker-compose.prod.yaml ps

# 停止服务
docker-compose -f docker-compose.prod.yaml down
```

## 配置说明

### 环境变量配置

创建 `.env` 文件来配置敏感信息：

```bash
# MySQL 配置
MYSQL_ROOT_PASSWORD=your_strong_password
MYSQL_USER=craw_user
MYSQL_PASSWORD=your_db_password
MYSQL_DATABASE=craw

# Redis 配置
REDIS_PASSWORD=your_redis_password

# 应用配置
JWT_KEY=your_very_strong_jwt_key_at_least_32_chars_long
APP_PORT=8080

# 爬虫配置
CRAWLER_DEVICE_ID=your_cnhnb_device_id
CRAWLER_SECRET=your_cnhnb_secret

# 邮件配置
EMAIL_HOST=smtp.your-email-provider.com
EMAIL_PORT=587
EMAIL_USERNAME=your@email.com
EMAIL_PASSWORD=your_email_app_password
EMAIL_FROM="Your Name <your@email.com>"

# AI API 配置
DOUBAO_API_KEY=your_doubao_api_key

# 日志级别
LOG_LEVEL=info
SERVER_MODE=release
```

### 证书配置

对于 HTTPS 支持，需要提供证书文件：

```bash
# 生成自签名证书（仅用于测试）
openssl req -x509 -newkey rsa:4096 -keyout craw-key.pem -out craw.pem -days 365 -nodes -subj "/CN=localhost"

# 将证书文件放置在项目根目录
ls -la *.pem *.crt
```

## 安全特性

1. **最小化基础镜像**：使用 Alpine Linux 减少攻击面
2. **非 root 用户**：应用程序以非 root 用户身份运行
3. **只读文件系统**：配置文件挂载为只读
4. **健康检查**：内置健康检查机制
5. **资源限制**：限制容器资源使用防止 DOS 攻击

## 构建优化

1. **多阶段构建**：分离构建环境和运行环境
2. **Docker 层缓存**：优化 go.mod 下载以利用缓存
3. **静态链接**：构建静态二进制文件，无需外部依赖
4. **符号表剥离**：减小二进制文件大小

## 日志和监控

1. **结构化日志**：支持 JSON 格式日志输出
2. **健康检查端点**：`/health` 端点用于容器健康检查
3. **指标收集**：通过 `/metrics` 端点提供 Prometheus 指标

## 环境特定配置

- **开发环境** (`docker-compose.dev.yaml`)：调试模式，端口映射，自动重启禁用
- **生产环境** (`docker-compose.prod.yaml`)：发布模式，资源限制，自动重启策略

## 故障排除

### 常见问题

1. **数据库连接失败**：
   - 检查 MySQL/Redis 服务是否已启动
   - 验证网络连接性
   - 确认环境变量配置正确

2. **权限错误**：
   - 确保证书文件权限正确
   - 检查卷挂载权限

3. **内存不足**：
   - 调整 docker-compose 文件中的资源限制

### 调试命令

```bash
# 查看容器日志
docker-compose logs app

# 进入容器调试
docker-compose exec app sh

# 检查网络连接
docker-compose exec app ping mysql
docker-compose exec app ping redis
```