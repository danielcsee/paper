# internal/app

The application service. Both transports call these methods directly; neither
shells out to the other. Files are grouped by concern, each paired with a
`_test.go` of the same name.

| File | Responsibility |
| --- | --- |
| `app.go` | The `App` type, its lifecycle, and configuration lookups |
| `adapter.go` | The `Discoverer` interface and the language registry |
| `inventory.go` | Source to symbol inventory, and inventory comparison |
| `coverage.go` | Which symbols count as gaps, and symbol context |
| `attribution.go` | Native coverage reports to per-symbol evidence |
| `dispositions.go` | Recorded decisions to leave a symbol untested |
| `proposals.go` | The propose / decide / implement lifecycle |
| `workflow.go` | `Status` and `NextActions` |
| `runner.go` | Executing a language's native test runner |
| `results.go` | Normalising each runner's result format |
| `jobs.go` | Asynchronous runs and reading their results |
| `selection.go` | Affected-test selection |
| `mutation.go` | Optional mutation signals |
| `support.go` | Identifiers, hashing, subprocess environment, paging |

A language adapter satisfies `Discoverer`, declared here in the consuming
package and registered in one map, so adding a language is a registry entry.

Coverage policy lives here and nowhere else: an adapter reports what a runner
observed, and this package decides whether that clears the threshold.

## Dependencies

`internal/store`, `internal/config`, `internal/model`, and the three language
adapters. `testdata/python_project` is a fixture the tests copy into a temporary
directory.
