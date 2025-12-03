.PHONY: build run test clean install dev fmt vet lint deps

BINARY_NAME=torrent-tui
BUILD_DIR=./build
CMD_DIR=./cmd/torrent-tui

# Build the binary
build:
	@echo "Building $(BINARY_NAME)..."
	@mkdir -p $(BUILD_DIR)
	go build -o $(BUILD_DIR)/$(BINARY_NAME) $(CMD_DIR)

# Run in development mode
run: build
	@$(BUILD_DIR)/$(BINARY_NAME)

# Run without building (faster iteration)
dev:
	@go run $(CMD_DIR)

# Run tests
test:
	go test -v ./...

# Format code
fmt:
	gofmt -w .

# Run go vet
vet:
	go vet ./...

# Lint: vet + format check
lint: vet
	@echo "Checking formatting..."
	@test -z "$$(gofmt -l .)" || (echo "Files need formatting:"; gofmt -l .; exit 1)
	@echo "Lint passed"

# Clean build artifacts
clean:
	@rm -rf $(BUILD_DIR)
	@go clean

# Install to ~/.local/bin
install: build
	@mkdir -p ~/.local/bin
	@cp $(BUILD_DIR)/$(BINARY_NAME) ~/.local/bin/
	@echo "Installed to ~/.local/bin/$(BINARY_NAME)"

# Fetch dependencies
deps:
	go mod download
	go mod tidy
