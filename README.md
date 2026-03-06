# 🌾 agri-price-crawler — 智能农产品价格监控与膳食推荐系统

[![Go Version](https://img.shields.io/badge/Go-1.24+-00ADD8?logo=go)](https://golang.org)
[![License](https://img.shields.io/badge/License-MIT-green.svg)](LICENSE)
[![Redis](https://img.shields.io/badge/Redis-7.x-red?logo=redis)](https://redis.io)
[![MySQL](https://img.shields.io/badge/MySQL-8.x-blue?logo=mysql)](https://mysql.com)
[![CI](https://github.com/Y1le/agri-price-crawler/actions/workflows/ci.yml/badge.svg)](https://github.com/Y1le/agri-price-crawler/actions/workflows/ci.yml)

> **让新鲜看得见，让餐桌更聪明**
> 实时爬取惠农网全国农产品价格，结合 AI 推荐每日健康食谱，助你吃得新鲜、吃得划算！

---

## ✨ 核心功能

- **实时价格监控**：每日自动抓取全国农贸市场和惠农网（cnhnb.com）的最新农产品价格
- **智能订阅推送**：用户可订阅所在城市，每日接收本地市场行情简报（支持邮件）
- **AI 膳食推荐**：基于当日低价优质食材，智能生成午餐 & 晚餐搭配建议
- **数据可视化**：提供 Web 界面查看历史价格趋势、区域比价
- **高可靠反爬**：完美复现惠农网前端签名算法，稳定绕过风控

---

## 📦 技术栈

| 模块 | 技术 |
|------|------|
| 后端服务 | Go 1.24+ Gin + GORM |
| 定时任务 | Cron + 自定义调度器 |
| 数据存储 | MySQL + Redis |
| 反爬引擎 | 动态 Header 签名（SHA384 + Base36 TraceID） |
| AI 推荐 | 规则引擎 + 营养搭配模型（可扩展 LLM） |

---

## 🖼️ 项目效果展示

1. 首页价格概览
首页直观展示全国农产品价格表：
<img src="./docs/images/price_table.png" alt="" width="600" />

2. 注册与登录
用户需先注册账号，才能订阅价格推送服务。注册完成后，可通过登录页进行身份验证。
<img src="./docs/images/register.png" alt="" width="400" />
<img src="./docs/images/login.png" alt="" width="400" />


3. 用户注册与订阅管理
用户可通过注册页完成账号创建，随后订阅城市、设置偏好食材,每日7点推送邮件：
<img src="./docs/images/subscribe.png" alt="" width="600" />

4. 邮件订阅成功提示
用户完成所在城市价格订阅后，系统会立即发送确认邮件，并在 Web 端展示成功页面：
<img src="./docs/images/email_sussess.png" alt="" width="600" />

---

## 🚀 快速开始

>⚠️ 本章节仍在完善中，部分步骤（如生产环境部署、CI/CD 配置）尚未完成；下方列出的本地开发环境启动流程为规划版本，部分环节暂未完全验证，仅供参考。如需稳定运行，建议结合实际环境调整。

### 1. 环境要求

- Go 1.24+
- MySQL 8.x
- Redis 7.x

### 2. 克隆与配置

```bash
git clone https://github.com/yourname/agri-price-crawler.git
cd agri-price-crawler

# 复制示例配置文件
cp .env.example .env
# 编辑 .env 文件，添加敏感信息
vim .env
```

创建 craw.pem 证书和 craw-key.pem 文件

```bash
openssl req -x509 -newkey rsa:4096 -keyout craw-key.pem -out craw.pem -days 365 -nodes -subj "/CN=localhost"
```

### 3. 启动依赖服务

```bash
# 启动 MySQL 和 Redis
docker-compose up -d mysql redis

# 查看服务状态
docker-compose ps
```

### 4. 初始化数据库

创建craw数据库

```bash
# 先登录 MySQL 终端（会提示输入密码）
mysql -u root -p

# 登录成功后，先创建数据库（如果已存在则跳过）
CREATE DATABASE IF NOT EXISTS craw;
```

导入 craw.sql 文件

方式1：直接在 MySQL 终端导入

```bash
mysql -u root -p

# 切换到目标数据库（必须！否则表会导入到默认数据库如 mysql 中）
USE craw;

# 导入 sql 文件（注意：这里的路径是服务器/本地的绝对路径/相对路径）
SOURCE /你的项目路径/configs/craw.sql;

# 验证是否导入成功（可选）
SHOW TABLES;  # 能看到 craw.sql 里的表名则说明导入成功
```

方式2： 通过管道直接导入（快捷方式）
在项目根目录下执行

```bash
mysql -u root -p craw < configs/craw.sql
```

### 5. 生成代码文件,构建项目

```bash
# 生成代码文件
make tools # 生成工具
make gen # 生成代码
# 构建项目
go  build ./cmd/craw-server/crawserver.go
```

### 6. 启动项目

```bash
# 启动项目
./crawserver
```

---

## 🔄 CI/CD 配置

本项目采用 GitHub Actions 实现完整的 CI/CD 流水线：

### 工作流配置

1. **CI 流水线** (`.github/workflows/ci.yml`):
   - 代码提交或 PR 创建时自动触发
   - 运行单元测试和覆盖率检查
   - 代码质量检查 (golangci-lint)
   - 构建 Docker 镜像

2. **开发环境部署** (`.github/workflows/deploy-dev.yml`):
   - 推送到 `develop` 分支时自动部署到开发环境
   - 支持 Docker 部署

3. **生产环境部署** (`.github/workflows/deploy-prod.yml`):
   - 推送到 `main/master` 分支时部署到生产环境
   - 包含安全扫描和健康检查
   - 使用 GitHub Environments 实现保护规则

4. **发布管理** (`.github/workflows/release.yml`):
   - 打标签时自动创建 GitHub Release
   - 生成多平台构建产物

### 环境变量配置

在 GitHub Secrets 中配置以下变量：

#### Docker Registry (可选)
- `DOCKER_USERNAME`: Docker Hub 用户名
- `DOCKER_PASSWORD`: Docker Hub 密码

#### 开发环境部署
- `DEV_SERVER_HOST`: 开发服务器主机地址
- `DEV_SERVER_USER`: 开发服务器用户名
- `DEV_SSH_KEY`: 开发服务器 SSH 密钥

#### 生产环境部署
- `PROD_SERVER_HOST`: 生产服务器主机地址
- `PROD_SERVER_USER`: 生产服务器用户名
- `PROD_SSH_KEY`: 生产服务器 SSH 密钥

#### 通知配置 (可选)
- `SLACK_WEBHOOK_URL`: Slack 通知 Webhook 地址

---

## 🔐 安全配置

### 环境变量管理

项目支持使用 `.env` 文件存储敏感信息，请确保：
1. 将 `.env` 添加到 `.gitignore` 中
2. 在部署环境中使用适当的密钥管理系统
3. 定期轮换敏感凭据

### 安全最佳实践

1. **HTTPS 传输**: 应用支持 HTTPS 通信
2. **JWT 认证**: 使用安全的 JWT 令牌机制
3. **SQL 注入防护**: 使用 ORM 的参数化查询
4. **XSS 防护**: 后端响应头安全设置
5. **输入验证**: 所有用户输入都经过验证