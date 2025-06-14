.PHONY: build clean test deps examples

# Default target
all: build

# Build the Link C library
deps:
	@echo "Building Ableton Link dependencies..."
	@./build.sh

# Generate Go code (builds dependencies automatically)
generate: deps
	@echo "Running go generate..."
	@go generate ./...

# Build the Go module
build: generate
	@echo "Building Go module..."
	@go build ./...

# Build static binary (Linux/Windows only)
build-static: generate
	@echo "Building static Go module..."
	@go build -tags static ./...

# Build examples
examples: build
	@echo "Building examples..."
	@cd examples && go build ./...

# Run tests
test: build
	@echo "Running tests..."
	@go test ./...

# Clean build artifacts
clean:
	@echo "Cleaning build artifacts..."
	@rm -rf vendor/link/build
	@go clean ./...
	@cd examples && go clean ./...

# Install dependencies and build everything
install: deps build examples
	@echo "Installation complete!"

# Help
help:
	@echo "Available targets:"
	@echo "  all       - Build everything (default)"
	@echo "  deps      - Build Ableton Link C library"
	@echo "  generate  - Run go generate"
	@echo "  build     - Build Go module"
	@echo "  examples  - Build examples"
	@echo "  test      - Run tests"
	@echo "  clean     - Clean build artifacts"
	@echo "  install   - Full build and install"
	@echo "  help      - Show this help"