/**
 * Run cost in US dollars, as every screen shows it (PR list, run timeline,
 * trace stats). A cost is null when the provider doesn't report one; that
 * reads "—", while a free run reads "$0.00".
 */

/** Compact USD: 4 decimals under a cent ("$0.0042"), else 2 ("$0.12"). */
export function formatCost(usd: number | null | undefined): string {
  if (usd == null) return "—";
  if (usd === 0) return "$0.00";
  return `$${usd.toFixed(usd < 0.01 ? 4 : 2)}`;
}

/** The unrounded value, for a tooltip; undefined when the cost is unknown. */
export function exactCost(usd: number | null | undefined): string | undefined {
  return usd == null ? undefined : `$${usd}`;
}
