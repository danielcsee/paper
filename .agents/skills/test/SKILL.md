---
name: test
description: Use for running unit tests and checking unit test coverage.
---

<!-- Generated from .claude/skills/test/SKILL.md by scripts/claude-to-codex.py. Do not edit; edit the source and re-run. -->

## Using Testledger

Run tests and check coverage using the testledger script, found in <project_root>/testledger

Do not alter or rebuild testledger source code without express human permission. Testledger is supposed to be an enhancement tool for you - not a project to actively build.

Run testledger from its binary, testledger/bin/testledger

Read testledger's api by reading its README.md, docs/agentic-workflow.md, and docs/architecture.md.

# Writing Unit Tests

Each unit test should test only one function.

Whenever possible, structure tests like the following pseudocode example:

// TestExample is an example of desired unit test structure. Although it's written in Go,
// this design applies to all unit tests in any language. Test cases should be clearly defined
// at the beginning of the test function. The body of the test should be a simple loop
// executing the function under test against each case's inputs and outputs.
func TestExample() {
  // Define test cases first in an iterable structure
	tcs := []struct{
		input string
		expectedOutput int
		shouldErr bool
	}{
		{
			input: "valid",
			expectedOutput: 5,
			shouldErr: false,
		},
		{
			input: "invalid",
			expectedOutput: 0,
			shouldErr: true,
		},
	}
  // Iterate over cases, running the function under test against each case's inputs and outputs
	for _, tc := range tcs {
		actualOutput, err := Example(tc.input)
		if tc.shouldErr {
			assertError(t, err)
		}
		assertEquals(t, actualOutput, tc.output)
	}
}


## Running Unit Tests and Finding Coverage

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

