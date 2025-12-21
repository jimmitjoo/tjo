# Contributing to Tjo

Thank you for your interest in contributing to Tjo! This guide will help you get started.

## Code of Conduct

Be respectful and inclusive. We're all here to learn and build great software together. See our [CODE_OF_CONDUCT.md](CODE_OF_CONDUCT.md) for details.

## Getting Started

### Prerequisites

- Go 1.23+
- Docker (optional, for database/cache tests)
- PostgreSQL (optional, for database tests)
- Redis (optional, for cache tests)

### Development Setup

1. Fork the repository on GitHub

2. Clone your fork:
   ```bash
   git clone https://github.com/YOUR_USERNAME/tjo.git
   cd tjo
   ```

3. Add upstream remote:
   ```bash
   git remote add upstream https://github.com/jimmitjoo/tjo.git
   ```

4. Install dependencies:
   ```bash
   go mod download
   ```

5. Run tests:
   ```bash
   make test
   ```

### Running Tests

```bash
# Run all tests with colorful output
make test

# Run tests without colors
make test-simple

# Run tests with coverage
make cover

# Run specific package tests
go test ./cache/...

# Skip Docker-dependent tests
go test -short ./...
```

## Making Changes

### Branch Naming

Create a branch from `main` with a descriptive name:

- `feature/description` - New features
- `fix/description` - Bug fixes
- `docs/description` - Documentation updates
- `refactor/description` - Code refactoring
- `test/description` - Test improvements

Example:
```bash
git checkout -b feature/add-redis-cluster-support
```

### Commit Messages

Use clear, descriptive commit messages:

```
Add hot-reload support for development server

Fix cache expiration bug in Redis adapter

Update README with MCP instructions

Refactor session handling for clarity
```

Keep commits focused - one logical change per commit.

### Code Style

- Run `go fmt ./...` before committing
- Run `go vet ./...` to catch common issues
- Follow Go best practices and idioms
- Add tests for new features
- Update documentation as needed
- Keep functions small and focused
- Use meaningful variable names

### Testing Requirements

- All new features must include tests
- Bug fixes should include a regression test
- Maintain or improve code coverage
- Tests should be deterministic (no flaky tests)

## Pull Request Process

### Before Submitting

1. Sync with upstream:
   ```bash
   git fetch upstream
   git rebase upstream/main
   ```

2. Run all tests:
   ```bash
   make test
   ```

3. Run linting:
   ```bash
   go fmt ./...
   go vet ./...
   ```

### Submitting

1. Push to your fork:
   ```bash
   git push origin feature/your-feature
   ```

2. Open a Pull Request on GitHub

3. Fill in the PR template with:
   - Clear description of changes
   - Type of change (bug fix, feature, etc.)
   - Testing performed
   - Any breaking changes

### PR Checklist

- [ ] Tests pass locally (`make test`)
- [ ] Code is formatted (`go fmt ./...`)
- [ ] No linting errors (`go vet ./...`)
- [ ] New tests added for new features
- [ ] Documentation updated if needed
- [ ] Commit messages are clear
- [ ] PR description explains the changes

### Review Process

1. A maintainer will review your PR within a few days
2. Address any feedback or requested changes
3. Once approved, your PR will be merged
4. Your contribution will be included in the next release

## Good First Issues

New to Tjo? Look for issues labeled [`good first issue`](https://github.com/jimmitjoo/tjo/labels/good%20first%20issue) - these are great starting points for new contributors.

## Development Tips

### Project Structure

```
tjo/
├── api/           # REST API utilities
├── cache/         # Cache implementations (Redis, Badger)
├── cmd/cli/       # CLI tool (gq command)
├── config/        # Configuration handling
├── database/      # Database utilities
├── email/         # Email providers
├── filesystems/   # File storage (S3, MinIO)
├── jobs/          # Background job processing
├── logging/       # Structured logging
├── render/        # Template rendering
├── security/      # Security middleware
├── session/       # Session management
├── sms/           # SMS providers
├── urlsigner/     # URL signing
└── websocket/     # WebSocket support
```

### Running the CLI Locally

```bash
# Build the CLI
make build

# Run from dist/
./dist/gq help

# Or install globally
go install ./cmd/cli
```

### Testing with Docker

For tests that require external services:

```bash
# Start test dependencies
docker-compose -f docker-compose.test.yml up -d

# Run all tests
make test

# Stop dependencies
docker-compose -f docker-compose.test.yml down
```

## Questions?

- Open a [GitHub Discussion](https://github.com/jimmitjoo/tjo/discussions) for questions
- Open an [Issue](https://github.com/jimmitjoo/tjo/issues) for bugs or feature requests
- Check existing issues before creating new ones

## License

By contributing, you agree that your contributions will be licensed under the [MIT License](LICENSE).
