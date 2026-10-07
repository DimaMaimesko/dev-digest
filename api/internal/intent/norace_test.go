//go:build !race

package intent_test

// raceEnabled is false in builds without the race detector, where
// TestExtractScansLargeTextQuickly can use a tight bound.
const raceEnabled = false
