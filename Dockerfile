# Stage 1: Build Tailwind CSS
FROM node:20-alpine AS css-builder
WORKDIR /app
COPY package*.json ./
RUN npm ci
COPY tailwind.config.js ./
COPY views/ ./views/
COPY static/css/input.css ./static/css/input.css
RUN npx tailwindcss -i ./static/css/input.css -o ./static/css/output.css --minify

# Stage 2: Compile templ & Build Go Binary
FROM golang:1.23-alpine AS go-builder
WORKDIR /app

# Install git, ca-certificates, and templ
RUN apk add --no-cache git ca-certificates
RUN go install github.com/a-h/templ/cmd/templ@v0.3.833

# Cache Go modules
COPY go.mod go.sum ./
RUN go mod download

# Copy source code and compiled CSS
COPY . .
COPY --from=css-builder /app/static/css/output.css ./static/css/output.css

# Generate templ components and build single binary
RUN templ generate
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w -extldflags '-static'" -o /app/botkeep ./cmd/server

# Stage 3: Minimal Ultra-Lean Final Image (~15MB)
FROM alpine:3.20 AS runner
RUN apk --no-cache add ca-certificates tzdata

WORKDIR /app
COPY --from=go-builder /app/botkeep /app/botkeep

# Default environment
ENV PORT=8080
ENV DB_PATH=/app/data/botkeep.db

# Persistent volume for SQLite data
VOLUME ["/app/data"]

EXPOSE 8080

HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
  CMD wget --no-verbose --tries=1 --spider http://localhost:8080/healthz || exit 1

ENTRYPOINT ["/app/botkeep"]
