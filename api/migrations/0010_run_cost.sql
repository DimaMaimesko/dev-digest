ALTER TABLE "agent_runs" ADD COLUMN "cost_usd" double precision;--> statement-breakpoint
CREATE INDEX "agent_runs_pr_id_idx" ON "agent_runs" USING btree ("pr_id");