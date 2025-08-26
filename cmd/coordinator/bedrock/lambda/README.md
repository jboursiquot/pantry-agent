# Pantry Agent - AWS Lambda Deployment

This directory contains the AWS Lambda version of the Pantry Agent coordinator, demonstrating how to deploy LLM-powered applications in serverless environments. This deployment introduces several additional layers of complexity compared to running the coordinator as a standalone CLI application.

## Architecture Overview

The Lambda version transforms our CLI coordinator into a serverless function that:
- Accepts meal planning tasks via JSON input
- Loads pantry and recipe data from S3 instead of local files
- Orchestrates LLM interactions through AWS Bedrock
- Returns structured meal plans as JSON output

## Key Differences from CLI Version

### 1. **Cold Start Considerations**
Lambda functions experience "cold starts" when they haven't been invoked recently:
- **Impact**: Additional 1-3 seconds of latency for initial requests
- **Mitigation**: The function pre-loads data and initializes clients during startup
- **Student Note**: Consider provisioned concurrency for production workloads with strict latency requirements

### 2. **Execution Time Limits**
AWS Lambda has strict timeout constraints:
- **Default**: 3 seconds
- **Maximum**: 15 minutes
- **Current Setting**: 300 seconds (5 minutes) - see `deploy.yaml`
- **Challenge**: LLM coordination loops can be unpredictable in duration
- **Solution**: Our coordinator implements `MaxIterations` to prevent runaway loops

### 3. **Memory Constraints**
Lambda memory allocation affects both RAM and CPU performance:
- **Current Setting**: 256MB - sufficient for our coordination logic
- **Consideration**: Larger models or complex data processing may require more memory
- **Trade-off**: Higher memory = higher cost but better performance
