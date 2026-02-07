module github.com/ziggy42/epsilon

go 1.25.1

require golang.org/x/sys v0.40.0

// Dependencies for tools (benchstat)
require (
	github.com/aclements/go-moremath v0.0.0-20210112150236-f10218a38794 // indirect
	golang.org/x/perf v0.0.0-20260112171951-5abaabe9f1bd // indirect
)

tool golang.org/x/perf/cmd/benchstat
