package config

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

const (
	defaultHuinongTimeout = 15 * time.Second
	maxHuinongTimeout     = 30 * time.Second
	minRawRetention       = 24 * time.Hour
	defaultRawRetention   = 30 * 24 * time.Hour
	maxRejectRatioBP      = 2_000
)

// Ingestion holds runtime configuration for the price-source worker.
// Secrets are intentionally never included in validation errors or logs.
type Ingestion struct {
	Huinong                   Huinong
	RejectRatioMaxBasisPoints uint16
	RawRetention              time.Duration
	ScheduleTimezone          string
}

type Huinong struct {
	Enabled  bool
	BaseURL  string
	Timeout  time.Duration
	DeviceID string
	Secret   string
}

func loadIngestion(environment string) (Ingestion, error) {
	enabled, err := boolEnv("INGESTION_HUINONG_ENABLED", false)
	if err != nil {
		return Ingestion{}, err
	}
	timeout, err := durationEnv("INGESTION_HUINONG_TIMEOUT", defaultHuinongTimeout)
	if err != nil {
		return Ingestion{}, err
	}
	if timeout <= 0 || timeout > maxHuinongTimeout {
		return Ingestion{}, fmt.Errorf("INGESTION_HUINONG_TIMEOUT must be positive and at most 30s")
	}
	rawRetention, err := durationEnv("INGESTION_RAW_RETENTION", defaultRawRetention)
	if err != nil {
		return Ingestion{}, err
	}
	if rawRetention < minRawRetention {
		return Ingestion{}, fmt.Errorf("INGESTION_RAW_RETENTION must be at least 24h")
	}
	rejectRatio, err := ratioBasisPointsEnv("INGESTION_REJECT_RATIO_MAX", "0.05")
	if err != nil {
		return Ingestion{}, err
	}

	huinong := Huinong{
		Enabled:  enabled,
		BaseURL:  stringEnv("INGESTION_HUINONG_BASE_URL", ""),
		Timeout:  timeout,
		DeviceID: stringEnv("INGESTION_HUINONG_DEVICE_ID", ""),
		Secret:   stringEnv("INGESTION_HUINONG_SECRET", ""),
	}
	if enabled {
		if huinong.BaseURL == "" {
			return Ingestion{}, fmt.Errorf("INGESTION_HUINONG_BASE_URL is required when INGESTION_HUINONG_ENABLED is true")
		}
		if err := validateHTTPBaseURL("INGESTION_HUINONG_BASE_URL", huinong.BaseURL, environment == "production"); err != nil {
			return Ingestion{}, err
		}
		if huinong.DeviceID == "" {
			return Ingestion{}, fmt.Errorf("INGESTION_HUINONG_DEVICE_ID is required when INGESTION_HUINONG_ENABLED is true")
		}
		if huinong.Secret == "" {
			return Ingestion{}, fmt.Errorf("INGESTION_HUINONG_SECRET is required when INGESTION_HUINONG_ENABLED is true")
		}
	}

	return Ingestion{
		Huinong:                   huinong,
		RejectRatioMaxBasisPoints: rejectRatio,
		RawRetention:              rawRetention,
		ScheduleTimezone:          "Asia/Shanghai",
	}, nil
}

// ratioBasisPointsEnv parses a non-negative decimal ratio with at most four
// fractional digits. Keeping the value in basis points makes later threshold
// comparisons exact without pulling source values through float64.
func ratioBasisPointsEnv(name, defaultValue string) (uint16, error) {
	raw := stringEnv(name, defaultValue)
	if raw == "" || strings.HasPrefix(raw, "+") || strings.HasPrefix(raw, "-") {
		return 0, fmt.Errorf("%s must be a decimal ratio between 0 and 0.20", name)
	}
	parts := strings.Split(raw, ".")
	if len(parts) > 2 || len(parts) == 0 || parts[0] == "" {
		return 0, fmt.Errorf("%s must be a decimal ratio between 0 and 0.20", name)
	}
	for _, part := range parts {
		if part == "" && len(parts) == 2 {
			continue
		}
		for _, r := range part {
			if r < '0' || r > '9' {
				return 0, fmt.Errorf("%s must be a decimal ratio between 0 and 0.20", name)
			}
		}
	}
	whole, err := strconv.ParseUint(parts[0], 10, 16)
	if err != nil {
		return 0, fmt.Errorf("%s must be a decimal ratio between 0 and 0.20", name)
	}
	fraction := ""
	if len(parts) == 2 {
		fraction = parts[1]
	}
	if len(fraction) > 4 {
		return 0, fmt.Errorf("%s must have at most four decimal places", name)
	}
	fraction += strings.Repeat("0", 4-len(fraction))
	fractionBP := uint64(0)
	if fraction != "" {
		fractionBP, err = strconv.ParseUint(fraction, 10, 16)
		if err != nil {
			return 0, fmt.Errorf("%s must be a decimal ratio between 0 and 0.20", name)
		}
	}
	basisPoints := whole*10_000 + fractionBP
	if basisPoints > maxRejectRatioBP {
		return 0, fmt.Errorf("%s must be between 0 and 0.20", name)
	}
	return uint16(basisPoints), nil
}
