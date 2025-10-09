package api

import (
	"time"

	"go.uber.org/zap"
)

const LogBodyLimit = 4096 // 4  TODO could be a customized via envVar

func LogSafe(b []byte) []byte {
	if len(b) > LogBodyLimit {
		return append(b[:LogBodyLimit], []byte("... [truncated]")...)
	}
	return b
}

func LogRequest(logger *zap.Logger, tag string, method, path string, body []byte) time.Time {
	logger.Info(tag+"_request",
		zap.String("method", method),
		zap.String("path", path),
		zap.ByteString("body", LogSafe(body)),
	)
	return time.Now()
}

func LogResponse(logger *zap.Logger, tag string, status int, body []byte, started time.Time) {
	logger.Info(tag+"_response",
		zap.Int("status", status),
		zap.Int64("latency_ms", time.Since(started).Milliseconds()),
		zap.ByteString("body", LogSafe(body)),
	)
}
