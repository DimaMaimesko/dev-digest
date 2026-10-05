package mcpserver

import (
	"context"
	"fmt"
	"strings"
)

// The tools name things the way a person does: a repository by "owner/name",
// a pull request by its number, an agent by its name. These turn them into
// the API's IDs, and fail with a message that says what exists.

// maxListed is how many names an error lists.
const maxListed = 10

func (a api) findRepo(ctx context.Context, name string) (repoJSON, error) {
	var repos []repoJSON
	if err := a.get(ctx, "/repos", &repos); err != nil {
		return repoJSON{}, err
	}
	name = strings.TrimSpace(name)
	names := make([]string, 0, len(repos))
	for _, r := range repos {
		if strings.EqualFold(r.FullName, name) {
			return r, nil
		}
		names = append(names, r.FullName)
	}
	if len(names) == 0 {
		return repoJSON{}, fmt.Errorf("repository %q isn't imported in DevDigest, and none is: add it in the web app first", name)
	}
	return repoJSON{}, fmt.Errorf("repository %q isn't imported in DevDigest; imported: %s", name, list(names))
}

// pull is a pull request found by repository and number.
type pull struct {
	id    string
	label string // "owner/name#12", for answers and errors
}

func (a api) findPull(ctx context.Context, repoName string, number int) (pull, error) {
	if number < 1 {
		return pull{}, fmt.Errorf("pr must be a pull request number, such as 12")
	}
	repo, err := a.findRepo(ctx, repoName)
	if err != nil {
		return pull{}, err
	}
	var pulls []pullJSON
	if err := a.get(ctx, "/repos/"+repo.ID+"/pulls", &pulls); err != nil {
		return pull{}, err
	}
	label := fmt.Sprintf("%s#%d", repo.FullName, number)
	for _, p := range pulls {
		if p.Number == number {
			return pull{id: p.ID, label: label}, nil
		}
	}
	return pull{}, fmt.Errorf("%s isn't in DevDigest: open the repository in the web app to sync its pull requests (needs a GitHub token)", label)
}

func (a api) agents(ctx context.Context) ([]agentJSON, error) {
	var agents []agentJSON
	err := a.get(ctx, "/agents", &agents)
	return agents, err
}

// findAgent finds an agent by ID, or by name, ignoring case.
func findAgent(agents []agentJSON, nameOrID string) (agentJSON, error) {
	nameOrID = strings.TrimSpace(nameOrID)
	var found []agentJSON
	names := make([]string, 0, len(agents))
	for _, ag := range agents {
		if ag.ID == nameOrID {
			return ag, nil
		}
		if strings.EqualFold(ag.Name, nameOrID) {
			found = append(found, ag)
		}
		names = append(names, ag.Name)
	}
	switch len(found) {
	case 1:
		return found[0], nil
	case 0:
		return agentJSON{}, fmt.Errorf("no agent %q; agents: %s", nameOrID, list(names))
	default:
		return agentJSON{}, fmt.Errorf("%d agents are named %q: use the ID from devdigest_list_agents", len(found), nameOrID)
	}
}

// list joins names, at most maxListed of them.
func list(names []string) string {
	if len(names) > maxListed {
		return strings.Join(names[:maxListed], ", ") + fmt.Sprintf(" and %d more", len(names)-maxListed)
	}
	return strings.Join(names, ", ")
}
