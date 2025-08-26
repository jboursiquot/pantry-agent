package main

import (
	"context"
	"fmt"
	"log"
	"log/slog"
	"os"
	"pantryagent"
	"pantryagent/coordinator/bedrock"
	"pantryagent/tools"
	"pantryagent/tools/storage"

	"github.com/aws/aws-lambda-go/lambda"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/joeshaw/envdecode"
)

type Params struct {
	Task string `json:"task"`
}

type Results struct {
	Output any `json:"output"`
}

func main() {
	fn := func(ctx context.Context, params Params) (Results, error) {
		var modelConfig pantryagent.ModelConfig
		if err := envdecode.Decode(&modelConfig); err != nil {
			log.Fatalf("Failed to decode: %s", err)
		}

		var agentConfig pantryagent.AgentConfig
		if err := envdecode.Decode(&agentConfig); err != nil {
			log.Fatalf("Failed to decode: %s", err)
		}

		// S3 config from env
		s3Bucket := os.Getenv("ARTIFACTS_S3_BUCKET")
		pantryKey := os.Getenv("ARTIFACTS_PANTRY_S3_KEY")
		recipesKey := os.Getenv("ARTIFACTS_RECIPES_S3_KEY")
		if s3Bucket == "" || pantryKey == "" || recipesKey == "" {
			return Results{}, fmt.Errorf("missing S3 config: ARTIFACTS_S3_BUCKET, ARTIFACTS_PANTRY_S3_KEY, ARTIFACTS_RECIPES_S3_KEY must be set")
		}

		awsCfg, err := config.LoadDefaultConfig(ctx)
		if err != nil {
			return Results{}, fmt.Errorf("failed to load AWS config: %w", err)
		}
		s3Client := s3.NewFromConfig(awsCfg)

		ps := storage.NewS3PantryState(s3Client, s3Bucket, pantryKey)
		rs := storage.NewS3RecipeState(s3Client, s3Bucket, recipesKey)
		registry, err := tools.NewRegistry(ps, rs)
		if err != nil {
			slog.Error("SETUP: Failed to create tool registry", "error", err)
			return Results{}, err
		}
		slog.Info("SETUP: S3 pantry and recipe state initialized")

		// Use helpers to decode pantry and recipe data
		pantryData, err := loadPantryData(ps)
		if err != nil {
			slog.Error("SETUP: Failed to load pantry data from S3", "error", err)
			return Results{}, err
		}
		slog.Info("SETUP: Pantry data loaded from S3",
			"ingredients_count", func() int {
				if ingredients, ok := pantryData["ingredients"].([]any); ok {
					return len(ingredients)
				}
				return 0
			}())

		recipeData, err := loadRecipeData(rs)
		if err != nil {
			slog.Error("SETUP: Failed to load recipe data from S3", "error", err)
			return Results{}, err
		}
		slog.Info("SETUP: Recipe data loaded from S3", "recipes_count", len(recipeData))

		coordinationLogger := pantryagent.NewStdoutCoordinationLogger()

		brc, err := newBedrockRuntimeClient(ctx)
		if err != nil {
			slog.Error("SETUP: Failed to create Bedrock client", "error", err)
			return Results{}, err
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
			return Results{}, err
		}
		_ = meterProvider // TODO: Use meterProvider as needed
		defer func() {
			if err := otelShutdown(ctx); err != nil {
				slog.Error("SETUP: Failed to shutdown OpenTelemetry", "error", err)
			}
		}()

		output, err := bedrock.NewCoordinator(
			llm,
			registry,
			pantryData,
			recipeData,
			agentConfig.MaxIterations,
			coordinationLogger,
			tracerProvider).Run(ctx, params.Task)
		if err != nil {
			slog.Error("RESULT: Error handling task", "error", err)
			return Results{}, err
		}

		return Results{Output: output}, nil
	}

	lambda.Start(fn)
}

func newBedrockRuntimeClient(ctx context.Context) (*bedrockruntime.Client, error) {
	awsCfg, err := config.LoadDefaultConfig(ctx, config.WithRetryMaxAttempts(5))
	if err != nil {
		return nil, err
	}
	return bedrockruntime.NewFromConfig(awsCfg), nil
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

func loadRecipeData(rs storage.RecipeState) ([]any, error) {
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
