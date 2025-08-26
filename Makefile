PROJECT = $(shell basename -s .git `git config --get remote.origin.url`)
REVISION ?= $(shell git rev-parse --short HEAD)
STACK_NAME ?= $(PROJECT)
ENV ?= dev
GH_USERNAME ?= set-me
BUCKET ?= $(GH_USERNAME)-$(STACK_NAME)-$(ENV)
CF_TEMPLATE ?= deploy/deploy.yaml
PACKAGE_TEMPLATE = deploy/package.yaml

.PHONY: default test cover lint vendor build validate
.PHONY: vars clean params build-lambda bucket zip package
.PHONY: deploy destroy describe outputs

default: help

help: ## show help message
	@awk 'BEGIN {FS = ":.*##"; printf "\nUsage:\n  make \033[36m\033[0m\n"} /^[$$()% a-zA-Z_-]+:.*?##/ { printf "  \033[36m%-15s\033[0m %s\n", $$1, $$2 } /^##@/ { printf "\n\033[1m%s\033[0m\n", substr($$0, 5) } ' $(MAKEFILE_LIST)

test: ## go test with vendor
	go test -v -race -mod=vendor ./...

lint: ## lint code
	golangci-lint run ./...
	yamllint -c yamllint.yaml ./deploy/deploy.yaml
	yamllint -c yamllint.yaml ./deploy/params.yaml

install-spew: ## install spew
	go get github.com/davecgh/go-spew/spew

vendor: ## vendor dependencies
	go mod tidy
	go mod vendor

cover: ## go test coverage
	go test -coverprofile=coverage.out ./... && go tool cover -html=coverage.out

vars: ## list Makefile variables
	@echo PROJECT: $(PROJECT)
	@echo REVISION: $(REVISION)
	@echo STACK_NAME: $(STACK_NAME)
	@echo BUCKET: $(BUCKET)
	@echo AWS_PROFILE: $(if $(AWS_PROFILE), $(AWS_PROFILE), not set)

bucket: ## create bucket and ignore error if it already exists
	-aws s3 mb s3://$(BUCKET) $(if $(AWS_PROFILE),--profile $(AWS_PROFILE))
	-aws s3api put-public-access-block \
    --bucket $(BUCKET) \
    --public-access-block-configuration "BlockPublicAcls=true,IgnorePublicAcls=true,BlockPublicPolicy=true,RestrictPublicBuckets=true" \
	$(if $(AWS_PROFILE),--profile $(AWS_PROFILE))
	-aws s3api put-bucket-encryption \
    --bucket $(BUCKET) \
    --server-side-encryption-configuration '{"Rules": [{"ApplyServerSideEncryptionByDefault": {"SSEAlgorithm": "AES256"}}]}' \
	$(if $(AWS_PROFILE),--profile $(AWS_PROFILE))

clean: ## clean build artifacts
	-rm -rf build/*

package: build-lambda zip-lambda bucket ## package cloudformation template
	sam validate --template $(CF_TEMPLATE) $(if $(AWS_PROFILE),--profile $(AWS_PROFILE))
	sam package \
		$(if $(AWS_PROFILE),--profile $(AWS_PROFILE)) \
		--template-file $(CF_TEMPLATE) \
		--output-template-file $(PACKAGE_TEMPLATE) \
		--s3-bucket $(BUCKET)

upload-artifacts: ## upload artifacts json files to the S3 bucket from stack outputs
	$(eval ARTIFACTS_BUCKET := $(shell aws cloudformation describe-stacks \
		--stack-name $(STACK_NAME) \
		$(if $(AWS_PROFILE),--profile $(AWS_PROFILE)) \
		--query "Stacks[0].Outputs[?OutputKey=='ArtifactsBucket'].OutputValue" \
		--output text))
	@if [ -z "$(ARTIFACTS_BUCKET)" ]; then \
		echo "Artifacts bucket not found in stack outputs"; \
		exit 1; \
	fi
	aws s3 cp artifacts/pantry.json s3://$(ARTIFACTS_BUCKET)/pantry.json $(if $(AWS_PROFILE),--profile $(AWS_PROFILE))
	aws s3 cp artifacts/recipes.json s3://$(ARTIFACTS_BUCKET)/recipes.json $(if $(AWS_PROFILE),--profile $(AWS_PROFILE))

deploy: bucket clean package ## deploy cloudformation template
	sam deploy \
		$(if $(AWS_PROFILE),--profile $(AWS_PROFILE)) \
		--stack-name $(STACK_NAME) \
		--template-file $(PACKAGE_TEMPLATE) \
		--capabilities CAPABILITY_NAMED_IAM \
		--no-fail-on-empty-changeset \
		--parameter-overrides \
			version=$(REVISION) \
			OtelExporterOtlpEndpoint=$(if $(OTEL_EXPORTER_OTLP_ENDPOINT),$(OTEL_EXPORTER_OTLP_ENDPOINT),set-me) \
			OtelExporterOtlpHeaders=$(if $(OTEL_EXPORTER_OTLP_HEADERS),$(OTEL_EXPORTER_OTLP_HEADERS),set-me) \
			OtelServiceVersion=$(if $(OTEL_SERVICE_VERSION),$(OTEL_SERVICE_VERSION),0.1.0) \
			OtelServiceName=$(if $(OTEL_SERVICE_NAME),$(OTEL_SERVICE_NAME),pantry-agent) \
			OtelDeployEnv=$(if $(OTEL_DEPLOY_ENV),$(OTEL_DEPLOY_ENV),development) \
		--disable-rollback

validate: ## validate cloudformation template
	aws cloudformation validate-template \
		$(if $(AWS_PROFILE),--profile $(AWS_PROFILE)) \
		--template-body file://$(CF_TEMPLATE)

destroy: clean ## destroy cloudformation stack
	-aws cloudformation delete-stack --stack-name $(STACK_NAME) $(if $(AWS_PROFILE),--profile $(AWS_PROFILE))

describe: ## describe cloudformation stack
	aws cloudformation describe-stacks \
		$(if $(AWS_PROFILE),--profile $(AWS_PROFILE)) \
		--stack-name $(STACK_NAME)

outputs: ## describe cloudformation stack outputs
	aws cloudformation describe-stacks \
		$(if $(AWS_PROFILE),--profile $(AWS_PROFILE)) \
		--stack-name $(STACK_NAME) \
		--query 'Stacks[].Outputs'

# ---------------------------------------- #

build-lambda: ## build binaries for deployment
	@mkdir -p ./build/coordinator-bedrock-lambda
	GOARCH=amd64 GOOS=linux CGO_ENABLED=0 go build -tags lambda.norpc -v -mod vendor -o ./build/coordinator-bedrock-lambda/bootstrap ./cmd/coordinator/bedrock/lambda

zip-lambda: ## zip binaries for deployment
	@cd ./build/coordinator-bedrock-lambda && zip bootstrap.zip bootstrap

build-local: ## build binaries for local testing
	go build -v -mod vendor -o ./build/coordinator-mock ./cmd/coordinator/mock
	go build -v -mod vendor -o ./build/coordinator-ollama ./cmd/coordinator/ollama
	go build -v -mod vendor -o ./build/coordinator-ollama-instrumented ./cmd/coordinator/ollama/instrumented
	go build -v -mod vendor -o ./build/coordinator-bedrock-local ./cmd/coordinator/bedrock/local
	go build -v -mod vendor -o ./build/coordinator-bedrock-instrumented ./cmd/coordinator/instrumented

run-mock-example: ## run mock coordinator agent
	go run -race ./cmd/coordinator/mock/*.go

run-ollama-server:
	ollama serve

run-ollama-example: ## run Ollama coordinator agent
	MODEL_ID=llama3.2 \
		go run -race ./cmd/coordinator/ollama/plain/*.go

run-ollama-instrumented-example: ## run instrumented Ollama coordinator agent locally
	MODEL_ID=llama3.2 \
		go run -race ./cmd/coordinator/ollama/instrumented/*.go

BEDROCK_MODEL_ID ?= us.anthropic.claude-3-7-sonnet-20250219-v1:0
test-bedrock-model-access: ## run accesss test for Bedrock model
	aws bedrock-runtime converse \
		--model-id $(BEDROCK_MODEL_ID) \
  		--messages '[{"role":"user","content":[{"text":"Say hello in JSON"}]}]' \
		--cli-binary-format raw-in-base64-out \
		--inference-config '{"maxTokens":100}'

run-bedrock-local-example: ## run Bedrock coordinator agent locally
	MODEL_ID=$(BEDROCK_MODEL_ID) \
		go run -race ./cmd/coordinator/bedrock/local/*.go

run-bedrock-instrumented-example: ## run instrumented Bedrock coordinator agent locally
	MODEL_ID=$(BEDROCK_MODEL_ID) \
		go run -race ./cmd/coordinator/bedrock/instrumented/*.go

run-bedrock-lambda: ## run Bedrock coordinator agent as lambda
	echo "Not implemented"