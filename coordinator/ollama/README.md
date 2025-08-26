# Ollama Coordinator - Real Local LLMs

Now we move from mock responses to real language models running locally via Ollama. Same agentic principles, but with actual model inference and the complexities that brings.

## Ollama and Tool Support

[Ollama](https://ollama.com) makes it simple to run LLMs locally. But not all models support tool calling - a critical capability for agentic workflows. More on [tool support](https://ollama.com/blog/tool-support).

### Tool-Compatible Models

Only certain models can execute function calls:
- `llama3.2` (3B and 1B)
- `llama3.1` (8B, 70B) 
- `mistral-nemo`
- `firefunction-v2`

> This list may certainly change by the time you're reading this in class.

The key is native tool support. These models were trained to understand function schemas and generate structured tool calls. Without that, you'll need to build your own parsing layer on top of plain text responses--not fun, at all.

### Request Format

Ollama follows the OpenAI tool calling convention:

```json
{
  "model": "llama3.2",
  "messages": [...],
  "tools": [
    {
      "type": "function", 
      "function": {
        "name": "pantry_get",
        "description": "Get current pantry inventory",
        "parameters": {
          "type": "object",
          "properties": {
            "current_day": {"type": "integer"}
          }
        }
      }
    }
  ]
}
```

### Response Format

Tool-enabled models return structured tool calls:

```json
{
  "message": {
    "role": "assistant",
    "tool_calls": [
      {
        "function": {
          "name": "pantry_get",
          "arguments": {"current_day": 0}
        }
      }
    ]
  }
}
```

During our coordination loop, one of our jobs will be to specify the tools available and parse these structured responses to execute the right functions.

## Model Parameters

Our Ollama client uses these parameters to control model behavior:

- **Temperature** (0.2) - Lower = more deterministic, higher = more creative
- **Top-P** (0.9) - Nucleus sampling for response diversity
- **Repeat Penalty** (1.05) - Reduces repetitive outputs (higher values = more penalty, not recommended to go above 1.2 for some models)
- **Context Length** - How much text we can fit in a single request (conversation history + tools + prompt)

> "nucleus sampling" means considering the smallest set of tokens whose cumulative probability exceeds the threshold p

These matter for agentic workflows. Too high temperature and your agent becomes unreliable. Too low and it gets stuck in patterns.

## Key Differences from Mock

1. **Real inference latency** - Local model calls take seconds, not milliseconds
2. **Non-deterministic responses** - Same prompt can yield different outputs
3. **Model limitations** - Context windows, reasoning capabilities, tool understanding
4. **Resource constraints** - Memory, CPU usage, model loading time
5. **Prompt Engineering** - Crafting effective prompts to guide model behavior will be a critical skill to master

## Running Ollama Coordinator

```bash
# Start Ollama service
ollama serve

# Pull a tool-compatible model (hopefully you did this before coming to class 😬)
ollama pull llama3.2

# Run our coordinator
make run-ollama-example
```

## Logging

Recall we mentioned that coordinator runs generate interaction logs in the `logs` directory. These logs are invaluable for debugging and understanding the agent's behavior. Open the latest log file to see the detailed interaction flow. Can you reason about what's being sent and what's being returned? What happens when you tweak the system prompt or model parameters?

## Exercise: Build Your Own Agent

Using this coordinator as reference, implement your own agentic system:

**Requirements:**
- Use the same tool registry (`pantry_get`, `recipe_get`)
- Enforce single tool calls (prevent repetitive data fetching)
- Validate final JSON against `MealPlan` struct
- **Stretch goal**- Check recipe feasibility against pantry quantities

**Key challenges you'll face:**
- Handling model hallucinations (invented recipe IDs for example)
- Managing context length limits
- Dealing with inconsistent tool call formats
- Validating LLM reasoning vs hard constraints

Study `ollama/coordinator.go` and `ollama/llm.go` for patterns. The core loop remains the same, but error handling becomes crucial with real models.

---

## Production Observability

Real agentic systems are hard to debug. Models make non-deterministic decisions. Tool calls might fail. Context windows fill up. You need observability.

> Note that we're not focusing on any one vendor's observability stack. The patterns here apply broadly and can be adapted to your chosen tools. It's more important that you learn what's worth tracking when building agentic systems.

## Instrumented Coordinator

The `InstrumentedCoordinator` adds comprehensive monitoring without changing the core logic:

### Metrics We Capture

**Coordination Flow:**
- `coordinator_runs_total` - How many tasks started
- `coordinator_runs_completed_total` - How many succeeded
- `coordinator_iterations_total` - Total reasoning loops
- `coordination_duration_seconds` - End-to-end latency

**Tool Usage:**
- `tool_calls_total` - Calls per tool type
- `tool_call_duration_seconds` - Tool execution time
- `tool_deduplication_prevented_total` - Repeated calls blocked

**LLM Behavior:**
- `llm_calls_total` - Model invocations
- `llm_call_duration_seconds` - Inference latency  
- `llm_tokens_used_total` - Token consumption
- `llm_tool_calls_extracted_total` - Successful tool parsing

**Quality Metrics:**
- `feasibility_checks_total` - Recipe validation attempts
- `feasibility_failures_total` - Infeasible plans rejected
- `json_validation_failures_total` - Malformed responses

### Why These Metrics Matter

- **Cost management** - Track token usage across runs
- **Performance optimization** - Identify slow tools or models
- **Quality assurance** - Monitor hallucination rates and validation failures
- **Debugging** - Correlate failures with specific tool calls or iterations

### Distributed Tracing

Each coordination run creates a trace with spans for:
- Overall coordination run
- Individual iterations (each reasoning loop)

Within each iteration span, we record metrics for tool calls, feasibility checks, and validation as span events and attributes.

### Observability Architecture

```
Coordinator Run
├── Iteration 1
│   ├── LLM call metrics
│   ├── Tool call metrics (pantry_get, recipe_get)
│   └── Response parsing metrics
├── Iteration 2
│   ├── LLM call metrics
│   ├── Feasibility check metrics
│   └── JSON validation metrics
├── Iteration N
│   ├── LLM call metrics
│   ├── Tool call metrics (pantry_get, recipe_get)
│   └── Feasibility check metrics
└── Final result
```

Each span includes:
- Duration metrics
- Success/failure status
- Relevant attributes (tool names, token counts, etc.)
- Error details when things go wrong

## Running Instrumented Coordinator

```bash
make run-ollama-instrumented
```

Compare the logs. Notice the additional metric recordings and trace spans. This is what production agentic systems need.

> The instructor uses Honeycomb to visualize traces and metrics, but you can adapt these patterns to your preferred observability stack.

## Exercise: Instrument Your Agent

Take the agent you built earlier and add observability:

1. **Add metrics** for coordination attempts, tool calls, and validation checks
2. **Add tracing** to show request flow and identify bottlenecks  
3. **Track quality metrics** like hallucination rates and feasibility failures
4. **Monitor resource usage** - tokens consumed, inference time, memory

**Key patterns:**
- Initialize metrics once in constructor
- Record metrics at decision points
- Use spans to trace request flow
- Add attributes for debugging context
- Handle metric recording failures gracefully

The instrumented coordinator shows you how. Focus on metrics that help you understand model behavior and system performance.

Your production agentic system will depend on this observability to debug issues, optimize costs, and ensure reliability.