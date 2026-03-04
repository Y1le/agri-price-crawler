package notifications

import (
	"fmt"
	"strings"
	"time"

	"github.com/Y1le/agri-price-crawler/internal/pkg/alert/config"
	"github.com/Y1le/agri-price-crawler/pkg/log"
	gomail "gopkg.in/gomail.v2"
)

// EmailNotification 邮件通知器
type EmailNotification struct {
	config *config.EmailConfig
}

// NewEmailNotification 创建新的邮件通知器
func NewEmailNotification(cfg *config.EmailConfig) *EmailNotification {
	return &EmailNotification{
		config: cfg,
	}
}

// AlertType 告警类型定义
type AlertType string

const (
	AlertTypePriceSpike   AlertType = "price_spike"    // 价格暴涨
	AlertTypePriceDrop    AlertType = "price_drop"     // 价格暴跌
	AlertTypeHighVolatility AlertType = "high_volatility" // 高波动
	AlertTypeMissingData  AlertType = "missing_data"   // 数据缺失
	AlertTypeCrawlerDown  AlertType = "crawler_down"   // 爬虫异常
)

// AlertSeverity 告警级别
type AlertSeverity string

const (
	SeverityInfo    AlertSeverity = "info"
	SeverityWarning AlertSeverity = "warning"
	SeverityCritical AlertSeverity = "critical"
)

// AlertContent 告警内容
type AlertContent struct {
	Type       AlertType
	Severity   AlertSeverity
	Product    string // 品种名称
	Price      float64
	ChangePct  float64
	ChangeAbs  float64
	Region     string // 地区
	Category   string // 分类
	Timestamp  time.Time
	Extra      map[string]string // 额外信息
}

// SendAlert 发送告警邮件
func (e *EmailNotification) SendAlert(content *AlertContent) error {
	// 构建邮件内容
	subject, body := e.buildEmailContent(content)

	// 防止邮件风暴：如果收件人列表为空，不发送
	if len(e.config.Recipients) == 0 {
		log.Warnf("No recipients configured, skipping email alert")
		return nil
	}

	// 发送邮件
	return e.sendMail(subject, body)
}

// buildEmailContent 构建邮件标题和正文
func (e *EmailNotification) buildEmailContent(content *AlertContent) (string, string) {
	//Format timestamp
	ts := content.Timestamp.Format("2006-01-02 15:04:05")

	// 构建正文
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("_subject_?\n\n"))
	sb.WriteString(fmt.Sprintf("[!] 告警类型: %s\n", e.formatAlertType(content.Type)))
	sb.WriteString(fmt.Sprintf("[!] 告警级别: %s\n", e.formatSeverity(content.Severity)))
	sb.WriteString(fmt.Sprintf("[!] 品种: %s\n", content.Product))
	sb.WriteString(fmt.Sprintf("[!] 当前价格: %.2f 元/公斤\n", content.Price))

	// 价格变化
	if content.ChangePct != 0 {
	符号 := "+"
		if content.ChangePct < 0 {
			符号 = ""
		}
		sb.WriteString(fmt.Sprintf("[!] 价格变化: %s%.2f%% (%s%.2f元)\n",
			符号, content.ChangePct, 符号, content.ChangeAbs))
	}

	sb.WriteString(fmt.Sprintf("[!] 地区: %s\n", content.Region))
	sb.WriteString(fmt.Sprintf("[!] 分类: %s\n", content.Category))
	sb.WriteString(fmt.Sprintf("[!] 时间: %s\n", ts))

	// 额外信息
	if len(content.Extra) > 0 {
		sb.WriteString("\n[!] 详细信息:\n")
		for k, v := range content.Extra {
			sb.WriteString(fmt.Sprintf("    %s: %s\n", k, v))
		}
	}

	sb.WriteString("\n----------------------------------------\n")
	sb.WriteString("此邮件由 agri-price-crawler 系统自动发送\n")
	sb.WriteString("请勿直接回复\n")

	return e.formatSubject(content.Type, content.Severity, content.Product), sb.String()
}

// formatSubject 格式化邮件标题
func (e *EmailNotification) formatSubject(alertType AlertType, severity AlertSeverity, product string) string {
	prefix := "[农产品价格]"
	switch severity {
	case SeverityCritical:
		prefix = "[_CRITICAL_] " + prefix
	case SeverityWarning:
		prefix = "[WARNING] " + prefix
	default:
		prefix = "[INFO] " + prefix
	}

	alertDesc := ""
	switch alertType {
	case AlertTypePriceSpike:
		alertDesc = "暴涨"
	case AlertTypePriceDrop:
		alertDesc = "暴跌"
	case AlertTypeHighVolatility:
		alertDesc = "高波动"
	case AlertTypeMissingData:
		alertDesc = "数据缺失"
	case AlertTypeCrawlerDown:
		alertDesc = "系统异常"
	}

	return fmt.Sprintf("%s %s: %s", prefix, alertDesc, product)
}

// formatAlertType 格式化告警类型显示
func (e *EmailNotification) formatAlertType(alertType AlertType) string {
	switch alertType {
	case AlertTypePriceSpike:
		return "价格暴涨"
	case AlertTypePriceDrop:
		return "价格暴跌"
	case AlertTypeHighVolatility:
		return "高波动风险"
	case AlertTypeMissingData:
		return "数据缺失"
	case AlertTypeCrawlerDown:
		return "爬虫异常"
	default:
		return string(alertType)
	}
}

// formatSeverity 格式化告警级别显示
func (e *EmailNotification) formatSeverity(severity AlertSeverity) string {
	switch severity {
	case SeverityCritical:
		return "严重"
	case SeverityWarning:
		return "警告"
	default:
		return "提示"
	}
}

// sendMail 发送邮件
func (e *EmailNotification) sendMail(subject, body string) error {
	// 创建邮件
	m := gomail.NewMessage()
	m.SetHeader("From", e.config.From)
	m.SetHeader("To", e.config.Recipients...)
	m.SetHeader("Subject", subject)
	m.SetBody("text/plain", body)

	// 如果内容过长，也发送 HTML 格式
	htmlBody := e.buildHTMLBody(body)
	m.AddAlternative("text/html", htmlBody)

	// 发送
	d := gomail.NewDialer(e.config.Host, e.config.Port, e.config.Username, e.config.Password)
	d.SSL = true

	if err := d.DialAndSend(m); err != nil {
		log.Errorf("Failed to send email alert: %v", err)
		return err
	}

	log.Infof("Email alert sent successfully to %d recipients", len(e.config.Recipients))
	return nil
}

// buildHTMLBody 构建 HTML 格式的邮件正文
func (e *EmailNotification) buildHTMLBody(textBody string) string {
	lines := strings.Split(textBody, "\n")

	var sb strings.Builder
	sb.WriteString("<!DOCTYPE html><html><head><style>")
	sb.WriteString("body{font-family:Arial,sans-serif;line-height:1.6;color:#333}")
	sb.WriteString(".alert-box{background:#f8f9fa;border-left:4px solid #dc3545;padding:15px;margin:15px 0}")
	sb.WriteString(".info{color:#007bff}")
	sb.WriteString(".warning{color:#ffc107}")
	sb.WriteString(".critical{color:#dc3547}")
	sb.WriteString("pre{background:#f4f4f4;padding:10px;border-radius:4px;overflow-x:auto}")
	sb.WriteString("</style></head><body>")

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			sb.WriteString("<br>")
			continue
		}

		// 处理告警类型
		if strings.HasPrefix(line, "[!] 告警类型:") {
			alertType := strings.TrimSpace(strings.TrimPrefix(line, "[!] 告警类型:"))
			sb.WriteString(fmt.Sprintf("<div class=\"alert-box\"><strong class=\"%s\">%s</strong></div>",
				e.getTypeColor(alertType), e.escapeHTML(alertType)))
		} else if strings.HasPrefix(line, "[!]") {
			sb.WriteString(fmt.Sprintf("<p>%s</p>", e.escapeHTML(line)))
		} else if strings.HasPrefix(line, "此邮件") {
			sb.WriteString(fmt.Sprintf("<hr><p style=\"color:#666;font-size:12px\">%s</p>", e.escapeHTML(line)))
		} else {
			sb.WriteString(fmt.Sprintf("<p>%s</p>", e.escapeHTML(line)))
		}
	}

	sb.WriteString("</body></html>")
	return sb.String()
}

// getTypeColor 获取告警类型的颜色
func (e *EmailNotification) getTypeColor(alertType string) string {
	switch alertType {
	case "价格暴涨", "高波动风险", "爬虫异常":
		return "critical"
	case "价格暴跌":
		return "warning"
	default:
		return "info"
	}
}

// escapeHTML 转义 HTML 特殊字符
func (e *EmailNotification) escapeHTML(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	s = strings.ReplaceAll(s, "\"", "&quot;")
	return s
}
