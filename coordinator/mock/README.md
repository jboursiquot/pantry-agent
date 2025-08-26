# Mock Coordinator - Learning Agentic AI

This is your introduction to agentic AI systems. No complex observability, no cloud dependencies. Just the core concepts you need to understand.

## What's an AI Agent?

An AI agent isn't just a chatbot. It's a system that can:
- Reason about problems
- Use tools to gather information 
- Take actions based on what it learns
- Iterate until it solves the task

## The Agentic Loop

Every AI agent follows the same pattern:

1. **Receive a task** - "Plan dinners for the next 3 days for 2 servings each"
2. **Reason about what's needed** - "I need pantry data and recipes"
3. **Use tools** - Call `pantry_get` and `recipe_get` 
4. **Process results** - Analyze ingredients vs recipe requirements
5. **Make decisions** - Pick feasible recipes, check quantities
6. **Return final answer** - Complete and return a meal plan

The coordinator orchestrates this loop. It's the brain that decides when to call tools, when to retry, when to stop.

## Key Components

### LLM (Language Model)
The reasoning engine. Given a prompt with available tools, it decides:
- Which tools to call
- What arguments to pass
- How to interpret tool results
- When the task is complete

### Tools
Structured functions the LLM can invoke:
- `pantry_get` - Returns current ingredient inventory
- `recipe_get` - Returns available recipes by meal type

Each tool has a schema defining inputs/outputs. The LLM learns to call them correctly.

### Memory (Context)
The conversation history between coordinator and LLM:
- Original task
- Tool calls made
- Tool results received
- LLM responses

This builds up context so the agent can reason across multiple steps.

### Tool Provider
The registry that maps tool names to actual implementations. When the LLM says "call pantry_get", the coordinator looks up the real function.

## Mock Implementation

This mock coordinator uses fake responses instead of real LLMs. Perfect for learning because you can see exactly what happens at each step:

**Phase 1**: No tool results yet → Returns tool calls
**Phase 2**: Has tool results → Returns final meal plan

The `LLMClient` simulates how a real LLM would behave, but with predictable responses.

## Why This Matters

Traditional software is deterministic. You write `if/else` logic that always produces the same output for the same input.

Agentic systems are probabilistic. The LLM makes decisions based on reasoning, not hardcoded rules. This enables:
- Handling unexpected scenarios
- Adapting to new data
- Natural language interfaces
- Complex multi-step workflows

But it also means you need different testing, monitoring, and reliability patterns.

## Running the Mock

```bash
make run-mock-example
```

Watch the coordinator orchestrate the interaction. See how it:
1. Receives the task
2. Calls the mock LLM
3. Gets tool calls back
4. Executes tools via the registry
5. Sends results back to LLM
6. Gets final meal plan

This is the foundation. Real coordinators add error handling, retries, validation, and observability. But the core loop remains the same.

## Logging & Sanity Checks

Each time you run the examples a log will be generated, showing the step-by-step process and the decisions made by the coordinator. These logs live in the `logs` directory and will have the timestamp and name of the model used during the run. 

Agentic systems can be incredibly hard to debug due to their non-deterministic nature. Good observability practices will be your friend. For now, we'll rely on the logs generated with each run heavily to see what we're sending to the model and what're getting back.

## Next Steps

As we progress through the training, each version of our agent will build on similar concepts seen in this mock implementation, but with actual model use and integration.

Once you understand this mock, it's time to build your own agent. But first, you'll want to get familiar with Ollama [tool support](https://ollama.com/blog/tool-support).
