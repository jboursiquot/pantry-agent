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

	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime"
	"github.com/joeshaw/envdecode"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"pantryagent"
	"pantryagent/coordinator/bedrock"
	"pantryagent/slack"
	"pantryagent/tools"
	"pantryagent/tools/storage"
)

func main() {
	ctx := context.Background()

	var modelConfig pantryagent.ModelConfig
	if err := envdecode.Decode(&modelConfig); err != nil {
		log.Fatalf("Failed to decode: %s", err)
	}

	var agentConfig pantryagent.AgentConfig
	if err := envdecode.Decode(&agentConfig); err != nil {
		log.Fatalf("Failed to decode: %s", err)
	}

	ps := storage.NewFilePantryState(agentConfig.ArtifactsPantryPath)
	rs := storage.NewFileRecipeState(agentConfig.ArtifactsRecipesPath)
	registry, err := tools.NewRegistry(ps, rs)
	if err != nil {
		slog.Error("SETUP: Failed to create tool registry", "error", err)
		return
	}
	slog.Info("SETUP: Static pantry data loaded at initialization")

	// Load static pantry and recipe data
	pantryData, err := loadPantryData(ps)
	if err != nil {
		slog.Error("SETUP: Failed to load pantry data", "error", err)
		return
	}

	slog.Info("SETUP: Static pantry data loaded at initialization",
		"ingredients_count", func() int {
			if ingredients, ok := pantryData["ingredients"].([]any); ok {
				return len(ingredients)
			}
			return 0
		}())

	recipeData, err := loadRecipeData(rs)
	if err != nil {
		slog.Error("SETUP: Failed to load recipe data", "error", err)
		return
	}

	slog.Info("SETUP: Static recipe data loaded at initialization", "recipes_count", len(recipeData))

	task := argOr(1, "Plan dinners for the next 3 days for 2 servings each. If perishables will expire, prioritize them. If an ingredient is missing, pick a different recipe. Return a day-by-day plan.")

	logger, cleanup, err := newCoordinationLogger(modelConfig.ModelID)
	if err != nil {
		slog.Error("Failed to create coordination logger", "error", err)
		return
	}
	defer func() {
		if err := cleanup(); err != nil {
			slog.Error("Failed to flush coordination log", "error", err)
		}
	}()

	brc, err := newBedrockRuntimeClient(ctx)
	if err != nil {
		slog.Error("SETUP: Failed to create Bedrock client", "error", err)
		return
	}

	opts := bedrock.LLMOptions{
		ModelID:   modelConfig.ModelID,
		MaxTokens: modelConfig.MaxTokens,
		TopP:      modelConfig.TopP,
	}

	llm := bedrock.NewLLMClient(brc, opts)

	tracerProvider, meterProvider, otelShutdown, err := pantryagent.InitOtel(ctx)
	if err != nil {
		slog.Error("SETUP: Failed to initialize OpenTelemetry", "error", err)
		return
	}
	_ = meterProvider // TODO: Use meterProvider as needed
	defer func() {
		if err := otelShutdown(ctx); err != nil {
			slog.Error("SETUP: Failed to shutdown OpenTelemetry", "error", err)
		}
	}()

	tracer := tracerProvider.Tracer(pantryagent.TracerNameBedrock)
	ctx, span := tracer.Start(ctx, pantryagent.TracerNameBedrock, trace.WithAttributes(
		attribute.String("model.id", modelConfig.ModelID),
		attribute.Int("model.max_tokens", int(modelConfig.MaxTokens)),
		attribute.Float64("model.temperature", float64(modelConfig.Temperature)),
		attribute.Float64("model.top_p", float64(modelConfig.TopP)),
	))
	defer span.End()

	output, err := bedrock.NewCoordinator(
		llm,
		registry,
		pantryData,
		recipeData,
		agentConfig.MaxIterations,
		logger,
		tracerProvider).Run(ctx, task)
	if err != nil {
		slog.Error("RESULT: Error handling task", "error", err)
		return
	}

	testServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body := new(bytes.Buffer)
		body.ReadFrom(r.Body) // nolint: errcheck
		slog.Info("FINAL: Received request",
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

func newBedrockRuntimeClient(ctx context.Context) (*bedrockruntime.Client, error) {
	awsCfg, err := config.LoadDefaultConfig(ctx, config.WithRetryMaxAttempts(5))
	if err != nil {
		return nil, err
	}
	return bedrockruntime.NewFromConfig(awsCfg), nil
}

func argOr(i int, def string) string {
	if len(os.Args) > i {
		return os.Args[i]
	}
	return def
}

func loadPantryData(ps storage.PantryState) (map[string]any, error) {
	pantryTool := tools.NewPantryGet(ps)
	result, err := pantryTool.Run(context.Background(), map[string]any{"current_day": 0})
	if err != nil {
		return nil, fmt.Errorf("failed to load pantry: %w", err)
	}

	// Extract the inner pantry data from the tool result
	if pantryData, ok := result["pantry"].(map[string]any); ok {
		return pantryData, nil
	}

	return nil, fmt.Errorf("invalid pantry structure: missing 'pantry' key in result")
}

func loadRecipeData(rs *storage.FileRecipeState) ([]any, error) {
	recipeTool := tools.NewRecipeGet(rs)
	result, err := recipeTool.Run(context.Background(), map[string]any{"meal_types": []string{"dinner"}})
	if err != nil {
		return nil, fmt.Errorf("failed to load recipes: %w", err)
	}

	recipesRaw, ok := result["recipes"]
	if !ok {
		return nil, fmt.Errorf("no 'recipes' key in result")
	}

	// Handle both []any and []map[string]any types
	if recipes, ok := recipesRaw.([]any); ok {
		return recipes, nil
	} else if recipesMap, ok := recipesRaw.([]map[string]any); ok {
		// Convert []map[string]any to []any
		recipes := make([]any, len(recipesMap))
		for i, r := range recipesMap {
			recipes[i] = r
		}
		return recipes, nil
	}

	return nil, fmt.Errorf("invalid recipes data format: expected []any or []map[string]any, got %T", recipesRaw)
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
