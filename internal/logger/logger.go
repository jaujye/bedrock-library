package logger

import (
	"io"
	"os"
	"patyhank_gomc/config"

	"github.com/sirupsen/logrus"
)

type Logger struct {
	*logrus.Logger
	config *config.Config
}

// NewLogger creates a new logger instance based on configuration
func NewLogger(cfg *config.Config) *Logger {
	logger := logrus.New()

	// Set log level
	if cfg.IsDebugLevel() {
		logger.SetLevel(logrus.DebugLevel)
	} else {
		logger.SetLevel(logrus.InfoLevel)
	}

	// Set formatter
	var formatter logrus.Formatter
	if cfg.Logging.Format == "json" {
		formatter = &logrus.JSONFormatter{
			TimestampFormat: "15:04:05",
		}
	} else {
		formatter = &logrus.TextFormatter{
			TimestampFormat: "15:04:05",
			FullTimestamp:   cfg.Logging.IncludeTimestamp,
			ForceColors:     true,
		}
	}
	logger.SetFormatter(formatter)

	// Set output
	var outputs []io.Writer
	outputs = append(outputs, os.Stdout) // Always output to console

	// Add file output if enabled and debug level
	if cfg.ShouldLogToFile() {
		logFile, err := os.OpenFile(cfg.Logging.LogFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0666)
		if err != nil {
			logrus.Warnf("Failed to open log file %s: %v", cfg.Logging.LogFile, err)
		} else {
			outputs = append(outputs, logFile)
		}
	}

	if len(outputs) > 1 {
		logger.SetOutput(io.MultiWriter(outputs...))
	} else {
		logger.SetOutput(outputs[0])
	}

	return &Logger{
		Logger: logger,
		config: cfg,
	}
}

// LogPacket logs packet information based on configuration
func (l *Logger) LogPacket(packetName string, packetID uint32, data interface{}) {
	if l.config.IsDebugLevel() && l.config.Logging.IncludePacketDetails {
		l.Debugf("[PACKET] %s (ID: %d) - Data: %+v", packetName, packetID, data)
	} else if l.config.IsInfoLevel() {
		// Only log important packets for info level
		if isImportantPacket(packetName) {
			l.Infof("[PACKET] %s", packetName)
		}
	}
}

// LogText logs text messages (always shown in both modes)
func (l *Logger) LogText(sender, message string) {
	l.Infof("[CHAT] %s: %s", sender, message)
}

// LogSystemEvent logs important system events (always shown in both modes)
func (l *Logger) LogSystemEvent(event, message string) {
	l.Infof("[SYSTEM] %s: %s", event, message)
}

// LogConnection logs connection events (always shown in both modes)
func (l *Logger) LogConnection(event, details string) {
	l.Infof("[CONNECTION] %s: %s", event, details)
}

// LogError logs errors (always shown in both modes)
func (l *Logger) LogError(context, message string, err error) {
	if err != nil {
		l.Errorf("[ERROR] %s: %s - %v", context, message, err)
	} else {
		l.Errorf("[ERROR] %s: %s", context, message)
	}
}

// LogDebug logs debug information (only in debug mode)
func (l *Logger) LogDebug(context, message string) {
	if l.config.IsDebugLevel() {
		l.Debugf("[DEBUG] %s: %s", context, message)
	}
}

// isImportantPacket determines if a packet should be logged in info mode
func isImportantPacket(packetName string) bool {
	importantPackets := map[string]bool{
		"Text":           true, // Chat messages
		"Disconnect":     true, // Disconnection
		"PlayStatus":     true, // Play status changes
		"Kick":           true, // Player kicked
		"LoginStatus":    true, // Login status
		"ServerToClient": true, // Server messages
		"CommandOutput":  true, // Command responses
		"ModalForm":      true, // Form interactions
	}
	return importantPackets[packetName]
}

// GetConfig returns the logger's configuration
func (l *Logger) GetConfig() *config.Config {
	return l.config
}