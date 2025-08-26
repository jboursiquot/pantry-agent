
# Pantry Agent: Learning to Build AI Agents

This project is a Go-based, AI-powered meal planning system with a modular coordinator architecture, observability instrumentation, and support for both cloud and local LLM backends. It is used for teaching simple agentic concepts and workflows.

---

## Table of Contents
- [Coordinator Types & Overview](#coordinator-types--overview)
- [Usage & Makefile Commands](#usage--makefile-commands)
- [Environment Configuration](#environment-configuration)

---

## Coordinator Types & Overview

### 1. Mock Coordinator
- **Purpose:** Testing and teaching with predictable responses
- **Location:** `coordinator/mock/`
- **Entry:** `cmd/coordinator/mock/main.go`
- **Features:**
	- Canned responses, no external dependencies
	- Simple code, basic logging
	- No metrics or tracing

### 2. Ollama Coordinator
- **Purpose:** Local development and testing with Ollama models
- **Location:** `coordinator/ollama/`
- **Entry:** `cmd/coordinator/ollama/main.go`
- **Features:**
	- Local Ollama model integration
	- Tool call deduplication, native tool calling
	- Lightweight validation

### 3. Ollama Instrumented Coordinator
- **Purpose:** Local development/testing with Ollama models and observability
- **Location:** `coordinator/ollama/`
- **Entry:** `cmd/coordinator/ollama/instrumented/main.go`
- **Features:**
	- All features of the standard Ollama coordinator
	- Adds observability: metrics, tracing, deduplication metrics

### 4. Bedrock Coordinator
- **Purpose:** AWS Bedrock integration for production and advanced validation
- **Location:** `coordinator/bedrock/`
- **Entry:** `cmd/coordinator/bedrock/local/main.go`
- **Features:**
	- AWS Bedrock Claude integration
	- Feasibility checking, tool repetition prevention
	- Static data validation, error handling

### 5. Bedrock Instrumented Coordinator
- **Purpose:** Production Bedrock with full observability
- **Location:** `coordinator/bedrock/`
- **Entry:** `cmd/coordinator/instrumented/main.go`
- **Features:**
	- All features of the standard Bedrock coordinator
	- Adds observability: metrics, tracing, feasibility metrics

---

## Usage & Makefile Commands

### Running
```bash
# Mock coordinator
make run-mock-example

# Ollama coordinators  
make run-ollama-server
make run-ollama-example
make run-ollama-instrumented

# Bedrock coordinators
make run-bedrock-local
make run-bedrock-instrumented
```

---

## Environment Configuration

### Universal Variables
```bash
# Model configuration
MODEL_ID=<model-specific-id>
MAX_TOKENS=1024
TEMPERATURE=0.2
TOP_P=0.9

# Agent configuration  
MAX_ITERATIONS=10
ARTIFACTS_PANTRY_PATH=artifacts/pantry.json
ARTIFACTS_RECIPES_PATH=artifacts/recipes.json

# OpenTelemetry (for instrumented versions)
OTEL_EXPORTER_OTLP_ENDPOINT=<your-endpoint>
OTEL_EXPORTER_OTLP_HEADERS=<auth-headers>
OTEL_SERVICE_NAME=pantry-agent
OTEL_SERVICE_VERSION=0.1.0
OTEL_DEPLOY_ENV=development
```

### Bedrock-Specific
```bash
# AWS configuration
AWS_PROFILE=<your-profile>
AWS_REGION=us-east-1
BEDROCK_MODEL_ID=us.anthropic.claude-3-7-sonnet-20250219-v1:0
```

### Ollama-Specific
```bash
# Ollama configuration
BASE_OLLAMA_ENDPOINT=http://localhost:11434
OLLAMA_MODEL_ID=llama3.2
```