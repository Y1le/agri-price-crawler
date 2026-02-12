package cron

import (
	"context"
	"fmt"

	mailer "github.com/Y1le/agri-price-crawler/internal/craw/emailer"
	"github.com/Y1le/agri-price-crawler/internal/craw/store"

	v1 "github.com/Y1le/agri-price-crawler/pkg/api/v1"
	metav1 "github.com/marmotedu/component-base/pkg/meta/v1"
)

type PriceSendTask interface {
	Run(ctx context.Context, targetDate string) error
}

func NewPriceSendTask(store store.Factory) PriceSendTask {
	return newPriceSendTaskImpl(store)
}

type PriceSendTaskImpl struct {
	emailer mailer.Mailer
	store   store.Factory
}

var _ PriceSendTask = (*PriceSendTaskImpl)(nil)

func newPriceSendTaskImpl(store store.Factory) *PriceSendTaskImpl {
	return &PriceSendTaskImpl{
		store: store,
	}
}

// func (s *SMTPMailer) SendBulkEmails(recipients []Recipient, subject, htmlBody string) error {

func (t *PriceSendTaskImpl) Run(ctx context.Context, targetDate string) error {
	// 从数据库中获取所有订阅者
	subscribes, err := t.store.Subscribes().List(context.Background(), metav1.ListOptions{})
	if err != nil {
		return fmt.Errorf("failed to get all subscribers: %w", err)
	}
	recipients := make([]mailer.Recipient, 0, len(subscribes))
	for _, subscribe := range subscribes {
		recipients = append(recipients, mailer.Recipient{
			Email: subscribe.Email,
			Name:  subscribe.Name,
		})
	}
	offset, limit := int64(0), int64(10)
	var r metav1.ListOptions
	r.Offset = &offset
	r.Limit = &limit
	r.LabelSelector = fmt.Sprintf("createdAt=%s", targetDate)
	prices, err := t.store.HNPrices().List(context.Background(), r)
	if err != nil {
		return fmt.Errorf("failed to get all prices: %w", err)
	}
	subject := "最新的农产品价格"
	htmlBody := fmt.Sprintf(`
		<html>
			<body>
				<h1>最新的农产品价格</h1>
				<p>%s</p>
			</body>
		</html>
	`, pricesToHtml(prices))

	// 发送邮件
	emailer := mailer.GetInstance()
	if err := emailer.SendBulkEmails(recipients, subject, htmlBody); err != nil {
		return fmt.Errorf("failed to send bulk emails: %w", err)
	}

	return nil
}

func pricesToHtml(prices *v1.PriceList) string {
	var html string
	for _, price := range prices.Items {
		html += fmt.Sprintf("<p>%s: %f</p>", price.BreedName, price.AvgPrice)
	}
	return html
}
