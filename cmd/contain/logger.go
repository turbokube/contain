package main

import (
	"fmt"
	"os"
	"strings"
	"time"

	"go.uber.org/zap"
	"go.uber.org/zap/buffer"
	"go.uber.org/zap/zapcore"
)

// newLogger creates a logger based on global flags and env overrides.
// Flags: --logger=dev|plain, -x for debug
// Envs: CONTAIN_LOG_MODE=dev|plain, CONTAIN_LOG_LEVEL=debug|info|warn|error
// CLI flags take precedence over env; env only applies when the flag value is at its default.
func newLogger() *zap.Logger {
	// Effective mode
	effMode := loggerMode
	if effMode == "dev" { // default flag value; allow env to override
		if m := strings.TrimSpace(os.Getenv("CONTAIN_LOG_MODE")); m != "" {
			effMode = m
		}
	}
	// Effective level
	effLevel := zapcore.InfoLevel
	if debug { // CLI flag overrides env
		effLevel = zapcore.DebugLevel
	} else if lv := strings.TrimSpace(os.Getenv("CONTAIN_LOG_LEVEL")); lv != "" {
		if parsed, ok := parseLogLevel(lv); ok {
			effLevel = parsed
		}
	}
	switch effMode {
	case "plain":
		return newPlainLogger(effLevel)
	case "dev":
		fallthrough
	default:
		consoleDebugging := zapcore.Lock(os.Stderr)
		consoleEncoder := zapcore.NewConsoleEncoder(zap.NewDevelopmentEncoderConfig())
		consoleEnabler := zap.LevelEnablerFunc(func(lvl zapcore.Level) bool { return lvl >= effLevel })
		core := zapcore.NewCore(consoleEncoder, consoleDebugging, consoleEnabler)
		logger := zap.New(core)
		return logger
	}
}

// newPlainLogger returns a logger that prints: LEVEL msg key=value ...
// Values with whitespace are quoted. Booleans and numbers are left as-is.
func newPlainLogger(effLevel zapcore.Level) *zap.Logger {
	enc := &plainEncoder{}
	enabler := zap.LevelEnablerFunc(func(lvl zapcore.Level) bool { return lvl >= effLevel })
	core := zapcore.NewCore(enc, zapcore.Lock(os.Stderr), enabler)
	return zap.New(core)
}

// plainEncoder implements zapcore.Encoder
type plainEncoder struct{}

func (e *plainEncoder) EncodeEntry(ent zapcore.Entry, fields []zap.Field) (*buffer.Buffer, error) {
	buf := buffer.NewPool().Get()
	// Level padded to fixed width so messages align
	level := strings.ToUpper(ent.Level.String())
	const levelWidth = 6 // accommodates DPANIC
	buf.AppendString(level)
	if l := len(level); l < levelWidth {
		buf.AppendString(strings.Repeat(" ", levelWidth-l))
	}
	buf.AppendByte(' ')
	// Message
	buf.AppendString(ent.Message)
	// Fields
	for i := range fields {
		f := fields[i]
		buf.AppendByte(' ')
		buf.AppendString(f.Key)
		buf.AppendByte('=')
		switch f.Type {
		case zapcore.StringType:
			v := f.String
			if strings.ContainsAny(v, " \t") {
				buf.AppendByte('"')
				buf.AppendString(v)
				buf.AppendByte('"')
			} else {
				buf.AppendString(v)
			}
		case zapcore.BoolType:
			if f.Integer == 0 {
				buf.AppendString("false")
			} else {
				buf.AppendString("true")
			}
		case zapcore.Int64Type, zapcore.Int32Type, zapcore.Int16Type, zapcore.Int8Type:
			buf.AppendInt(f.Integer)
		case zapcore.Uint64Type, zapcore.Uint32Type, zapcore.Uint16Type, zapcore.Uint8Type:
			buf.AppendUint(uint64(f.Integer))
		case zapcore.Float64Type, zapcore.Float32Type:
			buf.AppendString(fmt.Sprintf("%v", f.Interface))
		case zapcore.ErrorType:
			if err, ok := f.Interface.(error); ok {
				v := err.Error()
				if strings.ContainsAny(v, " \t") {
					buf.AppendByte('"')
					buf.AppendString(v)
					buf.AppendByte('"')
				} else {
					buf.AppendString(v)
				}
			} else {
				buf.AppendString("<error>")
			}
		default:
			buf.AppendString(fmt.Sprintf("%v", f.Interface))
		}
	}
	buf.AppendByte('\n')
	return buf, nil
}

// Cloning returns a copy so mutations don't race
func (e *plainEncoder) Clone() zapcore.Encoder { return &plainEncoder{} }

// Below are required to satisfy the interface; we don't use structured state.
func (e *plainEncoder) AddArray(k string, v zapcore.ArrayMarshaler) error   { return nil }
func (e *plainEncoder) AddObject(k string, v zapcore.ObjectMarshaler) error { return nil }
func (e *plainEncoder) AddBinary(k string, v []byte)                        {}
func (e *plainEncoder) AddByteString(k string, v []byte)                    {}
func (e *plainEncoder) AddBool(k string, v bool)                            {}
func (e *plainEncoder) AddDuration(k string, v time.Duration)               {}
func (e *plainEncoder) AddComplex128(k string, v complex128)                {}
func (e *plainEncoder) AddComplex64(k string, v complex64)                  {}
func (e *plainEncoder) AddFloat64(k string, v float64)                      {}
func (e *plainEncoder) AddFloat32(k string, v float32)                      {}
func (e *plainEncoder) AddInt(k string, v int)                              {}
func (e *plainEncoder) AddInt64(k string, v int64)                          {}
func (e *plainEncoder) AddInt32(k string, v int32)                          {}
func (e *plainEncoder) AddInt16(k string, v int16)                          {}
func (e *plainEncoder) AddInt8(k string, v int8)                            {}
func (e *plainEncoder) AddString(k string, v string)                        {}
func (e *plainEncoder) AddUint(k string, v uint)                            {}
func (e *plainEncoder) AddUint64(k string, v uint64)                        {}
func (e *plainEncoder) AddUint32(k string, v uint32)                        {}
func (e *plainEncoder) AddUint16(k string, v uint16)                        {}
func (e *plainEncoder) AddUint8(k string, v uint8)                          {}
func (e *plainEncoder) AddUintptr(k string, v uintptr)                      {}
func (e *plainEncoder) AddReflected(k string, v interface{}) error          { return nil }
func (e *plainEncoder) OpenNamespace(k string)                              {}
func (e *plainEncoder) AddTime(k string, v time.Time)                       {}

// parseLogLevel maps string to zapcore.Level
func parseLogLevel(s string) (zapcore.Level, bool) {
	switch strings.ToLower(s) {
	case "debug":
		return zapcore.DebugLevel, true
	case "info":
		return zapcore.InfoLevel, true
	case "warn", "warning":
		return zapcore.WarnLevel, true
	case "error":
		return zapcore.ErrorLevel, true
	case "dpanic":
		return zapcore.DPanicLevel, true
	case "panic":
		return zapcore.PanicLevel, true
	case "fatal":
		return zapcore.FatalLevel, true
	default:
		return zapcore.InfoLevel, false
	}
}
