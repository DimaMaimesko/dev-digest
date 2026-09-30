# Go idioms: what not to carry over from TypeScript

The short version is in `../CLAUDE.md`. This is the full table, with the reasoning.

| Don't carry over from TS | Do this in Go |
|---|---|
| DI container (`platform/container.ts`) | Build every dependency in `cmd/*/main.go` and pass it into constructors |
| One central interfaces file (`vendor/shared/adapters.ts`) | Define small interfaces in the package that *uses* them. Accept interfaces, return structs. |
| Service classes that can reach everything | A struct holds only the dependencies it uses. Use a plain function when there's no state. |
| `throw` and error class hierarchies | Return errors. Wrap with `fmt.Errorf("doing x: %w", err)`, check with `errors.Is` / `errors.As` |
| Cancellation flags and `checkCancelled()` callbacks | `context.Context` as the first parameter of anything that does I/O or can take long |
| `helpers.ts`, `constants.ts`, `_shared/`, `utils` | No utils, helpers or common packages. Put code next to what uses it. |
| Deep folders and `index.ts` barrel files | Flat packages named for what they provide. Avoid stutter: `review.Ground`, not `review.GroundReviewFindings` |
| Global singletons (`export const runBus`) | No package-level mutable state. Pass values in. |
| Fire-and-forget `void promise` | Every goroutine has an owner, a `context`, and a way to stop |
| `mocks.ts` / mock frameworks | Small hand-written fakes in `_test.go` files |
| Zod runtime schemas | Plain structs; validate where input enters the system |
| Drizzle ORM | SQL with `pgx` + `sqlc` |
| `undefined` / optional everywhere | Useful zero values. Use a pointer only when "absent" differs from "zero". |

## Clean architecture, kept light

Follow the **dependency rule**: domain packages such as `internal/review` never import
HTTP, SQL or SDK packages. Skip the ceremony: no package per layer, no interface for
every struct, no mapping between identical structs. Add structure when it's needed, not
in advance.

## Other rules

- Standard library first. Add a dependency only when it saves real work, and say why in
  the commit.
- Tests: standard `testing`, table-driven with `t.Run`, external `_test` packages for
  public APIs. No assertion libraries.
- Doc comments on every exported identifier, written as full sentences.
- A TS pattern with no Go equivalent is not imitated. Write it the Go way, and explain
  the difference in the commit message.
