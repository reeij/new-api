package model

import (
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCalcTokenNextResetTime(t *testing.T) {
	// 2026-03-04 10:30:00 本地时间（周三）
	now := time.Date(2026, 3, 4, 10, 30, 0, 0, time.Local)

	cases := []struct {
		name       string
		period     string
		customDays int
		want       int64
	}{
		{"never", TokenResetNever, 0, 0},
		{"daily", TokenResetDaily, 0, time.Date(2026, 3, 5, 0, 0, 0, 0, time.Local).Unix()},
		{"weekly", TokenResetWeekly, 0, time.Date(2026, 3, 9, 0, 0, 0, 0, time.Local).Unix()},   // 下周一
		{"monthly", TokenResetMonthly, 0, time.Date(2026, 4, 1, 0, 0, 0, 0, time.Local).Unix()}, // 下月 1 号
		{"custom 1 day", TokenResetCustom, 1, time.Date(2026, 3, 5, 0, 0, 0, 0, time.Local).Unix()},
		{"custom 7 days aligned", TokenResetCustom, 7, time.Date(2026, 3, 5, 0, 0, 0, 0, time.Local).Unix()},
		{"custom invalid", TokenResetCustom, 0, 0},
		{"unknown period", "whatever", 0, 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := CalcTokenNextResetTime(now, c.period, c.customDays)
			assert.Equal(t, c.want, got)
			if got > 0 {
				assert.Greater(t, got, now.Unix(), "next reset must be in the future")
				resetAt := time.Unix(got, 0)
				assert.Equal(t, 0, resetAt.Hour())
				assert.Equal(t, 0, resetAt.Minute())
			}
		})
	}
}

func TestApplyQuotaResetConfig(t *testing.T) {
	now := time.Now().Unix()

	t.Run("enabled daily", func(t *testing.T) {
		token := &Token{RemainQuota: 200, QuotaResetPeriod: TokenResetDaily}
		token.ApplyQuotaResetConfig(now)
		assert.Equal(t, TokenResetDaily, token.QuotaResetPeriod)
		assert.Equal(t, 200, token.QuotaResetQuota)
		assert.Equal(t, now, token.LastResetTime)
		assert.Greater(t, token.NextResetTime, now)
	})

	t.Run("custom clamps days", func(t *testing.T) {
		token := &Token{RemainQuota: 500, QuotaResetPeriod: TokenResetCustom, QuotaResetCustomDays: 0}
		token.ApplyQuotaResetConfig(now)
		assert.Equal(t, 1, token.QuotaResetCustomDays)
		assert.Equal(t, 500, token.QuotaResetQuota)
	})

	t.Run("unlimited clears", func(t *testing.T) {
		token := &Token{RemainQuota: 200, UnlimitedQuota: true, QuotaResetPeriod: TokenResetDaily, NextResetTime: 123}
		token.ApplyQuotaResetConfig(now)
		assert.Equal(t, TokenResetNever, token.QuotaResetPeriod)
		assert.Zero(t, token.QuotaResetQuota)
		assert.Zero(t, token.NextResetTime)
		assert.Zero(t, token.LastResetTime)
	})

	t.Run("never clears", func(t *testing.T) {
		token := &Token{RemainQuota: 200, QuotaResetPeriod: TokenResetNever, NextResetTime: 123}
		token.ApplyQuotaResetConfig(now)
		assert.Zero(t, token.QuotaResetQuota)
		assert.Zero(t, token.NextResetTime)
	})
}

func TestResetDueTokens(t *testing.T) {
	require.NotNil(t, DB)
	t.Cleanup(func() {
		DB.Exec("DELETE FROM tokens")
	})

	now := GetDBTimestamp()

	due := Token{
		UserId:           1,
		Key:              "reset-due-key",
		Name:             "due",
		Status:           common.TokenStatusEnabled,
		CreatedTime:      now - 100000,
		RemainQuota:      50,
		UsedQuota:        150,
		QuotaResetPeriod: TokenResetDaily,
		QuotaResetQuota:  200,
		LastResetTime:    now - 86400,
		NextResetTime:    now - 10,
	}
	notDue := Token{
		UserId:           1,
		Key:              "reset-notdue-key",
		Name:             "notdue",
		Status:           common.TokenStatusEnabled,
		CreatedTime:      now,
		RemainQuota:      30,
		UsedQuota:        70,
		QuotaResetPeriod: TokenResetDaily,
		QuotaResetQuota:  100,
		NextResetTime:    now + 86400,
	}
	unlimited := Token{
		UserId:           1,
		Key:              "reset-unlimited-key",
		Name:             "unlimited",
		Status:           common.TokenStatusEnabled,
		CreatedTime:      now,
		RemainQuota:      0,
		UnlimitedQuota:   true,
		QuotaResetPeriod: TokenResetDaily,
		QuotaResetQuota:  0,
		NextResetTime:    now - 10,
	}
	require.NoError(t, DB.Create(&due).Error)
	require.NoError(t, DB.Create(&notDue).Error)
	require.NoError(t, DB.Create(&unlimited).Error)

	n, err := ResetDueTokens(100)
	require.NoError(t, err)
	assert.Equal(t, 1, n, "only the due, non-unlimited token should be reset")

	var got Token
	require.NoError(t, DB.Where("id = ?", due.Id).First(&got).Error)
	assert.Equal(t, 200, got.RemainQuota, "remain quota restored to per-period quota")
	assert.Equal(t, 0, got.UsedQuota, "used quota cleared")
	assert.Greater(t, got.NextResetTime, now, "next reset advanced")
	assert.Equal(t, now, got.LastResetTime)

	var untouched Token
	require.NoError(t, DB.Where("id = ?", notDue.Id).First(&untouched).Error)
	assert.Equal(t, 30, untouched.RemainQuota)
	assert.Equal(t, 70, untouched.UsedQuota)

	var unlimitedGot Token
	require.NoError(t, DB.Where("id = ?", unlimited.Id).First(&unlimitedGot).Error)
	assert.Equal(t, now-10, unlimitedGot.NextResetTime, "unlimited token untouched")

	// 幂等：立即再次执行不应重复重置
	n2, err := ResetDueTokens(100)
	require.NoError(t, err)
	assert.Equal(t, 0, n2)
}
