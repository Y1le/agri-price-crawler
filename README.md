# 🌾 agri-price-crawler — 智能农产品价格监控与膳食推荐系统

[![Go Version](https://img.shields.io/badge/Go-1.22+-00ADD8?logo=go)](https://golang.org)
[![License](https://img.shields.io/badge/License-MIT-green.svg)](LICENSE)
[![Redis](https://img.shields.io/badge/Redis-7.x-red?logo=redis)](https://redis.io)
[![MySQL](https://img.shields.io/badge/MySQL-8.x-blue?logo=mysql)](https://mysql.com)

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
| 后端服务 | Go 1.24.3+ Gin + GORM |
| 定时任务 | Cron + 自定义调度器 |
| 数据存储 | MySQL + Redis |
| 反爬引擎 | 动态 Header 签名（SHA384 + Base36 TraceID） |
| AI 推荐 | 规则引擎 + 营养搭配模型（可扩展 LLM） |
---

## 🚀 快速开始
