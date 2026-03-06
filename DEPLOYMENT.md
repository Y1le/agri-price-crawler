# 部署指南

## 环境准备

### 1. 服务器配置

为保证应用正常运行，服务器需要满足以下要求：

- CPU: 2核以上
- 内存: 4GB以上
- 磁盘: 20GB以上可用空间
- 系统: Linux (推荐 Ubuntu 20.04+ 或 CentOS 7+)

### 2. 软件依赖

- Docker 20.10+
- Docker Compose v2+
- Git

## 部署方式

### 方式一：Docker Compose 部署

1. **克隆代码**

```bash
git clone https://github.com/Y1le/agri-price-crawler.git
cd agri-price-crawler
```

2. **配置环境变量**

```bash
cp .env.example .env
# 编辑 .env 文件，填入敏感配置信息
vim .env
```

3. **准备证书文件**

```bash
# 创建 HTTPS 证书 (如已有证书可跳过)
openssl req -x509 -newkey rsa:4096 -keyout craw-key.pem -out craw.pem -days 365 -nodes -subj "/CN=localhost"
```

4. **启动服务**

```bash
# 使用 docker-compose 启动所有服务
docker-compose up -d
```

### 方式二：Kubernetes 部署

Coming soon...

### 方式三：二进制部署

1. **下载预编译二进制文件**

从 GitHub Releases 下载对应平台的二进制文件：

```bash
# 示例 (请替换为最新版本)
wget https://github.com/Y1le/agri-price-crawler/releases/download/v1.0.0/agri-price-crawler-linux-amd64
chmod +x agri-price-crawler-linux-amd64
```

2. **配置系统服务**

创建 systemd 服务文件：

```bash
sudo tee /etc/systemd/system/agri-price-crawler.service << EOF
[Unit]
Description=Agri Price Crawler Service
After=network.target

[Service]
Type=simple
User=agri-price-crawler
Group=agri-price-crawler
WorkingDirectory=/opt/agri-price-crawler
ExecStart=/opt/agri-price-crawler/crawserver
Restart=always
RestartSec=5
EnvironmentFile=/opt/agri-price-crawler/.env

[Install]
WantedBy=multi-user.target
EOF
```

3. **启动服务**

```bash
# 重载 systemd 配置
sudo systemctl daemon-reload

# 启动服务
sudo systemctl start agri-price-crawler

# 设置开机自启
sudo systemctl enable agri-price-crawler
```

## 环境变量配置详解

以下是所有支持的环境变量配置：

### 数据库配置
- `CRAW_MYSQL_HOST`: MySQL 主机地址和端口 (默认: 127.0.0.1:3306)
- `CRAW_MYSQL_USERNAME`: MySQL 用户名
- `CRAW_MYSQL_PASSWORD`: MySQL 密码
- `CRAW_MYSQL_DATABASE`: 数据库名称

### Redis 配置
- `CRAW_REDIS_HOST`: Redis 主机地址 (默认: 127.0.0.1)
- `CRAW_REDIS_PORT`: Redis 端口 (默认: 6379)
- `CRAW_REDIS_PASSWORD`: Redis 密码

### JWT 配置
- `CRAW_JWT_KEY`: JWT 密钥 (重要：需要足够复杂且保密)
- `CRAW_JWT_TIMEOUT`: Token 超时时间 (默认: 24h)

### 邮件配置
- `CRAW_EMAIL_HOST`: SMTP 服务器地址 (默认: smtp.qq.com)
- `CRAW_EMAIL_PORT`: SMTP 端口 (默认: 465)
- `CRAW_EMAIL_USERNAME`: SMTP 用户名
- `CRAW_EMAIL_PASSWORD`: SMTP 密码 (通常是应用密码)
- `CRAW_EMAIL_FROM`: 发件人地址

### 爬虫配置
- `CRAW_CRAWLER_DEVICE_ID`: 惠农网设备ID
- `CRAW_CRAWLER_SECRET`: 惠农网密钥

### AI 服务配置
- `CRAW_DOUBAO_API_KEY`: 豆包AI API密钥
- `CRAW_DOUBAO_MODEL`: AI模型名称

## 监控与日志

### 日志配置

应用支持多种日志输出方式：
- 控制台输出
- 文件输出
- 结构化日志 (JSON格式)

### 健康检查

服务提供健康检查接口：
- HTTP端点: `GET /health`
- 返回状态: 200 OK 表示服务正常

### 性能监控

- Prometheus 指标: `GET /metrics`
- pprof 性能分析: `GET /debug/pprof/`

## 安全注意事项

1. **证书安全**
   - 生产环境必须使用有效的 SSL 证书
   - 定期更新证书，避免过期

2. **访问控制**
   - 配置防火墙，限制不必要的端口暴露
   - 使用强密码策略
   - 定期轮换敏感密钥

3. **数据安全**
   - 定期备份数据库
   - 加密传输敏感数据
   - 遵循最小权限原则

## 故障排查

### 常见问题

1. **服务启动失败**
   - 检查数据库连接是否正常
   - 检查 Redis 连接是否正常
   - 查看日志输出

2. **定时任务未执行**
   - 检查 Cron 配置
   - 确认网络连接正常

3. **爬虫功能异常**
   - 检查设备ID和密钥是否正确
   - 确认目标网站是否更改接口

### 日志定位

```bash
# Docker 部署日志查看
docker logs agri-price-crawler

# 二进制部署日志查看
journalctl -u agri-price-crawler -f

# 应用日志文件位置 (根据配置可能不同)
tail -f /var/log/agri-price-crawler/app.log
```

## 版本升级

### 自动升级 (通过 CI/CD)

当推送到 main 分支时，会自动执行生产环境部署。

### 手动升级

1. **停止当前服务**
   ```bash
   docker-compose down
   ```

2. **拉取最新代码**
   ```bash
   git pull origin main
   ```

3. **重新构建并启动**
   ```bash
   docker-compose up -d --build
   ```

## 回滚策略

如遇严重问题需要回滚：

1. **确定回滚版本**
   ```bash
   git log --oneline
   git checkout <previous_commit_hash>
   ```

2. **重建服务**
   ```bash
   docker-compose down
   docker-compose up -d
   ```

## 性能优化建议

1. **数据库优化**
   - 定期清理历史数据
   - 合理配置连接池大小
   - 启用慢查询日志

2. **Redis 优化**
   - 启用持久化策略
   - 监控内存使用情况
   - 设置合理的过期时间

3. **应用层优化**
   - 调整并发参数
   - 优化爬虫频率
   - 合理配置缓存策略