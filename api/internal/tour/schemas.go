package tour

import _ "embed"

// Each kind's model-answer schema, written to OpenAI's strict-mode rules
// (every property required, additionalProperties: false), the same way
// review.schema.json and intent.schema.json are.

//go:embed architecture.schema.json
var architectureSchema []byte

//go:embed critical_paths.schema.json
var criticalPathsSchema []byte

//go:embed run_locally.schema.json
var runLocallySchema []byte

//go:embed reading_path.schema.json
var readingPathSchema []byte

//go:embed first_tasks.schema.json
var firstTasksSchema []byte

// schemaNames names each kind's schema for the provider (JSONRequest.SchemaName).
var schemaNames = map[Kind]string{
	KindArchitecture:  "Architecture",
	KindCriticalPaths: "CriticalPaths",
	KindRunLocally:    "RunLocally",
	KindReadingPath:   "ReadingPath",
	KindFirstTasks:    "FirstTasks",
}

// schemaFor returns kind's JSON Schema.
func schemaFor(kind Kind) []byte {
	switch kind {
	case KindArchitecture:
		return architectureSchema
	case KindCriticalPaths:
		return criticalPathsSchema
	case KindRunLocally:
		return runLocallySchema
	case KindReadingPath:
		return readingPathSchema
	case KindFirstTasks:
		return firstTasksSchema
	default:
		return nil
	}
}
