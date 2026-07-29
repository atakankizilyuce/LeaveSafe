package main

import (
	"fmt"
	"path/filepath"

	log "github.com/sirupsen/logrus"

	"github.com/leavesafe/leavesafe/internal/config"
	"github.com/leavesafe/leavesafe/internal/rotate"
)

// logFileName is the rotating application log kept next to the config.
const logFileName = "leavesafe.log"

// fileHook mirrors every log entry into a rotating file.
//
// The dashboard owns the terminal, so log lines scroll past inside a scrolling
// region and are gone the moment the window closes. That is fine while someone
// is watching, and useless afterwards: when a user reports that the alarm did
// not fire last Tuesday, the terminal is long gone. The file is what makes that
// question answerable.
//
// It is a hook rather than a second io.Writer because the file wants a full
// date on every line, while the dashboard only has room for a clock.
type fileHook struct {
	writer    *rotate.Writer
	formatter log.Formatter
}

// newFileHook opens the rotating log file in dir.
func newFileHook(dir string) (*fileHook, error) {
	w, err := rotate.Open(filepath.Join(dir, logFileName), rotate.Options{})
	if err != nil {
		return nil, err
	}
	return &fileHook{
		writer: w,
		formatter: &log.TextFormatter{
			DisableColors:   true,
			FullTimestamp:   true,
			TimestampFormat: "2006-01-02 15:04:05",
		},
	}, nil
}

// Levels reports which entries the hook wants. Everything the logger emits goes
// to the file: the level filtering happens once, on the logger itself.
func (h *fileHook) Levels() []log.Level { return log.AllLevels }

// Fire writes one entry to the file.
func (h *fileHook) Fire(entry *log.Entry) error {
	line, err := h.formatter.Format(entry)
	if err != nil {
		return fmt.Errorf("format log entry: %w", err)
	}
	_, err = h.writer.Write(line)
	return err
}

// Close closes the underlying file.
func (h *fileHook) Close() error { return h.writer.Close() }

// Path returns the path of the live log file.
func (h *fileHook) Path() string { return h.writer.Path() }

// LogFilePath returns where the application log lives, for messages that tell
// the user where to look.
func LogFilePath() string {
	return filepath.Join(config.ConfigDir(), logFileName)
}
