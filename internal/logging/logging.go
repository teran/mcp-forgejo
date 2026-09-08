// Package logging configures a logrus logger with a sink appropriate to the
// transport (L1): HTTP/SSE logs to stdout, stdio logs to a file. It has no
// internal dependencies.
package logging

import (
	"fmt"
	"io"
	"os"

	"github.com/sirupsen/logrus"
)

// Channel selects the logging sink for the active transport (L1).
type Channel string

const (
	// ChannelHTTP selects the stdout sink (12-factor) for HTTP/SSE.
	ChannelHTTP Channel = "http"
	// ChannelStdio selects the file sink for stdio (stdout is the MCP protocol).
	ChannelStdio Channel = "stdio"
)

// Setup configures logger for the given channel, level, filename and format.
//
// If level is empty, logging is disabled (L2) and the returned logger writes
// to io.Discard. For the stdio channel the log file is created with mode 0600
// (L3). The returned io.Closer is non-nil only when a log file was opened and
// must be closed by the caller.
func Setup(channel Channel, level, filename, format string) (*logrus.Logger, io.Closer, error) {
	logger := logrus.New()

	if level == "" {
		logger.SetOutput(io.Discard)
		return logger, nil, nil
	}

	lvl, err := logrus.ParseLevel(level)
	if err != nil {
		return nil, nil, fmt.Errorf("invalid log level %q: %w", level, err)
	}
	logger.SetLevel(lvl)

	if format == "json" {
		logger.SetFormatter(&logrus.JSONFormatter{})
	} else {
		logger.SetFormatter(&logrus.TextFormatter{FullTimestamp: true})
	}

	if channel == ChannelStdio {
		// #nosec G304 -- the log file path comes from operator config
		// (LOG_FILENAME), never from user input; writing logs there is the
		// intended behavior, not an arbitrary-file-inclusion vector.
		f, err := os.OpenFile(filename, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
		if err != nil {
			return nil, nil, fmt.Errorf("open log file: %w", err)
		}
		logger.SetOutput(f)
		return logger, f, nil
	}

	logger.SetOutput(os.Stdout)
	return logger, nil, nil
}
