# internal/pathmatch

The path-glob subset every adapter shares, so `include` and `exclude` patterns
mean exactly the same thing in Python, Go and TypeScript configuration.

`filepath.Match` is not enough: it has no `**`, so a pattern like
`api/**/*.py` cannot be expressed. This package compiles a pattern to a regular
expression with the small set of rules that matter:

- `**/` crosses directory boundaries, and matches zero of them as well as many.
- `*` and `?` stay within one path segment.
- Paths and patterns always use slash separators, on every platform.

Keeping this in one package rather than per adapter is the point: a pattern that
excludes a vendored directory from Go discovery must exclude the same directory
from TypeScript discovery, or a project's coverage denominator quietly depends
on which language it is written in.

## Dependencies

Standard library only (`regexp`, `strings`).
