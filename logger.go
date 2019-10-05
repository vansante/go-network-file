package networkfile

// Logger is an optional interface used for outputting debug logging
type Logger interface {
	Infof(format string, args ...interface{})
	Errorf(format string, args ...interface{})
}

// embedLogger is a embeddable struct to make another struct more easily loggable
type embedLogger struct {
	logger Logger
}

// SetLogger sets a logger on the server
func (l *embedLogger) SetLogger(logger Logger) {
	l.logger = logger
}

// Infof checks if a logger is present and logs to it at info level if it is
func (l *embedLogger) Infof(format string, args ...interface{}) {
	if l.logger == nil {
		return
	}
	l.logger.Infof(format, args...)
}

// Errorf checks if a logger is present and logs to it at error level if it is
func (l *embedLogger) Errorf(format string, args ...interface{}) {
	if l.logger == nil {
		return
	}
	l.logger.Errorf(format, args...)
}
