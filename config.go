package pantryagent

type ModelConfig struct {
	ModelID     string  `env:"MODEL_ID,required"`
	MaxTokens   int32   `env:"MAX_TOKENS,default=1024"`
	Temperature float32 `env:"TEMPERATURE,default=0.2"`
	TopP        float32 `env:"TOP_P,default=0.9"`
}

type AgentConfig struct {
	ArtifactsPantryPath  string `env:"ARTIFACTS_PANTRY_PATH,default=artifacts/pantry.json"`
	ArtifactsRecipesPath string `env:"ARTIFACTS_RECIPES_PATH,default=artifacts/recipes.json"`
	BaseOllamaEndpoint   string `env:"BASE_OLLAMA_ENDPOINT,default=http://localhost:11434"`
	MaxIterations        int    `env:"MAX_ITERATIONS,default=10"`
}
