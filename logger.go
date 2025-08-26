package pantryagent

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"time"
)

// CoordinationLogger is the interface for coordinator logging.
type CoordinationLogger interface {
	LogIteration(iteration IterationLog) error
}

// NewCoordinationLogFilePath returns a file path based on a cleaned up model name or id to make easier to identify specific logs produced with various models.
func NewCoordinationLogFilePath(model string) string {
	return fmt.Sprintf(
		"./logs/%d.%s.json",
		time.Now().Unix(),
		strings.ReplaceAll(strings.ToLower(model), ":", "_"),
	)
}

// IterationLog represents a single iteration in the coordination process
type IterationLog struct {
	Iteration int           `json:"iteration"`
	Timestamp time.Time     `json:"timestamp"`
	LLMInput  string        `json:"llm_input,omitempty"`
	LLMOutput any           `json:"llm_output"`
	ToolCalls []ToolCallLog `json:"tool_calls,omitempty"`
	Error     string        `json:"error,omitempty"`
}

// ToolCallLog represents a tool execution within a step
type ToolCallLog struct {
	Name   string         `json:"name"`
	Input  map[string]any `json:"input"`
	Output map[string]any `json:"output"`
	Error  string         `json:"error,omitempty"`
}

// FileCoordinationLogger logs to a file, accumulating iterations and flushing at the end
type FileCoordinationLogger struct {
	iterations []IterationLog
	writer     io.Writer
}

// NewFileCoordinationLogger creates a new file-based coordination logger
func NewFileCoordinationLogger(writer io.Writer) *FileCoordinationLogger {
	return &FileCoordinationLogger{
		iterations: make([]IterationLog, 0),
		writer:     writer,
	}
}

// LogIteration logs an iteration to the buffer (does not flush immediately)
func (fcl *FileCoordinationLogger) LogIteration(iteration IterationLog) error {
	fcl.iterations = append(fcl.iterations, iteration)
	return nil
}

// Flush flushes all accumulated iterations to the writer
func (fcl *FileCoordinationLogger) Flush() error {
	if fcl.writer == nil {
		return nil
	}

	data, err := json.MarshalIndent(map[string]any{
		"coordination_session": map[string]any{
			"timestamp":  time.Now(),
			"iterations": fcl.iterations,
		},
	}, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal coordination log: %w", err)
	}

	if _, err := fcl.writer.Write(data); err != nil {
		return fmt.Errorf("failed to write coordination log: %w", err)
	}

	// Clear the buffer after successful write
	fcl.iterations = fcl.iterations[:0]
	return nil
}

// NoOpCoordinationLogger is a logger that discards all log entries
type NoOpCoordinationLogger struct{}

// NewNoOpCoordinationLogger creates a new no-op coordination logger
func NewNoOpCoordinationLogger() *NoOpCoordinationLogger {
	return &NoOpCoordinationLogger{}
}

// LogIteration discards the iteration log (no-op)
func (nop *NoOpCoordinationLogger) LogIteration(iteration IterationLog) error {
	return nil
}

// StdoutCoordinationLogger logs each iteration as a JSON line to stdout (for Lambda/CloudWatch)
type StdoutCoordinationLogger struct{}

// NewStdoutCoordinationLogger creates a new stdout-based coordination logger
func NewStdoutCoordinationLogger() *StdoutCoordinationLogger {
	return &StdoutCoordinationLogger{}
}

// LogIteration writes the iteration as a JSON line to os.Stdout
func (l *StdoutCoordinationLogger) LogIteration(iteration IterationLog) error {
	data, err := json.Marshal(iteration)
	if err != nil {
		return err
	}
	fmt.Fprintln(os.Stdout, string(data))
	return nil
}
