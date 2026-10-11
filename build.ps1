param(
    [string]$Target = "build"
)

$ErrorActionPreference = "Stop"

switch ($Target.ToLower()) {
    "css" {
        Write-Host "==> Compiling minified Tailwind CSS..." -ForegroundColor Cyan
        npm run build:css
    }
    "templ" {
        Write-Host "==> Generating templ components..." -ForegroundColor Cyan
        templ generate
    }
    "build" {
        $templCmd = if (Get-Command templ -ErrorAction SilentlyContinue) { "templ" } elseif (Test-Path "$env:USERPROFILE\go\bin\templ.exe") { "$env:USERPROFILE\go\bin\templ.exe" } else { "templ" }
        $goCmd = if (Get-Command go -ErrorAction SilentlyContinue) { "go" } elseif (Test-Path "C:\Program Files\Go\bin\go.exe") { "C:\Program Files\Go\bin\go.exe" } else { "go" }
        Write-Host "==> Building BotKeep Single-Binary..." -ForegroundColor Green
        npm run build:css
        & $templCmd generate
        New-Item -ItemType Directory -Force -Path "bin" | Out-Null
        & $goCmd build -ldflags="-s -w" -o bin/botkeep.exe ./cmd/server
        Write-Host "==> Build complete: bin\botkeep.exe" -ForegroundColor Green
    }
    "run" {
        & "$PSScriptRoot\build.ps1" -Target "build"
        & "$PSScriptRoot\bin\botkeep.exe"
    }
    "clean" {
        Write-Host "==> Cleaning build artifacts..." -ForegroundColor Yellow
        Remove-Item -Recurse -Force -ErrorAction SilentlyContinue "bin", "static/css/output.css"
        Get-ChildItem -Recurse -Filter "*_templ.go" | Remove-Item -Force
    }
    default {
        Write-Host "Usage: .\build.ps1 [build | css | templ | run | clean]" -ForegroundColor Red
    }
}
