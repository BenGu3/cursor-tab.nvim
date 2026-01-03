.PHONY: build install clean generate

generate:
	@echo "Generating protobuf code..."
	buf generate

build: generate
	@echo "Building cursor-tab RPC server..."
	go build -o bin/cursor-tab-server ./cmd/server

install: build
	@echo "Installing to /usr/local/bin..."
	cp bin/cursor-tab-server /usr/local/bin/

clean:
	rm -rf bin/
	rm -rf cursor-api/gen/
	rm -f /tmp/cursor-tab.log

run: build
	./bin/cursor-tab-server

deps:
	go mod download
	go mod tidy
