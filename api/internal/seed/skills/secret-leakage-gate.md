# Secret Leakage Gate

Treat any credential that reaches the repository or the client as CRITICAL.

Flag:
- Live keys and tokens: `sk_live_`, `rk_live_`, `ghp_`, `github_pat_`, `xox[bp]-`, `AKIA…`,
  private key blocks (`-----BEGIN … PRIVATE KEY-----`).
- Server-only secrets exposed to the browser: a `NEXT_PUBLIC_` or `VITE_` variable holding a
  secret, or a Supabase `service_role` key used in client code.
- Secrets written to logs, error messages, or URLs.

Do not flag obvious placeholders (`sk_test_xxx`, `<your-key>`, `changeme`) or values read
from the environment. The fix is always: move it to a secret store and rotate the key.
