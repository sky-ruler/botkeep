.PHONY: all build dev templ css clean run

# Binary name
BINARY_NAME=botkeep
BINARY_PATH=./bin/$(BINARY_NAME)

# Default target
all: build

# Generate templ components
templ:
	templ generate

# Build minified Tailwind CSS
css:
	npm run build:css

# Build single executable binary with embedded assets
build: templ css
	go build -ldflags="-s -w" -o $(BINARY_PATH) ./cmd/server

# Run the server
run: build
	$(BINARY_PATH)

# Development mode (generate templates, build css, run)
dev: templ css
	go run ./cmd/server

# Clean generated artifacts
clean:
	rm -rf bin/ static/css/output.css *_templ.go views/**/*_templ.go
