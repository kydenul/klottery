package lottery

import "log"

// DefaultLogger implements Logger using standard log package
type DefaultLogger struct{}

// Info logs an info message
func (l *DefaultLogger) Info(msg string, args ...any) {
	log.Printf("[INFO] "+msg, args...)
}

// Error logs an error message
func (l *DefaultLogger) Error(msg string, args ...any) {
	log.Printf("[ERROR] "+msg, args...)
}

// Debug logs a debug message
func (l *DefaultLogger) Debug(msg string, args ...any) {
	log.Printf("[DEBUG] "+msg, args...)
}

// SilentLogger implements Logger interface but does not output any logs
// This is useful for testing environments where log output is not desired
type SilentLogger struct{}

// NewSilentLogger creates a new silent logger instance
func NewSilentLogger() *SilentLogger {
	return &SilentLogger{}
}

// Info does nothing (silent)
func (l *SilentLogger) Info(msg string, args ...any) {
	// Silent - no output
}

// Error does nothing (silent)
func (l *SilentLogger) Error(msg string, args ...any) {
	// Silent - no output
}

// Debug does nothing (silent)
func (l *SilentLogger) Debug(msg string, args ...any) {
	// Silent - no output
}
