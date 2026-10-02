ALTER TABLE "skill_versions" ADD COLUMN "message" text;--> statement-breakpoint
CREATE UNIQUE INDEX "skills_ws_name_uq" ON "skills" USING btree ("workspace_id","name");--> statement-breakpoint
CREATE INDEX "agent_skills_skill_id_idx" ON "agent_skills" USING btree ("skill_id");
