package crawler

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/Y1le/agri-price-crawler/pkg/log"
	utils "github.com/Y1le/agri-price-crawler/pkg/util"
)

// 仅供学习
func (c *PriceCrawler) fetchPage(ctx context.Context, date time.Time, pageNum int) (*APIResponse, error) {
	// 构造请求体
	reqBody := map[string]interface{}{
		"pageNum":     pageNum,
		"pageSize":    15,
		"marketType":  "area",
		"collectDate": date.Format("2006-01-02"),
		"hnUserId":    "",
	}

	bodyBytes, _ := json.Marshal(reqBody)

	ts := time.Now().UnixMilli()
	timeStr := strconv.FormatInt(ts, 10)
	nonce := c.generateNonce(ts)
	traceID := c.generateTraceID(ts)
	clientSid := "S_" + utils.Base36EncodeFixed(ts, 8)
	sign := c.generateSign(nonce, timeStr, c.config.DeviceID, c.config.Secret)
	if c.config.DeviceID == "" || c.config.Secret == "" {
		return nil, fmt.Errorf("deviceID or secret is empty")
	}
	// 创建请求
	req, err := http.NewRequestWithContext(ctx, "POST", URL, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("create request failed: %w", err)
	}

	// 设置 headers
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36")
	req.Header.Set("X-Client-Appid", "5")
	req.Header.Set("X-Client-Environment", "pro")
	req.Header.Set("X-Client-Page", "/hangqing/")
	req.Header.Set("X-Client-Time", timeStr)
	req.Header.Set("X-Client-Nonce", nonce)
	req.Header.Set("X-Client-Sign", sign)
	req.Header.Set("X-Client-Id", c.config.DeviceID)
	req.Header.Set("X-B3-Traceid", traceID)
	req.Header.Set("X-Client-Sid", clientSid)
	req.Header.Set("X-Hn-Job", "If you see these message, I hope you dont hack us, I hope you can join us! Please visit https://www.cnhnkj.com/job.html")

	log.Debugf("req.Header: %v", req.Header)
	// 发送请求
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("HTTP request failed: %w", err)
	}
	defer resp.Body.Close()
	// 解析响应
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response body failed: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("status %d: %s", resp.StatusCode, string(body))
	}

	// 解析 JSON
	var apiResp APIResponse
	if err := json.Unmarshal(body, &apiResp); err != nil {
		return nil, fmt.Errorf("unmarshal response failed: %w. Raw: %s", err, string(body))
	}

	return &apiResp, nil
}
