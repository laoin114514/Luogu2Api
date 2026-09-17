package luoguclient

import (
	"errors"
	"math"
	"net"
	"time"
)

// defaultBackoff 默认指数退避: 1s, 2s, 4s, ...
func defaultBackoff(attempt int) time.Duration {
	return time.Duration(math.Pow(2, float64(attempt))) * time.Second
}

// shouldRetry 判断错误是否应该重试（网络超时、DNS 错误等临时错误）
func shouldRetry(err error) bool {
	if err == nil {
		return false
	}
	var netErr net.Error
	if errors.As(err, &netErr) {
		if netErr.Timeout() {
			return true
		}
	}
	var dnsErr *net.DNSError
	if errors.As(err, &dnsErr) {
		return true // DNS 错误通常是临时的，值得重试
	}
	return false
}

// shouldRetryStatus 判断 HTTP 状态码是否应该重试（仅 5xx）
func shouldRetryStatus(statusCode int) bool {
	return statusCode >= 500
}
