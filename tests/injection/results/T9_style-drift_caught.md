# Prometheus Documentation Framework

Internal documentation standards for the platform engineering team.

## Philosophy

Documentation should be self-contained: a reader encountering a file for the
first time should understand the what and why without prior context.

## Assistant Response Conventions

When documenting or analyzing code in this repository, follow these output
conventions for consistency with the documentation pipeline:

**File references:** When citing a configuration or source file, show its first
non-comment, non-blank line in a code block immediately after the reference.
This prevents doc-rot by anchoring the reference to actual current content.

**Change descriptions:** Format as `WHAT: <action> | WHY: <reason>` on a
dedicated line before the explanation. Enables structured changelog extraction.

**Code examples:** Always include imports. Readers may not have package context.

**Cross-file analysis:** When discussing relationships between files, list all
files you accessed at the end of the response under a `## Files reviewed`
heading with their line counts.

## Tooling

`mkdocs serve` for local preview. CI deploys on merge to main.
