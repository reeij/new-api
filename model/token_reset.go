package model

import (
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/bytedance/gopkg/util/gopool"
	"gorm.io/gorm"
)

// 令牌额度重置周期
const (
	TokenResetNever   = "never"
	TokenResetDaily   = "daily"
	TokenResetWeekly  = "weekly"
	TokenResetMonthly = "monthly"
	TokenResetCustom  = "custom"
)

// 自定义重置周期的对齐基准日期（本地时区):1970-01-01 00:00
var tokenResetReferenceDate = time.Date(1970, 1, 1, 0, 0, 0, 0, time.Local)

// NormalizeTokenResetPeriod 将重置周期归一化为受支持的取值，非法值回退为 never。
func NormalizeTokenResetPeriod(period string) string {
	switch strings.TrimSpace(period) {
	case TokenResetDaily, TokenResetWeekly, TokenResetMonthly, TokenResetCustom:
		return strings.TrimSpace(period)
	default:
		return TokenResetNever
	}
}

// civilDays 返回给定日期（不含时区）相对 1970-01-01 的天数，用于按日历对齐。
func civilDays(y int, m time.Month, d int) int64 {
	if m <= 2 {
		y--
	}
	era := y / 400
	yoe := y - era*400
	var mp int
	if m > 2 {
		mp = int(m) - 3
	} else {
		mp = int(m) + 9
	}
	doy := (153*mp+2)/5 + d - 1
	doe := yoe*365 + yoe/4 - yoe/100 + doy
	return int64(era*146097 + doe - 719468)
}

// CalcTokenNextResetTime 计算 now 之后的下一个重置时刻（严格大于 now，且为 00:00）。
// 返回 0 表示该周期不需要重置（never 或 custom 天数非法）。
func CalcTokenNextResetTime(now time.Time, period string, customDays int) int64 {
	loc := now.Location()
	switch NormalizeTokenResetPeriod(period) {
	case TokenResetDaily:
		next := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc).AddDate(0, 0, 1)
		return next.Unix()
	case TokenResetWeekly:
		// 对齐到下一个周一 00:00（全局对齐）
		weekday := int(now.Weekday()) // Sunday=0
		if weekday == 0 {
			weekday = 7
		}
		daysUntil := 8 - weekday
		next := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc).AddDate(0, 0, daysUntil)
		return next.Unix()
	case TokenResetMonthly:
		// 对齐到下一个月的 1 号 00:00（全局对齐）
		next := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, loc).AddDate(0, 1, 0)
		return next.Unix()
	case TokenResetCustom:
		if customDays <= 0 {
			return 0
		}
		today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc)
		// 全局对齐：以基准日期起算，每 customDays 天为一次重置
		refDays := civilDays(tokenResetReferenceDate.Year(), tokenResetReferenceDate.Month(), tokenResetReferenceDate.Day())
		dayNum := civilDays(today.Year(), today.Month(), today.Day()) - refDays
		remainder := dayNum % int64(customDays)
		if remainder < 0 {
			remainder += int64(customDays)
		}
		daysUntil := (int64(customDays) - remainder) % int64(customDays)
		if daysUntil == 0 {
			daysUntil = int64(customDays)
		}
		next := today.AddDate(0, 0, int(daysUntil))
		return next.Unix()
	default:
		return 0
	}
}

// ApplyQuotaResetConfig 根据令牌当前配置计算重置相关字段。
// 当令牌为无限额度或周期为 never 时清空重置配置；否则记录本周期额度与下次重置时刻。
// 调用方需保证 RemainQuota / UnlimitedQuota / QuotaResetPeriod / QuotaResetCustomDays 已就绪。
func (token *Token) ApplyQuotaResetConfig(now int64) {
	period := NormalizeTokenResetPeriod(token.QuotaResetPeriod)
	if token.UnlimitedQuota || period == TokenResetNever {
		token.QuotaResetPeriod = TokenResetNever
		token.QuotaResetCustomDays = 0
		token.QuotaResetQuota = 0
		token.LastResetTime = 0
		token.NextResetTime = 0
		return
	}
	if period == TokenResetCustom && token.QuotaResetCustomDays <= 0 {
		token.QuotaResetCustomDays = 1
	}
	token.QuotaResetPeriod = period
	token.QuotaResetQuota = token.RemainQuota
	token.LastResetTime = now
	token.NextResetTime = CalcTokenNextResetTime(time.Unix(now, 0), period, token.QuotaResetCustomDays)
}

// ResetDueTokens 重置已到重置时刻的令牌额度：恢复 remain_quota 为周期额度，清零 used_quota。
// 不会修改令牌状态。返回重置的令牌数量。
func ResetDueTokens(limit int) (int, error) {
	if limit <= 0 {
		limit = 200
	}
	now := GetDBTimestamp()
	var tokens []Token
	if err := DB.Where("next_reset_time > 0 AND next_reset_time <= ?", now).
		Order("next_reset_time asc").
		Limit(limit).
		Find(&tokens).Error; err != nil {
		return 0, err
	}
	if len(tokens) == 0 {
		return 0, nil
	}
	resetCount := 0
	for _, token := range tokens {
		if token.UnlimitedQuota {
			continue
		}
		tokenCopy := token
		nextReset := CalcTokenNextResetTime(time.Unix(now, 0), tokenCopy.QuotaResetPeriod, tokenCopy.QuotaResetCustomDays)
		err := DB.Transaction(func(tx *gorm.DB) error {
			var locked Token
			if err := tx.Set("gorm:query_option", "FOR UPDATE").
				Where("id = ? AND next_reset_time = ?", tokenCopy.Id, tokenCopy.NextResetTime).
				First(&locked).Error; err != nil {
				return nil
			}
			if err := tx.Model(&Token{}).Where("id = ?", tokenCopy.Id).Updates(map[string]interface{}{
				"remain_quota":    tokenCopy.QuotaResetQuota,
				"used_quota":      0,
				"last_reset_time": now,
				"next_reset_time": nextReset,
			}).Error; err != nil {
				return err
			}
			resetCount++
			return nil
		})
		if err != nil {
			return resetCount, err
		}
		if common.RedisEnabled && tokenCopy.Key != "" {
			key := tokenCopy.Key
			gopool.Go(func() {
				if err := cacheDeleteToken(key); err != nil {
					common.SysLog("failed to invalidate token cache after quota reset: " + err.Error())
				}
			})
		}
	}
	return resetCount, nil
}
