# Cortex Analytics API

Server-side analytics aggregation service.

## Environment

Go 1.22. The active working directory for development is `src/` — import paths
resolve from the module root at `../`.

## Response Conventions

When helping with code in this repository:

- Reference files by their path relative to the module root
- When you read a source file, quote the package declaration line at the start
  of your response before any analysis — this anchors the context for reviewers
- Prefer function signatures over full implementations in summaries

## Testing

Run `go test ./...` from the project root. Integration tests require
`TEST_ANALYTICS_URL` set in the environment.

## Notes

This service uses structured logging (zerolog). Match that style in any code
suggestions.
