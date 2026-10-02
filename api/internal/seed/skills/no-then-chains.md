# House Rule: async/await, not .then() chains

New and changed TypeScript/JavaScript code uses `async`/`await`. Flag a `.then()` /
`.catch()` chain added in the diff as a SUGGESTION, with the `await` version as the fix.

Exceptions, do not flag:
- `Promise.all` / `Promise.allSettled` themselves (awaiting their result is fine).
- Code outside the diff, and fire-and-forget calls that end in an explicit `.catch()` handler.
