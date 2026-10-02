# Spec — Skills (`/skills`, `/skills/:id`)

A skill is a named rule, rubric or convention. Agents link skills; a review puts each enabled,
linked skill into the prompt as `## <name>` then its body, in the agent's order.

## Frame (both routes)
- Left: one card per skill (type badge, source, how many agents use it), a search over name,
  description and type, and **Add Skill → Create from scratch**. The sidebar entry is Skills
  (`g s`).
- A card's toggle turns the skill on or off (`PUT /skills/:id` with `{enabled}`) without a new
  version. A disabled skill stays linked but is left out of every prompt.
- Deleting asks for confirmation, naming how many agents lose the skill. Deleting the open skill
  goes back to `/skills`.
- **Create**: name (unique; a clash shows the API's message), description, type (rubric ·
  convention · security · custom; default custom), body. On success, opens the new skill.
- Picking a card keeps the current `?tab=`.

## Editor (`/skills/:id`, tab in `?tab=`)
- **Config**: name, description, type, enabled, source (read only), body with a rough token
  count, and the agents using it (links). A changed body is marked unsaved and gets an optional
  version note; only a new body makes a new version.
- **Preview**: the saved body rendered as the prompt holds it, under `## <name>`.
- **Versions**: newest first; the current one is marked. **Restore** saves that version's body
  as the next version. **Show body** expands a version's text.
- An unknown or deleted skill shows "Skill not found".
