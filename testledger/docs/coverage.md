# Coverage policy

Coverage here is **execution evidence, not proof that assertions are
meaningful**. A test that calls a function and asserts nothing still covers it.
Mutation signals exist to push against that, and stay deliberately separate from
the pass/fail record.

## How a function becomes covered

A passing test is assigned to a function version when that test's dynamic
coverage context executed at least one executable line in the function, and
reached `minimum_line_percent` of its executable lines.

Two consequences are worth understanding before you write tests against it.

### The threshold is per test, not per suite

Coverage is **not** unioned across the suite. One individual test must reach the
threshold on its own. Idiomatic `@pytest.mark.parametrize` splits cases across
test IDs, so each proves one input and none proves the function:

```
normalise_identifier   12 parametrized cases, exhaustive
                       best single test: 69%  ->  still a gap
```

The fix is not to abandon parametrization. Pair granular parametrized cases,
which isolate a failure to one input, with one "contract" test that walks the
whole documented behaviour in a single body. The contract test earns the
attribution; the parametrized ones keep diagnosis sharp.

### Definition-time lines do not count

Everything above a function's first body statement — decorators, the `def` line,
a signature split across lines, default and annotation expressions — executes at
import time, in coverage.py's empty context. It can never be attributed to a
test, so it is excluded from the denominator.

Counting it once meant a one-line `@property` could reach at most 50% and was
permanently below any useful threshold, while an identical module-level function
passed.

A symbol left with no executable body at all — an overload stub, a docstring-only
body — is excluded rather than reported as a gap nobody can close.

## Changed functions

Semantic hashes ignore formatting and comments, so reformatting does not
invalidate coverage. Changing a body does: a changed function must be observed
again before coverage of its previous version counts, and an unfulfilled
proposal targeting it becomes `obsolete`.

## Dispositions

A skip records a human's decision to leave something untested, with a mandatory
reason. Prefer version-scoped, which is the default: it lapses when the function
changes, so a rewrite resurfaces as a gap. `--durable` survives edits and should
be explicitly requested.

A disposition is evidence of a choice, never of coverage. It resolves an
intended test link without ever marking it verified.
