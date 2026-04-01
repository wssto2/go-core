Please review codebase:
- find all duplicate logic
- find unused logic that should be removed or improved
- find performance bottlenecks
- find potential bugs
- find parallelism opportunities
- find partially implemented logic
- find dead code
- find logic that can be improved for better DX
- find confusing naming and recommend clean naming
- find logic that should be moved to another file or package
- recommend missing features that should be added
- find similar features that should be merged

Generate AGENT.md, ARCHITECTURE.md and PLAN.md for implmentation of all bugs and improvements you've found. Implementation will be done using GPT4.1 model which is not as capable as you are so make it ultra specific and descriptive to minimize hallucinations. For each task write exact steps than need to be done and tests that need to be done after task is completed. If you are not sure about implementation details use askQuestionsTool with clear and easy to understand questions and options.

Task 8.1 is approved. Proceed to Task 8.2.

Task 8.2 is titled: Configurable worker count in `audit/async.go`

Attached files for this task:
- go-core/audit/async.go
- go-core/audit/async_test.go

Follow AGENT.md and PLAN.md Task 8.2 exactly.
