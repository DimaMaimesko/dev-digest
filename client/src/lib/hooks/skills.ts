/* hooks/skills.ts — React Query hooks for the Skills Lab: skill CRUD, the body
   version history, and the agents using a skill. */
"use client";

import { useQuery, useMutation, useQueryClient, type QueryClient } from "@tanstack/react-query";
import { api } from "../api";
import type { Agent, Skill, SkillSource, SkillType, SkillVersion } from "@devdigest/shared";

export function useSkills() {
  return useQuery({
    queryKey: ["skills"],
    queryFn: () => api.get<Skill[]>("/skills"),
  });
}

export function useSkill(id: string | null | undefined) {
  return useQuery({
    queryKey: ["skill", id],
    queryFn: () => api.get<Skill>(`/skills/${id}`),
    enabled: !!id,
  });
}

/** A skill's body history, newest first. */
export function useSkillVersions(id: string | null | undefined) {
  return useQuery({
    queryKey: ["skill-versions", id],
    queryFn: () => api.get<SkillVersion[]>(`/skills/${id}/versions`),
    enabled: !!id,
  });
}

/** The agents that use a skill. */
export function useSkillAgents(id: string | null | undefined) {
  return useQuery({
    queryKey: ["skill-agents", id],
    queryFn: () => api.get<Agent[]>(`/skills/${id}/agents`),
    enabled: !!id,
  });
}

/** After a skill changed: refresh the list and its history; cache the new skill. */
function skillChanged(qc: QueryClient, skill: Skill) {
  qc.invalidateQueries({ queryKey: ["skills"] });
  qc.invalidateQueries({ queryKey: ["skill-versions", skill.id] });
  qc.setQueryData(["skill", skill.id], skill);
}

export interface CreateSkillInput {
  name: string;
  description?: string;
  type: SkillType;
  source?: SkillSource;
  body: string;
  enabled?: boolean;
  /** Describes version 1. */
  message?: string;
}

export function useCreateSkill() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (input: CreateSkillInput) => api.post<Skill>("/skills", input),
    onSuccess: (skill) => skillChanged(qc, skill),
  });
}

export interface UpdateSkillInput {
  id: string;
  patch: Partial<Pick<Skill, "name" | "description" | "type" | "body" | "enabled">> & {
    /** Describes the new version, when the body changes. */
    message?: string;
  };
}

export function useUpdateSkill() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ id, patch }: UpdateSkillInput) => api.put<Skill>(`/skills/${id}`, patch),
    onSuccess: (skill) => skillChanged(qc, skill),
  });
}

/** Makes an earlier version's body the skill's body, as a new version. */
export function useRestoreSkillVersion() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ id, version }: { id: string; version: number }) =>
      api.post<Skill>(`/skills/${id}/versions/${version}/restore`),
    onSuccess: (skill) => skillChanged(qc, skill),
  });
}

export function useDeleteSkill() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (id: string) => api.del<{ ok: boolean }>(`/skills/${id}`),
    onSuccess: (_d, id) => {
      qc.invalidateQueries({ queryKey: ["skills"] });
      // The skill's links to agents went with it.
      qc.invalidateQueries({ queryKey: ["agent-skills"] });
      qc.removeQueries({ queryKey: ["skill", id] });
    },
  });
}
