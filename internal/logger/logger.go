package logger

import (
	"fmt"
	"github.com/rs/zerolog"
	"gopkg.in/natefinch/lumberjack.v2"
	"os"
	"path/filepath"
	"strings"
)

func GetLogPath() string {
	logsDir := os.Getenv("LOG_PATH")
	if logsDir == "" {
		// Create the logs directory if it doesn't exist
		logsDir = "logs"
	}

	if err := os.MkdirAll(logsDir, 0755); err != nil {
		panic(fmt.Sprintf("Failed to create logs directory: %v", err))
	}

	return filepath.Join(logsDir, "decypharr.log")
}

func NewLogger(prefix string, level string, output *os.File) zerolog.Logger {

	rotatingLogFile := &lumberjack.Logger{
		Filename:   GetLogPath(),
		MaxSize:    10,
		MaxBackups: 2,
		MaxAge:     28,
		Compress:   true,
	}

	consoleWriter := zerolog.ConsoleWriter{
		Out:        output,
		TimeFormat: "2006-01-02 15:04:05",
		NoColor:    false, // Set to true if you don't want colors
		FormatLevel: func(i interface{}) string {
			return strings.ToUpper(fmt.Sprintf("| %-6s|", i))
		},
		FormatMessage: func(i interface{}) string {
			return fmt.Sprintf("[%s] %v", prefix, i)
		},
	}

	fileWriter := zerolog.ConsoleWriter{
		Out:        rotatingLogFile,
		TimeFormat: "2006-01-02 15:04:05",
		NoColor:    true, // No colors in file output
		FormatLevel: func(i interface{}) string {
			return strings.ToUpper(fmt.Sprintf("| %-6s|", i))
		},
		FormatMessage: func(i interface{}) string {
			return fmt.Sprintf("[%s] %v", prefix, i)
		},
	}

	multi := zerolog.MultiLevelWriter(consoleWriter, fileWriter)

	logger := zerolog.New(multi).
		With().
		Timestamp().
		Logger().
		Level(zerolog.InfoLevel)

	// Set the log level
	switch level {
	case "debug":
		logger = logger.Level(zerolog.DebugLevel)
	case "info":
		logger = logger.Level(zerolog.InfoLevel)
	case "warn":
		logger = logger.Level(zerolog.WarnLevel)
	case "error":
		logger = logger.Level(zerolog.ErrorLevel)
	}
	return logger
}
