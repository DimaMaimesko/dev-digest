/* providers.tsx — client provider stack: React Query + Theme + active Repo. */
"use client";

import React from "react";
import {
  QueryClient,
  QueryClientProvider,
  QueryCache,
  MutationCache,
} from "@tanstack/react-query";
import { ThemeProvider } from "./theme";
import { RepoProvider } from "./repo-context";
import { ToastProvider, notify } from "./toast";
import { ApiError } from "./api";

function errorMessage(e: unknown): string {
  // A 422 names what's wrong in its details; the message alone is generic.
  if (e instanceof ApiError && e.code === "validation_error" && Array.isArray(e.details)) {
    const first = e.details[0] as { message?: unknown } | undefined;
    if (typeof first?.message === "string") return first.message;
  }
  if (e instanceof Error) return e.message;
  return "Something went wrong";
}

export function Providers({ children }: { children: React.ReactNode }) {
  const [qc] = React.useState(
    () =>
      new QueryClient({
        defaultOptions: {
          queries: {
            retry: 1,
            staleTime: 30_000,
            refetchOnWindowFocus: false,
          },
        },
        // Global error surfacing (errors anywhere → toast). Mutations always
        // toast (they are user actions). Queries only toast on network/5xx —
        // expected 4xx like a 404 "no tour yet" stay silent for inline empty states.
        queryCache: new QueryCache({
          onError: (err) => {
            const status = err instanceof ApiError ? err.status : 500;
            if (status === 0 || status >= 500) notify.error(errorMessage(err));
          },
        }),
        mutationCache: new MutationCache({
          onError: (err) => notify.error(errorMessage(err)),
        }),
      })
  );
  return (
    <QueryClientProvider client={qc}>
      <ThemeProvider>
        <ToastProvider>
          <RepoProvider>{children}</RepoProvider>
        </ToastProvider>
      </ThemeProvider>
    </QueryClientProvider>
  );
}
