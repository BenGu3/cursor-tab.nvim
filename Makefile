.PHONY: build install clean clean-logs generate

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

clean-logs:
	@echo "Cleaning logs..."
	rm -f /tmp/cursor-tab.log
	touch /tmp/cursor-tab.log
	@echo "Logs cleaned!"

run: build
	./bin/cursor-tab-server

deps:
	go mod download
	go mod tidy
