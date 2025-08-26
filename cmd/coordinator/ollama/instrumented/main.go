package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"

	"github.com/joeshaw/envdecode"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"pantryagent"
	"pantryagent/coordinator/ollama"
	"pantryagent/slack"
	"pantryagent/tools"
	"pantryagent/tools/storage"
)

func main() {
	ctx := context.Background()

	var modelConfig pantryagent.ModelConfig
	if err := envdecode.Decode(&modelConfig); err != nil {
		log.Fatalf("SETUP: Failed to decode: %s", err)
	}

	var agentConfig pantryagent.AgentConfig
	if err := envdecode.Decode(&agentConfig); err != nil {
		log.Fatalf("SETUP: Failed to decode: %s", err)
	}

	ps := storage.NewFilePantryState(agentConfig.ArtifactsPantryPath)
	rs := storage.NewFileRecipeState(agentConfig.ArtifactsRecipesPath)
	registry, err := tools.NewRegistry(ps, rs)
	if err != nil {
		slog.Error("SETUP: Failed to create tool registry", "error", err)
		return
	}
	slog.Info("SETUP: Static pantry data loaded at initialization")

	logger, cleanup, err := newCoordinationLogger(modelConfig.ModelID)
	if err != nil {
		slog.Error("SETUP: Failed to create coordination logger", "error", err)
		return
	}
	defer func() {
		if err := cleanup(); err != nil {
			slog.Error("SETUP: Failed to flush coordination log", "error", err)
		}
	}()

	task := argOr(1, "Plan dinners for the next 3 days for 2 servings each. If perishables will expire, prioritize them. If an ingredient is missing, pick a different recipe. Return a day-by-day plan.")

	prompt, err := ollama.NewPrompt(task, registry)
	if err != nil {
		slog.Error("SETUP: Failed to apply system prompt", "error", err)
		return
	}

	llm, err := ollama.NewClient(ollama.ClientOpts{
		BaseEndpoint: agentConfig.BaseOllamaEndpoint,
		ModelID:      modelConfig.ModelID,
		Prompt:       prompt,
		HTTPClient:   http.DefaultClient,
	})
	if err != nil {
		slog.Error("SETUP: Failed to create LLM client", "error", err)
		return
	}

	tracerProvider, meterProvider, otelShutdown, err := pantryagent.InitOtel(ctx)
	if err != nil {
		slog.Error("SETUP: Failed to initialize OpenTelemetry", "error", err)
		return
	}
	defer func() {
		if err := otelShutdown(ctx); err != nil {
			slog.Error("SETUP: Failed to shutdown OpenTelemetry", "error", err)
		}
	}()

	tracer := tracerProvider.Tracer(pantryagent.TracerNameOllama)
	meter := meterProvider.Meter(pantryagent.TracerNameOllama)

	ctx, span := tracer.Start(ctx, pantryagent.TracerNameOllama, trace.WithAttributes(
		attribute.String("model.id", modelConfig.ModelID),
		attribute.Int("model.max_tokens", int(modelConfig.MaxTokens)),
		attribute.Float64("model.temperature", float64(modelConfig.Temperature)),
		attribute.Float64("model.top_p", float64(modelConfig.TopP)),
	))
	defer span.End()

	output, err := ollama.NewInstrumentedCoordinator(llm, registry, agentConfig.MaxIterations, logger, tracer, meter).Run(ctx, task)
	if err != nil {
		slog.Error("FAILURE: Error handling task", "error", err)
		return
	}

	testServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body := new(bytes.Buffer)
		body.ReadFrom(r.Body) // nolint: errcheck
		slog.Info("Received request",
			"method", r.Method,
			"path", r.URL.Path,
			"header", r.Header,
			"body", body.String(),
		)
		w.WriteHeader(http.StatusOK)
	}))
	defer testServer.Close()

	slackClient := slack.NewClient(testServer.URL, http.DefaultClient)
	if err := slackClient.PostMessage(ctx, "#general", output); err != nil {
		slog.Error("Failed to post result to Slack", "error", err)
	}
}

func argOr(i int, def string) string {
	if len(os.Args) > i {
		return os.Args[i]
	}
	return def
}

func newCoordinationLogger(modelID string) (pantryagent.CoordinationLogger, func() error, error) {
	logFilePath := pantryagent.NewCoordinationLogFilePath(modelID)
	logFile, err := os.OpenFile(logFilePath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		return nil, func() error { return err }, fmt.Errorf("failed to open log file: %w", err)
	}

	logger := pantryagent.NewFileCoordinationLogger(logFile)
	cleanup := func() error {
		return errors.Join(logger.Flush(), logFile.Close())
	}
	return logger, cleanup, nil
}
