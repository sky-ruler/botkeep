# Contributing to BotKeep

Thank you for your interest in contributing to **BotKeep**! We are building an ultra-lean, single-binary DevOps Server Management Dashboard that bypasses bloated client-side SPAs.

## Prerequisites

- **Go**: 1.22+ (tested on Go 1.23+)
- **Node.js**: 18+ (used strictly for the standalone Tailwind CSS compiler)
- **templ CLI**: `go install github.com/a-h/templ/cmd/templ@v0.3.833`

---

## Local Development Workflow

1. **Clone the repository**:
   ```bash
   git clone https://github.com/sky-ruler/botkeep.git
   cd botkeep
   ```

2. **Install Node dependencies**:
   ```bash
   npm install
   ```

3. **Generate templates & compile CSS**:
   ```bash
   # Generate templ code
   templ generate

   # Compile minified Tailwind CSS
   npm run build:css
   ```

4. **Run the server in development mode**:
   ```bash
   # Using Make
   make dev

   # Or on Windows using PowerShell
   .\build.ps1 -Target run

   # Or directly
   go run ./cmd/server
   ```

5. **Run test suite**:
   ```bash
   go test -v -race ./...
   ```

---

## Architecture Guidelines

- **No Heavy Client JavaScript**: All UI updates and state mutations MUST flow through **HTMX** DOM swaps (`hx-post`, `hx-get`, `hx-delete`, `hx-swap`).
- **Type-Safe Templating**: All HTML components MUST be written in `.templ` files and compiled using `templ generate`. Do not use untyped raw string HTML concatenations.
- **Pluggable Health Drivers**: Drivers belong in `internal/monitor/` or `internal/models/`. Any new driver (`http`, `system`, `tcp`, etc.) must implement non-blocking probing with configurable timeouts and record results into `probe_history`.
- **Strict Linear/Vercel Theme**: Adhere to the established Tailwind Zinc palette (`bg-zinc-950`, `bg-zinc-900`, `border-zinc-800`).
- **Single-Binary Principle**: All static assets and schemas MUST remain embeddable via Go `embed` (`//go:embed`). Avoid external runtime disk dependencies.
- **CGO-Free Guarantee**: The project relies on pure Go SQLite (`modernc.org/sqlite`) to guarantee seamless cross-compilation with `CGO_ENABLED=0` on all platforms.

---

## Submitting Pull Requests

1. Create a feature branch: `git checkout -b feat/my-feature`
2. Ensure all tests pass: `go test -v ./...`
3. Verify formatting: `go fmt ./...` and `templ fmt .`
4. Commit using Conventional Commits (`feat:`, `fix:`, `docs:`, `refactor:`)
5. Open a Pull Request on GitHub.
