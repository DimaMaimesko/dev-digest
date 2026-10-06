//go:build race

package intent_test

// raceEnabled is true under `go test -race`, where the race detector's
// instrumentation slows everything down enough that the adversarial cases
// in TestExtractScansLargeTextQuickly need a much more generous bound (or a
// skip) to stay reliable on slower CI hardware.
const raceEnabled = true
