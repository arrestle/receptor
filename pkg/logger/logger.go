package logger

import (
	"context"
	"fmt"
	"os"
	"reflect"
	"regexp"
	"strings"
	"sync"

	"github.com/ghjm/cmdline"
	"github.com/spf13/viper"
	"golang.org/x/exp/slog"
)

var (
	logLevel  slog.Level
	showTrace bool
)

var globalLevelVar *slog.LevelVar = new(slog.LevelVar)

// Log level constants.
const (
	ErrorLevel   = slog.LevelError
	WarningLevel = slog.LevelWarn
	InfoLevel    = slog.LevelInfo
	DebugLevel   = slog.LevelDebug
)

// QuietMode turns off all log output.
func SetGlobalQuietMode() {
	logLevel = slog.Level(-1) // Disable all logging
}

// SetLogLevel is a helper function for setting logLevel int.
func SetGlobalLogLevel(level slog.Level) {
	logLevel = level
	globalLevelVar.Set(level)
	fmt.Println("SetGlobalLogLevel current_level: ", logLevel, "globalLevelVar", globalLevelVar)
}

// GetLogLevelByName is a helper function for returning level associated with log
// level string.
func GetLogLevelByName(logName string) (int, error) {
	var err error
	if val, hasKey := logLevelMap[strings.ToLower(logName)]; hasKey {
		return int(val), nil
	}
	err = fmt.Errorf("%s is not a valid log level name", logName)

	return -1, err
}

// This doesn't seem to be used anywhere.
// GetLogLevel returns current log level.
func GetLogLevel() slog.Level {
	return logLevel
}

// logLevelMap maps strings to log level int
// allows for --LogLevel Debug at command line.
var logLevelMap = map[string]slog.Level{
	"error":   ErrorLevel,
	"warning": WarningLevel,
	"info":    InfoLevel,
	"debug":   DebugLevel,
}

var reverseLogLevelMap = map[slog.Level]string{
    slog.LevelDebug:  "debug",
    slog.LevelInfo:   "info",
    slog.LevelWarn:   "warning",
    slog.LevelError:  "error",
}


func (rl *ReceptorLogger) LogLevelToName(logLevel int) (string, error) {
    level := slog.Level(logLevel)
    name, ok := reverseLogLevelMap[level]
    if !ok {
        return "", fmt.Errorf("%d is not a valid log level", logLevel)
    }
    return name, nil
}


type MessageFunc func(level slog.Level, msg string, keysAndValues ...interface{})

var logger MessageFunc

// RegisterLogger registers a function for log delivery.
func RegisterLogger(msgFunc MessageFunc) {
	logger = msgFunc
}

type ReceptorLogger struct {
	logger *slog.Logger
	Prefix string
	m      sync.Mutex
}

// NewReceptorLogger to instantiate a new logger object.
func NewReceptorLogger(prefix string) *ReceptorLogger {

	globalLevelVar.Set(logLevel)

	handler := slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: globalLevelVar,
	})
	logger := slog.New(handler)

	return &ReceptorLogger{
		logger: logger,
		Prefix: prefix,
	}
}

// SetOutput sets the output destination for the logger.
func (rl *ReceptorLogger) SetOutput(w slog.Handler) {
	rl.logger = slog.New(w)
}

// SetShowTrace is a helper function for setting showTrace bool.
func (rl *ReceptorLogger) SetShowTrace(trace bool) {
	showTrace = trace
}

// GetLogLevel returns the log level.
func (rl *ReceptorLogger) GetLogLevel() int {
	level := int(slog.Level(logLevel))
	return level
}

// GetLogLevelByName returns the log level associated with the given log level name.
func (rl *ReceptorLogger) GetLogLevelByName(logName string) (int, error) {
	return GetLogLevelByName(logName)
}


// formatMessage checks for % in the message and performs formatting if necessary.
func formatMessage(msg string, keysAndValues ...interface{}) (string, int) {
	// count the number of % in the message
	count := strings.Count(msg, "%")
	if strings.Contains(msg, "%") {
		return fmt.Sprintf(msg, keysAndValues...), count
	}
	return msg, count
}

// Error reports unexpected behavior, likely to result in termination.
func (rl *ReceptorLogger) Error(msg string, keysAndValues ...interface{}) {
	rl.Log(int(ErrorLevel), msg, keysAndValues...)
}

// Warning reports unexpected behavior, not necessarily resulting in termination.
func (rl *ReceptorLogger) Warning(msg string, keysAndValues ...interface{}) {
	rl.Log(int(WarningLevel), msg, keysAndValues...)
}

// Info provides general purpose statements useful to end user.
func (rl *ReceptorLogger) Info(msg string, keysAndValues ...interface{}) {
	rl.Log(int(InfoLevel), msg, keysAndValues...)
}

// Debug contains extra information helpful to developers.
func (rl *ReceptorLogger) Debug(msg string, keysAndValues ...interface{}) {
	rl.Log(int(DebugLevel), msg, keysAndValues...)
}

// countFormatVerbs counts formatting directives like %v, %s, etc.
func countFormatVerbs(format string) int {
	re := regexp.MustCompile(`%[^%]`)
	return len(re.FindAllString(format, -1))
}

func processKeysAndValues(level slog.Level, keysAndValues ...interface{}) []interface{} {
	var result []interface{}

	for _, v := range keysAndValues {
		// Only process if it's specifically a *ReceptorLogRecord
		if rlr, ok := v.(*ReceptorLogRecord); ok {
			val := reflect.ValueOf(rlr).Elem()
			typ := reflect.TypeOf(*rlr)

			for i := 0; i < val.NumField(); i++ {
				fieldVal := val.Field(i)
				fieldType := typ.Field(i)

				if !fieldVal.CanInterface() {
					continue
				}

				// Inject log level into SeverityNumber if the field matches
				if fieldType.Name == "SeverityNumber" {
					result = append(result, fieldType.Name, convertSlogLevelToOTEL(level))
					continue
				}

				if shouldInclude(fieldVal) {
					result = append(result, fieldType.Name, fieldVal.Interface())
				}
			}
		} else {
			result = append(result, v)
		}
	}

	return result
}

func shouldInclude(v reflect.Value) bool {
	switch v.Kind() {
	case reflect.String:
		return v.String() != ""
	case reflect.Ptr, reflect.Interface, reflect.Slice, reflect.Map:
		return !v.IsNil()
	default:
		return true // Always include numbers, bools, structs, etc.
	}
}

func convertSlogLevelToOTEL(level slog.Level) int {
	switch {
	case level <= slog.LevelDebug:
		return 5 // DEBUG
	case level <= slog.LevelInfo:
		return 9 // INFO
	case level <= slog.LevelWarn:
		return 13 // WARN
	default:
		return 17 // ERROR
	}
}


// Log adds a prefix and prints a given log message.
func (rl *ReceptorLogger) Log(level int, msg string, keysAndValues ...interface{}) {

	slog_level := slog.Level(level)

	keysAndValues = processKeysAndValues(slog_level, keysAndValues...)


	if logger != nil {
		logger(slog_level, msg, keysAndValues...)
		return
	}

	if logLevel <= slog_level {
		// Count the number of formatting verbs in msg
		formatArgCount := countFormatVerbs(msg)

		// Cap args to avoid fmt.Printf (EXTRA ...) warnings
		if formatArgCount > len(keysAndValues) {
			formatArgCount = len(keysAndValues)
		}

		msg, _ := formatMessage(msg, keysAndValues[:formatArgCount]...)
		keysAndValues = keysAndValues[formatArgCount:]
		// for i := 1; i < len(keysAndValues); i += 2 {
		// 	keysAndValues[i] = convertToJSONIfNecessary(keysAndValues[i])
		// }
		rl.logger.Log(context.Background(), slog_level, msg, keysAndValues...)
	}
}


func (rl *ReceptorLogger) SetPrefix(prefix string) {
	rl.m.Lock()
	defer rl.m.Unlock()
	rl.Prefix = prefix
}

func (rl *ReceptorLogger) GetPrefix() string {
	rl.m.Lock()
	defer rl.m.Unlock()

	return rl.Prefix
}

type LoglevelCfg struct {
	Level string `description:"Log level: Error, Warning, Info or Debug" barevalue:"yes" default:"error"`
}

func (cfg LoglevelCfg) Init() error {
	var err error
	val, err := GetLogLevelByName(cfg.Level)
	if err != nil {
		return err
	}
	SetGlobalLogLevel(slog.Level(val))

	return nil
}

type TraceCfg struct{}

func (cfg TraceCfg) Prepare() error {
	return nil
}

func init() {
	version := viper.GetInt("version")
	if version > 1 {
		return
	}
	logLevel = InfoLevel
	showTrace = false

	cmdline.RegisterConfigTypeForApp("receptor-logging",
		"log-level", "Specifies the verbosity level for command output", LoglevelCfg{}, cmdline.Singleton)
	cmdline.RegisterConfigTypeForApp("receptor-logging",
		"trace", "Enables packet tracing output", TraceCfg{}, cmdline.Singleton)
}

// implement a Trace func
func (rl *ReceptorLogger) Trace(msg string, keysAndValues ...interface{}) {
	if showTrace {
		rl.Log(int(DebugLevel), msg, keysAndValues...)
	}
}

// SanitizedError logs an error message with sanitized input.
func (rl *ReceptorLogger) SanitizedError(msg string, keysAndValues ...interface{}) {
	// Implement sanitization logic here if needed
	rl.Error(msg, keysAndValues...)
}

// SanitizedWarning logs a warning message with sanitized input.
func (rl *ReceptorLogger) SanitizedWarning(msg string, keysAndValues ...interface{}) {
	// Implement sanitization logic here if needed
	rl.Warning(msg, keysAndValues...)
}

// SanitizedInfo logs an info message with sanitized input.
func (rl *ReceptorLogger) SanitizedInfo(msg string, keysAndValues ...interface{}) {
	// Implement sanitization logic here if needed
	rl.Info(msg, keysAndValues...)
}

// SanitizedDebug logs a debug message with sanitized input.
func (rl *ReceptorLogger) SanitizedDebug(msg string, keysAndValues ...interface{}) {
	// Implement sanitization logic here if needed
	rl.Debug(msg, keysAndValues...)
}

func (rl *ReceptorLogger) DebugPayload(payloadDebug int, payload, workUnitID, connectionType string, nodeid string) {

	receptorLogRecord := &ReceptorLogRecord{
	}

	if payloadDebug >= 1 && connectionType != "" {
		receptorLogRecord.Namespace = connectionType
		receptorLogRecord.NodeId = nodeid
	}

	if payloadDebug >= 2 {
		if workUnitID != "" {
			receptorLogRecord.TraceId = workUnitID
		} else {
			receptorLogRecord.Status = WorkloadStatus_WorkStateUnknown
		}
	}

	if payloadDebug >= 3 && payload != "" {
		receptorLogRecord.Body = payload
	}

	if payloadDebug >= 1 {
		rl.Debug("PACKET TRACING ENABLED", receptorLogRecord)
	}
}
