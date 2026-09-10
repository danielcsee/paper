---
name: test
description: Use for running unit tests and checking unit test coverage.
---

## Using Testledger

Run tests and check coverage using the testledger script, found in <project_root>/testledger

Do not alter or rebuild testledger source code without express human permission. Testledger is supposed to be an enhancement tool for you - not a project to actively build.

Run testledger from its binary, testledger/bin/testledger

## Testing and Coverage

Using testledger, do these steps in order:
(1) Check code coverage:
  - Identify the list of uncovered functions NOT marked as purposefully skipped.
  - From those, identify the functions most in need of testing.
  - Ssk for human approval to add tests for those functions.
(2) Add tests:
  - Add tests for all functions approved in step 1.
  - Record your additions in testledger
(3) Run tests:
  - Run tests using testledger's api
  - Use testledger's api to check which tests failed
  - Investigate source code failures and apply targeted fixes
  - If a failure can be traced to an untested function, add a new test at your discretion, but ask for human approval as in step 1.
  - Continue using testledger to re-run tests, and applying fixes, until all tests pass.
