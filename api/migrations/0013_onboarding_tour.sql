ALTER TABLE "onboarding" ADD COLUMN "commit_sha" text;--> statement-breakpoint
ALTER TABLE "onboarding" ADD COLUMN "files_indexed" integer DEFAULT 0 NOT NULL;--> statement-breakpoint
ALTER TABLE "onboarding" ADD COLUMN "index_status" text;--> statement-breakpoint
ALTER TABLE "onboarding" ADD COLUMN "code_index_empty" boolean DEFAULT false NOT NULL;--> statement-breakpoint
ALTER TABLE "onboarding" ADD COLUMN "provider" text;--> statement-breakpoint
ALTER TABLE "onboarding" ADD COLUMN "model" text;--> statement-breakpoint
ALTER TABLE "onboarding" ADD COLUMN "tokens_in" integer;--> statement-breakpoint
ALTER TABLE "onboarding" ADD COLUMN "tokens_out" integer;--> statement-breakpoint
ALTER TABLE "onboarding" ADD COLUMN "cost_usd" double precision;--> statement-breakpoint
CREATE TABLE "onboarding_runs" (
	"id" uuid PRIMARY KEY DEFAULT gen_random_uuid() NOT NULL,
	"repo_id" uuid NOT NULL,
	"workspace_id" uuid NOT NULL,
	"status" text DEFAULT 'queued' NOT NULL,
	"error" text,
	"commit_sha" text NOT NULL,
	"files_indexed" integer NOT NULL,
	"index_status" text NOT NULL,
	"code_index_empty" boolean NOT NULL,
	"provider" text NOT NULL,
	"model" text NOT NULL,
	"sections" jsonb NOT NULL,
	"tokens_in" integer,
	"tokens_out" integer,
	"cost_usd" double precision,
	"created_at" timestamp with time zone DEFAULT now() NOT NULL,
	"started_at" timestamp with time zone,
	"finished_at" timestamp with time zone
);
--> statement-breakpoint
ALTER TABLE "onboarding_runs" ADD CONSTRAINT "onboarding_runs_repo_id_repos_id_fk" FOREIGN KEY ("repo_id") REFERENCES "public"."repos"("id") ON DELETE cascade ON UPDATE no action;--> statement-breakpoint
CREATE UNIQUE INDEX "onboarding_runs_one_live_idx" ON "onboarding_runs" USING btree ("repo_id") WHERE status IN ('queued', 'running');--> statement-breakpoint
CREATE INDEX "onboarding_runs_repo_created_idx" ON "onboarding_runs" USING btree ("repo_id", "created_at");
