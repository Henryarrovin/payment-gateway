package data

import (
	"context"
	"errors"
	"fmt"
	"time"

	"payment-gateway/middleware"

	"go.uber.org/zap"
	"gorm.io/gorm/logger"
)

type GormLogger struct {
	logger               *zap.Logger
	SlowThreshold        time.Duration
	IgnoreRecordNotFound bool
}

func NewGormLogger(l *zap.Logger) *GormLogger {
	return &GormLogger{
		logger:               l,
		SlowThreshold:        200 * time.Millisecond,
		IgnoreRecordNotFound: true,
	}
}

func (g *GormLogger) LogMode(level logger.LogLevel) logger.Interface {
	return g
}

func (g *GormLogger) Info(ctx context.Context, msg string, args ...interface{}) {
	log := middleware.FromContext(ctx, g.logger)
	log.Info(fmt.Sprintf(msg, args...))
}

func (g *GormLogger) Warn(ctx context.Context, msg string, args ...interface{}) {
	log := middleware.FromContext(ctx, g.logger)
	log.Warn(fmt.Sprintf(msg, args...))
}

func (g *GormLogger) Error(ctx context.Context, msg string, args ...interface{}) {
	log := middleware.FromContext(ctx, g.logger)
	log.Error(fmt.Sprintf(msg, args...))
}

func (g *GormLogger) Trace(ctx context.Context, begin time.Time, fc func() (sql string, rowsAffected int64), err error) {
	log := middleware.FromContext(ctx, g.logger)
	elapsed := time.Since(begin)
	sql, rows := fc()

	fields := []zap.Field{
		zap.String("sql", sql),
		zap.Int64("rows", rows),
		zap.Duration("latency", elapsed),
	}

	switch {
	case err != nil && !(errors.Is(err, logger.ErrRecordNotFound) && g.IgnoreRecordNotFound):
		log.Error("gorm query error", append(fields, zap.Error(err))...)

	case elapsed > g.SlowThreshold:
		log.Warn("gorm slow query", fields...)

	default:
		log.Info("gorm query", fields...)
	}
}
