package service

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"

	"github.com/bytedance/gopkg/util/gopool"
)

const (
	tokenResetTickInterval = 1 * time.Minute
	tokenResetBatchSize    = 300
)

var (
	tokenResetOnce    sync.Once
	tokenResetRunning atomic.Bool
)

// StartTokenQuotaResetTask 启动令牌额度重置后台任务（每天/每周/每月/自定义天数）。
func StartTokenQuotaResetTask() {
	tokenResetOnce.Do(func() {
		if !common.IsMasterNode {
			return
		}
		gopool.Go(func() {
			logger.LogInfo(context.Background(), fmt.Sprintf("token quota reset task started: tick=%s", tokenResetTickInterval))
			ticker := time.NewTicker(tokenResetTickInterval)
			defer ticker.Stop()

			runTokenQuotaResetOnce()
			for range ticker.C {
				runTokenQuotaResetOnce()
			}
		})
	})
}

func runTokenQuotaResetOnce() {
	if !tokenResetRunning.CompareAndSwap(false, true) {
		return
	}
	defer tokenResetRunning.Store(false)

	ctx := context.Background()
	totalReset := 0
	for {
		n, err := model.ResetDueTokens(tokenResetBatchSize)
		if err != nil {
			logger.LogWarn(ctx, fmt.Sprintf("token quota reset task failed: %v", err))
			return
		}
		if n == 0 {
			break
		}
		totalReset += n
		if n < tokenResetBatchSize {
			break
		}
	}
	if common.DebugEnabled && totalReset > 0 {
		logger.LogDebug(ctx, "token quota reset: reset_count=%d", totalReset)
	}
}
