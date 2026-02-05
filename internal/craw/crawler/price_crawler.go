package crawler

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/Y1le/agri-price-crawler/internal/craw/store"
	v1 "github.com/Y1le/agri-price-crawler/pkg/api/v1"
	"github.com/Y1le/agri-price-crawler/pkg/log"
	utils "github.com/Y1le/agri-price-crawler/pkg/utils"
	"github.com/google/uuid"
)

const (
	URL = "https://pcapi.cnhnb.com/market-platform/api/market/statistics/pc/arealist"
)

type CrawlerConfig struct {
	DeviceID string
	Secret   string
}

// PriceCrawler 封装爬虫逻辑
type PriceCrawler struct {
	config CrawlerConfig
	client *http.Client
}

func NewPriceCrawler(cfg CrawlerConfig, store store.Factory) *PriceCrawler {
	return &PriceCrawler{
		config: cfg,
		client: &http.Client{Timeout: 15 * time.Minute},
	}
}

type APIResponse struct {
	Code int `json:"code"`
	Data struct {
		List       []PriceItem `json:"list"`
		Total      int         `json:"total"`
		TotalPages int         `json:"totalPages"`
		PageNum    int         `json:"pageNum"`
		PageSize   int         `json:"pageSize"`
	} `json:"data"`
	Message string `json:"message"`
}

// Run 执行一次爬取（指定日期）
func (c *PriceCrawler) Run(ctx context.Context, date time.Time) error {
	log.Infof("Starting full price crawl for date: %s", date.Format("2006-01-02"))
	totalPages := 1
	total_records := 0
	for pageNum := 1; pageNum <= totalPages; pageNum++ {

		if pageNum%51 == 50 {
			time.Sleep(5 * time.Minute)
		} else {
			time.Sleep(100 * time.Millisecond)
		}
		resp, err := c.fetchPage(ctx, date, pageNum)
		if err != nil {
			log.Errorf("Failed to fetch page %d: %v", pageNum, err)
			continue
		}
		err = c.saveData(ctx, resp.Data.List)
		if err != nil {
			return fmt.Errorf("save all data failed: %w", err)
		}
		if pageNum == 1 {
			totalPages = resp.Data.Total / 15
			if resp.Data.Total%15 != 0 {
				totalPages++
			}
			total_records = resp.Data.Total
			log.Infof("Total pages to crawl: %d", totalPages)
		}
	}

	log.Infof("Full price crawl succeeded for %s, total records: %d",
		date.Format("2006-01-02"), total_records)
	return nil
}

func (c *PriceCrawler) saveData(ctx context.Context, items []PriceItem) error {
	if len(items) == 0 {
		return nil
	}

	prices, err := parsePriceItems(items)
	if err != nil {
		return fmt.Errorf("parse price items failed: %w", err)
	}
	log.Infof("Saving %d price records", len(prices))
	return store.Client().Prices().Save(ctx, prices)
}

func (c *PriceCrawler) generateNonce(ts int64) string {
	template := "xxxxxxxxxxxxxyxxxxyxxxxxxxxxxxxx"
	result := make([]byte, len(template))
	t := ts
	for i := 0; i < len(template); i++ {
		c := template[i]
		if c == 'x' || c == 'y' {
			randomVal := float64(uuid.New().ID()%1000000) / 1000000.0 // 模拟 Math.random()
			n := int((float64(t) + 16*randomVal)) % 16
			t = t / 16
			if c == 'x' {
				result[i] = []byte(strconv.FormatInt(int64(n), 16))[0]
			} else {
				y := (3 & n) | 8
				result[i] = []byte(strconv.FormatInt(int64(y), 16))[0]
			}
		} else {
			result[i] = c
		}
	}
	return string(result)
}
func (c *PriceCrawler) generateTraceID(ts int64) string {
	part1 := utils.Base36EncodeFixed(ts, 9)
	part2 := utils.Base36EncodeFixed(int64(time.Now().UnixNano()%78364164095), 7)
	return part1 + part2
}
func (c *PriceCrawler) generateSign(nonce, timestamp, deviceId, secret string) string {
	R := utils.Md5Encrypt(nonce)
	N := utils.Sha1Encrypt(timestamp)
	B := utils.Md5Encrypt(nonce + deviceId)

	// I = sha1(secret + timestamp)
	I_full := utils.Sha1Encrypt(secret + timestamp)

	// C = last 16 to last 2 → 14 chars
	var C_hex string
	if len(I_full) >= 16 {
		C_hex = I_full[len(I_full)-16 : len(I_full)-1]
	} else {
		C_hex = I_full
	}

	D_dec := utils.HexToDecimal(C_hex)
	V := R + "!" + N + "!" + B + "!" + D_dec
	return utils.Sha384Encrypt(V)
}

type PriceItem struct {
	FirstCateId       int64   `json:"firstCateId"`
	SecondCateId      int64   `json:"secondCateId"`
	CateId            int64   `json:"cateId"`
	CateName          string  `json:"cateName"`
	BreedId           int64   `json:"breedId"` // 注意是 breedId 不是 breedName
	BreedName         string  `json:"breedName"`
	MinPrice          float64 `json:"minPrice"`
	MaxPrice          float64 `json:"maxPrice"`
	AvgPrice          float64 `json:"avgPrice"`
	WeightingAvgPrice float64 `json:"weighting_avgPrice"`
	UpDownPrice       float64 `json:"upDownPrice"`
	Increase          float64 `json:"increase"`
	Unit              string  `json:"unit"`
	AddressDetail     string  `json:"addressDetail"`
	ProvinceId        int32   `json:"provinceId"`
	CityId            int32   `json:"cityId"`
	AreaId            int32   `json:"areaId"`
	CreateTime        int64   `json:"createTime"` // 时间戳（毫秒）
	StatisNum         int32   `json:"statisNum"`
	SourseType        string  `json:"sourse_type"` // 注意是 sourse_type（小写）
	Trend             int8    `json:"trend"`
}

func parsePriceItems(items []PriceItem) ([]*v1.Price, error) {
	var prices []*v1.Price

	for _, item := range items {

		// 创建 v1.Price 实例
		p := &v1.Price{
			FirstCateID:       uint64(item.FirstCateId),
			SecondCateID:      uint64(item.SecondCateId),
			CateID:            uint64(item.CateId),
			CateName:          item.CateName,
			BreedName:         item.BreedName,
			MinPrice:          item.MinPrice,
			MaxPrice:          item.MaxPrice,
			AvgPrice:          item.AvgPrice,
			WeightingAvgPrice: item.WeightingAvgPrice,
			UpDownPrice:       item.UpDownPrice,
			Increase:          item.Increase,
			Unit:              item.Unit,
			AddressDetail:     item.AddressDetail,
			ProvinceID:        uint32(item.ProvinceId),
			CityID:            uint32(item.CityId),
			AreaID:            uint32(item.AreaId),
			StatisNum:         uint32(item.StatisNum),
			SourceType:        item.SourseType,
			Trend:             item.Trend,
			TraceID:           "",
		}

		prices = append(prices, p)
	}

	return prices, nil
}
