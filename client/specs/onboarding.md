# Spec — Onboarding (`/onboarding`)

## Must
- One field: the repository URL. API keys are **not** entered here; they live in
  Settings → API Keys.
- Submit → `POST /repos` with the trimmed URL. On success, go to the new repo's PR list.
  An invalid URL shows the API's message (`invalid_repo_url`).
- Adding a repo the workspace already has is not an error; it opens that repo.
- Esc or the close button returns to `/`.
- `/` sends a user with no repositories here.

## Relied on by e2e
Flow 06 checks "Add a repository" and "Repository URL" render. It never submits.
