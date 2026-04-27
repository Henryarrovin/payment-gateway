package data

import (
	"context"
	"errors"
	"time"

	"go.uber.org/zap"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

type gormZapLogger struct {
	logger *zap.Logger
}

func NewGormLogger(logger *zap.Logger) gormlogger.Interface {
	return &gormZapLogger{logger: logger}
}

func (l *gormZapLogger) LogMode(level gormlogger.LogLevel) gormlogger.Interface {
	return l
}

func (l *gormZapLogger) Info(ctx context.Context, msg string, args ...interface{}) {
	l.logger.Sugar().Infof(msg, args...)
}

func (l *gormZapLogger) Warn(ctx context.Context, msg string, args ...interface{}) {
	l.logger.Sugar().Warnf(msg, args...)
}

func (l *gormZapLogger) Error(ctx context.Context, msg string, args ...interface{}) {
	l.logger.Sugar().Errorf(msg, args...)
}

func (l *gormZapLogger) Trace(ctx context.Context, begin time.Time, fc func() (string, int64), err error) {
	elapsed := time.Since(begin)
	sql, rows := fc()
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		l.logger.Error("gorm query error",
			zap.Error(err),
			zap.String("sql", sql),
			zap.Int64("rows", rows),
			zap.Duration("elapsed", elapsed),
		)
		return
	}
	l.logger.Debug("gorm query",
		zap.String("sql", sql),
		zap.Int64("rows", rows),
		zap.Duration("elapsed", elapsed),
	)
}
