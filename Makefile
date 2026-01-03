.PHONY: build install clean

# Build the RPC server
build:
	@echo "Building cursor-tab RPC server..."
	go build -o bin/cursor-tab-server ./cmd/server

# Install the binary to a location in PATH (optional)
install: build
	@echo "Installing to /usr/local/bin..."
	cp bin/cursor-tab-server /usr/local/bin/

# Clean build artifacts
clean:
	rm -rf bin/
	rm -f /tmp/cursor-tab.log

# Run the server (for testing)
run: build
	./bin/cursor-tab-server

# Download dependencies
deps:
	go mod download
	go mod tidy
