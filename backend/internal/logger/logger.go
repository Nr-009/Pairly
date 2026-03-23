package logger

import (
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

var Log *zap.Logger

func Init(environment string) error {
	var err error

	if environment == "production" {
		Log, err = zap.NewProduction()
	} else {
		cfg := zap.NewDevelopmentConfig()
		cfg.EncoderConfig.EncodeLevel = zapcore.CapitalColorLevelEncoder
		Log, err = cfg.Build()
	}

	if err != nil {
		return err
	}

	zap.ReplaceGlobals(Log)
	return nil
}

func Sync() {
	if Log != nil {
		_ = Log.Sync()
	}
}