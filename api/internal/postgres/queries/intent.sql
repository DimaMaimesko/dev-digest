-- name: GetPullIntent :one
-- The PR's most recently derived intent, or no row when none exists.
SELECT * FROM pr_intent WHERE pr_id = $1;

-- name: UpsertPullIntent :exec
-- Replaces the PR's stored intent with a newly derived one.
INSERT INTO pr_intent (pr_id, intent, in_scope, out_of_scope, confidence, sources, unresolved,
                       head_sha, fingerprint, provider, model, tokens_in, tokens_out, cost_usd,
                       derived_at)
VALUES (@pr_id, @intent, @in_scope, @out_of_scope, @confidence, @sources, @unresolved,
        @head_sha, @fingerprint, @provider, @model, @tokens_in, @tokens_out, @cost_usd, now())
ON CONFLICT (pr_id) DO UPDATE SET
    intent = EXCLUDED.intent,
    in_scope = EXCLUDED.in_scope,
    out_of_scope = EXCLUDED.out_of_scope,
    confidence = EXCLUDED.confidence,
    sources = EXCLUDED.sources,
    unresolved = EXCLUDED.unresolved,
    head_sha = EXCLUDED.head_sha,
    fingerprint = EXCLUDED.fingerprint,
    provider = EXCLUDED.provider,
    model = EXCLUDED.model,
    tokens_in = EXCLUDED.tokens_in,
    tokens_out = EXCLUDED.tokens_out,
    cost_usd = EXCLUDED.cost_usd,
    derived_at = now();
