# Bedrock Coordinator - Production Cloud LLMs

This is where we move from local development to production-ready cloud AI. AWS Bedrock provides managed access to frontier models like Claude, with enterprise-grade reliability, security, and observability.

## AWS Bedrock vs Local Models

### Why Bedrock?

> We talk about Bedrock here but the same considerations apply to any cloud-based LLM service.

- **Enterprise reliability** - SLA guarantees, multi-AZ deployment, managed infrastructure
- **Security and compliance** - VPC endpoints, encryption, audit trails, no data retention
- **Model access** - Latest Claude models without managing infrastructure
- **Scalability** - Auto-scaling, rate limiting, cost controls
- **Integration** - Native AWS SDK, IAM permissions, CloudTrail and CloudWatch logging

### (Obvious) Trade-offs

- **Cost** - Pay per token vs free local inference
- **Latency** - Network calls vs local processing
- **Dependencies** - AWS credentials, network connectivity
- **Vendor lock-in** - Anthropic models via AWS but you could use model providers directly if you so wish

## Claude Models & Access

Our Bedrock client targets Claude 3.7 Sonnet via inference profiles. See [AWS documentation](https://docs.aws.amazon.com/bedrock/latest/userguide/inference-profiles.html) for details. You should request model access via the Bedrock console (if you haven't already). Once approved, you'll be able to use the model in your applications.

### Model Parameters

- **Max Tokens** (1024) - Response length limit for cost control
- **Temperature** (0.2) - Low for consistent tool calling and JSON outputs
- **Top-P** (0.9) - Focused sampling for structured responses

These are tuned for agentic workflows. Claude excels at tool calling and structured reasoning, but still needs constrained parameters to avoid hallucination.

### Authentication and Setup

```bash
# Configure AWS credentials
aws configure

# Test Bedrock access
make test-bedrock-access

# Run coordinator
make run-bedrock-example
```

The coordinator uses default AWS credential chain (environment, profile, IAM roles).

## Key Architectural Differences

### Static Data Loading

The Bedrock coordinator takes a different approach, accepting both tool providers and static data upfront:

```go
func NewCoordinator(llm llmClient, toolRegistry pantryagent.ToolProvider, 
    pantryData map[string]any, recipeData []any, maxIterations int, 
    logger pantryagent.CoordinationLogger, tracerProvider *trace.TracerProvider) *Coordinator
```

The instructor's not in love with passing in the static data here like this and may refactor this at a later time (bookmark or clone the repo) for an updated version later 😊.

### Comprehensive Feasibility Checking  

The Bedrock coordinator includes sophisticated validation:

```go
feasible, problems, err := c.checkFeasible(finalJSON)
```

This validates:
- Recipe ingredient availability vs pantry quantities
- Unit compatibility (no automatic conversions)
- Serving size calculations
- Expiration date prioritization

> You likely already know this but LLMs can sound confident while being wrong. Validate their responses against hard constraints and nudge them as needed.

### Enhanced Error Handling

Production systems need robust error handling:
- AWS API failures and retries
- Token limit exceeded errors
- Claude-specific response parsing
- Structured error feedback to model

> Some rough safeguards are coded in to address the above but you'll want to enhance these for your production systems.

## Tool Calling with Claude via Bedrock Converse API

AWS Bedrock provides the [Converse API](https://docs.aws.amazon.com/bedrock/latest/userguide/conversation-inference.html) as a unified interface for multi-turn conversations and tool calling across different foundation models. This standardizes the interaction pattern regardless of whether you're using Claude, Titan, or other supported models.

### Converse API Benefits

- **Model-agnostic interface** - Same API works with Claude, Titan, Cohere, and others
- **Native tool calling support** - Built-in tool specification and result handling
- **Standardized conversation format** - Consistent message structure for multi-turn interactions
- **Token usage reporting** - Returns input/output token counts for cost calculation
- **AWS ecosystem integration** - IAM, CloudTrail, VPC endpoints, billing consolidation

### Trade-offs vs Direct Anthropic API

**What you gain with Bedrock:**
- Enterprise features (VPC, IAM, audit trails)
- AWS billing consolidation and credits
- Model switching without code changes (in theory)
- Regional deployment options

**What you lose:**
- **Pricing** - AWS markup on top of Anthropic's rates
- **Feature lag** - New Claude features _may_ arrive later on Bedrock
- **Model versions** - Limited selection compared to Anthropic's full catalog
- **Additional latency** - AWS proxy layer adds overhead to provide those aforementioned enterprise features
- **API differences** - Different request/response formats than Anthropic's native API

### Tool Calling Format

Tools are defined using Bedrock's tool specification format ([AWS documentation](https://docs.aws.amazon.com/bedrock/latest/userguide/tool-use-inference-call.html)):

```json
{
  "tools": [
    {
      "toolSpec": {
        "name": "pantry_get",
        "description": "Get current pantry inventory",
        "inputSchema": {
          "json": {
            "type": "object",
            "properties": {
              "current_day": {
                "type": "integer",
                "description": "Current day number for freshness calculations"
              }
            },
            "required": ["current_day"]
          }
        }
      }
    }
  ]
}
```

Claude responds with structured tool calls (this shows the conceptual structure - our LLM client parses the actual Bedrock response format):

```json
{
  "role": "assistant",
  "content": [
    {
      "toolUse": {
        "toolUseId": "tool_123", 
        "name": "pantry_get",
        "input": {"current_day": 0}
      }
    }
  ]
}
```

Tool results are sent back using our internal format (which gets converted to Bedrock's wire format):

```json
{
  "role": "user",
  "content": [
    {
      "type": "tool_result",
      "tool_use_id": "tool_123",
      "tool_name": "pantry_get", 
      "data": {"pantry": {"ingredients": [...]}}
    }
  ]
}
```

Our LLM client handles the conversion between our internal format and Bedrock's Converse API automatically.

## Running Bedrock Coordinator

```bash
# Requires AWS credentials configured
make run-bedrock-example
```

Notice the different behavior from local models:
- Faster reasoning, more reliable tool calls
- Better JSON structure adherence  
- More sophisticated ingredient feasibility analysis
- Higher consistency across runs

## Exercise: Build Your Production Agent

Implement a production-ready agentic system using Bedrock:

**Requirements:**
- AWS authentication and error handling
- Static data loading with validation (or data from external source if you're keen, maybe PostgreSQL or S3)
- Comprehensive feasibility checking
- Structured logging for debugging
- Cost tracking (token usage)

**Production considerations:**
- Handle AWS credential rotation
- Implement circuit breakers for API failures
- Add request/response caching
- Monitor token costs and usage patterns
- Set up alerts for high failure rates

Study the `checkFeasible()` method - it's more complex than the local coordinators.

---

## Production Observability at Scale

The `InstrumentedCoordinator` provides enhanced monitoring for our agentic system.

## Comprehensive Metrics

### Coordination Metrics
- `coordinator_runs_total` - Task attempts
- `coordinator_runs_completed_total` - Successful completions
- `coordinator_iterations_total` - Reasoning loops per task
- `coordination_duration_seconds` - End-to-end latency

### AWS Bedrock Metrics  
- `llm_calls_total` - Claude API invocations
- `llm_call_duration_seconds` - Claude response times
- `llm_tokens_total` - Token consumption (input/output)
- `llm_errors_total` - API failures by type

### Quality and Validation Metrics
- `feasibility_checks_total` - Recipe validation attempts
- `feasibility_checks_failed_total` - Infeasible plans  
- `feasibility_problems_count` - Problem severity gauge
- `json_validation_failures_total` - Malformed responses
- `tool_repetition_prevented_total` - Deduplication saves

### Business Logic Metrics
- `meal_plan_days_planned` - Plan complexity
- `meal_plan_total_servings` - Scale metrics
- `ingredient_waste_minimized` - Optimization success

## Why These Metrics Matter for Production

### Cost Management
Track token usage patterns to optimize:
- Prompt engineering for efficiency
- Model parameter tuning
- Caching opportunities
- Budget alerts and controls

### Performance Optimization  
Identify bottlenecks:
- Slow feasibility checks
- Claude API latency patterns
- Tool execution performance
- Iteration count optimization

### Quality Assurance
Monitor model reliability:
- Hallucination detection (feasibility failures)
- JSON structure compliance
- Tool calling accuracy
- Response consistency

### Operational Health
Production reliability metrics:
- Error rates by failure type
- Recovery patterns after failures
- Resource usage trends
- SLA compliance

## Distributed Tracing in Production

Each coordination creates detailed traces:

```
Bedrock Coordination Run
├── Iteration 1
│   ├── Claude API call
│   ├── Tool result parsing  
│   └── Response validation
├── Iteration 2
│   ├── Claude API call
│   ├── Feasibility checking
│   └── JSON validation
├── Iteration N
│   ├── Claude API call
│   ├── Tool usage enforcement
│   ├── Error handling and retries
│   └── Final feasibility validation
└── Final meal plan
```

Traces include:
- LLM call timing and response metadata
- Tool execution results and performance  
- Error information via `span.RecordError()`
- Timing data to identify slow operations

## Running Instrumented Bedrock

```bash
make run-bedrock-instrumented
```

Study the metrics output. Notice the additional validation and business logic metrics compared to Ollama.

## Exercise: Production Instrumentation

Add enterprise observability to your Bedrock agent:

1. **Cost tracking** - Monitor token usage trends and budget alerts
2. **Quality metrics** - Track hallucination rates and model reliability  
3. **Performance monitoring** - Identify API latency and optimization opportunities
4. **Business metrics** - Measure meal planning effectiveness and user satisfaction
5. **Error tracking** - Classify failures and recovery patterns

**Production patterns:**
- Initialize metrics once, record throughout request lifecycle
- Use structured attributes for filtering and aggregation
- Set up alerting on quality degradation
- Export metrics to monitoring systems (CloudWatch, Prometheus)
- Correlate traces with business outcomes

The instrumented coordinator shows enterprise patterns. Your production agentic systems need this level of observability to operate reliably at scale.

## Next Steps

This Bedrock coordinator represents production-grade agentic AI:
- Enterprise cloud model access
- Comprehensive validation and error handling
- Full observability and monitoring
- Cost and quality controls

These patterns scale to complex multi-agent systems, workflow orchestration, and production AI applications.

## Final Exercise: Add Nutrition Analysis

Now that you understand the complete Bedrock coordinator architecture, implement a nutrition analysis feature:

### The Challenge

The system has access to `artifacts/nutrition.json` containing detailed nutritional data for ingredients. Your task is to create a `nutrition_get` tool that Claude can use to provide nutritional analysis in meal plans.

### Requirements

1. **Create a nutrition tool** (`tools/nutrition_get.go`):
   - Name: `nutrition_get` 
   - Input: List of ingredient names
   - Output: Nutritional data per ingredient (calories, protein, vitamins, etc.)
   - Handle missing ingredients gracefully

2. **Update the tool registry** to include your nutrition tool

3. **Modify the coordinator** to load nutrition data and pass it to the tool

4. **Enhance the prompt** to instruct Claude to:
   - Call the nutrition tool when planning meals
   - Include nutritional summaries in the final meal plan
   - Consider nutritional balance in recipe selection

5. **Update the `MealPlan` struct** (if needed) to include nutritional information

### Implementation Hints

**Tool Structure:**
```go
type NutritionGet struct {
    state storage.NutritionState // You'll need to create this interface
}

func (t *NutritionGet) InputSchema() *jsonschema.Schema {
    // Accept array of ingredient names
}
```

**Integration Points:**
- Add nutrition data loading in `main.go` 
- Register the tool in the tool registry
- Update coordinator constructor to accept nutrition data
- Modify prompt system message to mention nutrition analysis

**Claude Behavior:**
After getting pantry and recipe data, Claude should call `nutrition_get` with the ingredients from selected recipes, then incorporate nutritional information into the final meal plan.

### Expected Output

Your enhanced meal plan should include:
```json
{
  "summary": "3-day meal plan with nutritional analysis",
  "days_planned": [...],
  "nutritional_summary": {
    "total_calories": 4200,
    "avg_calories_per_day": 1400,
    "total_protein_g": 180,
    "dietary_notes": ["High in protein", "Good calcium content"]
  }
}
```

### Validation

Test your implementation:
1. Run the coordinator and verify nutrition tool is called
2. Check that meal plans include nutritional data
3. Verify the tool handles missing nutrition data gracefully
4. Ensure Claude uses nutrition info in meal selection decisions

This exercise demonstrates real-world agentic system evolution - adding capabilities while maintaining architectural patterns and production reliability.
