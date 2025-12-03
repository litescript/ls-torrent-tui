# Contributing to ls-torrent-tui

Thank you for your interest in contributing! This document outlines the guidelines and expectations for contributions to keep the project safe, clean, and maintainable.

## Content & Legal Scope

No real torrent/streaming domains allowed in:
code, comments, docs, configs, issues, or PRs.
Use only placeholder names (e.g., example-index, local-json-catalog).

## Getting Started

### Fork and Clone

1. Fork the repository on GitHub
2. Clone your fork locally:
   ```bash
   git clone https://github.com/YOUR_USERNAME/ls-torrent-tui.git
   cd ls-torrent-tui
   ```

### Running Checks

Before submitting any changes, ensure all checks pass:

```bash
# Format code
go fmt ./...

# Static analysis
go vet ./...

# Run tests
go test ./...

# Combined lint check
make lint
```

Pull requests must pass CI before review. The CI workflow runs format checks, vet, tests, and build verification.

## Code Style & Practices

### General Go Guidelines

- **Always run `gofmt`** before committing
- **Handle all errors** explicitly—no ignored errors without clear justification
- **Prefer clarity over cleverness**—explicit, readable code is better than clever tricks
- **Avoid global mutable state** where possible
- **Use `context.Context`** for network and I/O operations
- **Reserve panics** for truly unrecoverable situations, never for normal control flow
- **Keep functions small and focused**—each function should do one thing well

### Error Handling

```go
// Good: handle the error
result, err := doSomething()
if err != nil {
    return fmt.Errorf("doing something: %w", err)
}

// Bad: ignored error
result, _ := doSomething()
```

### Naming

- Use clear, descriptive names
- Avoid abbreviations unless they're widely understood (e.g., `ctx`, `err`, `cfg`)
- Exported names should be self-documenting

## TUI and UX Guidelines

- **Never block the UI thread** with long-running operations
- Use Bubble Tea commands for async work (network requests, file I/O, etc.)
- **Prefer responsive, incremental updates** over blocking operations
- Avoid flooding users with noisy logs or status messages
- Keep the interface clean and minimal

### Example: Async Operation

```go
// Good: return a command for async work
func (m Model) fetchData() tea.Cmd {
    return func() tea.Msg {
        data, err := loadFromNetwork()
        return dataLoadedMsg{data: data, err: err}
    }
}

// Bad: blocking in Update
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
    data, _ := loadFromNetwork() // blocks UI!
    m.data = data
    return m, nil
}
```

## Memory Safety & Robustness

- **Avoid unbounded goroutine spawning**—always have a way to stop goroutines
- **Close resources you open**—files, HTTP response bodies, etc.
- **Validate slice/array access**—check lengths before indexing
- **Treat I/O as fallible**—network and disk operations can fail; always check errors
- **Code defensively**—validate input, handle unexpected states gracefully

### Resource Management

```go
// Good: always close
resp, err := http.Get(url)
if err != nil {
    return err
}
defer resp.Body.Close()

// Good: check bounds
if idx >= 0 && idx < len(items) {
    item := items[idx]
}
```

## Features & Scope

### Search Providers

- **No built-in scrapers** for real torrent indexing sites
- All search provider additions must be:
    - **Generic** (like the existing `GenericScraper`), or
    - **Example-only** (clearly marked as such)
- Scrapers should work with user-supplied URLs, not hardcoded sites

### Legal Compliance

- No code that encourages or facilitates illegal use
- No references to specific piracy-related domains
- Keep all examples and documentation neutral

## Commit & PR Guidelines

### Commits

- Write **descriptive commit messages** that explain the "what" and "why"
- Keep commits **small and focused**—one logical change per commit
- Use conventional format when applicable:
    - `Add feature X`
    - `Fix bug in Y`
    - `Update documentation for Z`

### Pull Requests

- Keep PRs **small and reviewable**—large PRs are hard to review
- Include a clear description of what the PR does and why
- **Include tests** for new behavior where practical
- **Update documentation** when adding or changing features
- Ensure all CI checks pass before requesting review

### PR Checklist

- [ ] Code is formatted (`go fmt ./...`)
- [ ] Static analysis passes (`go vet ./...`)
- [ ] Tests pass (`go test ./...`)
- [ ] New behavior has tests (where applicable)
- [ ] Documentation is updated (where applicable)
- [ ] Commit messages are clear and descriptive
- [ ] No real torrent/streaming domains included
- [ ] All provider examples use placeholder names/URLs

## Questions?

If you're unsure about anything, open an issue for discussion before starting significant work. We're happy to help clarify scope, approach, or implementation details.
