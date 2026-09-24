package cmd

import (
	"encoding/json"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spf13/afero"

	"hop.top/git/internal/config"
	"hop.top/git/internal/docker"
	"hop.top/git/internal/git"
	"hop.top/git/internal/hop"
	"hop.top/git/internal/output"
	"hop.top/git/internal/services"
)

// targetStatusRecords is the structured result of `git hop status
// <branch>`: the branch's status record -- the same one the hub view
// lists -- plus its allocated ports and Docker Compose containers. It is
// a one-element list so every status view has the command's one shape.
func targetStatusRecords(fs afero.Fs, g git.GitInterface, d *docker.Docker, hubPath, target string) []statusRecord {
	hub, err := hop.LoadHub(fs, hubPath)
	if err != nil {
		output.Fatal("Failed to load hub: %v", err)
	}
	branch, ok := hub.Config.Branches[target]
	if !ok {
		output.Fatal("Branch %s not found in hub", target)
	}

	r := hubBranchStatusRecord(fs, g, hub, target)

	hopspacePath := hop.ResolveHopspacePath(fs, hub.Path, hub.Config.Repo.Org, hub.Config.Repo.Repo)
	if portsCfg, _ := config.NewLoader(fs).LoadPortsConfig(hopspacePath); portsCfg != nil {
		if bp, ok := portsCfg.Branches[branch.HopspaceBranch]; ok && len(bp.Ports) > 0 {
			r.Ports = bp.Ports
		}
	}

	if exists, _ := afero.Exists(fs, filepath.Join(r.Path, "docker-compose.yml")); exists {
		project := services.ComposeProjectName(hub.Config.Repo.Org, hub.Config.Repo.Repo, branch.HopspaceBranch)
		if ps, err := d.ComposePs(r.Path, project); err == nil {
			r.Services = parseComposePs(ps)
		}
	}
	return []statusRecord{r}
}

// systemStatusRecords is the structured result of `git hop status --all`:
// a status record for every worktree of every tracked repository, in
// repository then branch order. The human view's summary (paths, counts,
// disk use) is not part of it; the records are what those are counted
// from.
func systemStatusRecords(fs afero.Fs, g git.GitInterface) []statusRecord {
	st, err := loadStateOrLegacy(fs)
	if err != nil {
		output.Fatal("Failed to load state: %v", err)
	}
	listed := listRecords(fs, g, st, sortedRepoIDs(st))
	records := make([]statusRecord, 0, len(listed))
	for _, l := range listed {
		state := "Missing"
		if l.State == "active" {
			state = "Linked"
		}
		records = append(records, statusRecord{
			Branch:     l.Branch,
			Base:       l.Base,
			State:      state,
			Status:     l.Status,
			Path:       l.Path,
			Repository: l.Repository,
		})
	}
	return records
}

// parseComposePs reads `docker compose ps --format json` output, which is
// a JSON array or one JSON object per line depending on the Compose
// version, into services sorted by service then container name.
// Unparseable entries are skipped.
func parseComposePs(ps string) []statusService {
	type psEntry struct {
		Service string `json:"Service"`
		Name    string `json:"Name"`
		State   string `json:"State"`
	}
	var entries []psEntry
	trimmed := strings.TrimSpace(ps)
	if strings.HasPrefix(trimmed, "[") {
		_ = json.Unmarshal([]byte(trimmed), &entries)
	} else {
		for _, line := range strings.Split(trimmed, "\n") {
			var e psEntry
			if json.Unmarshal([]byte(strings.TrimSpace(line)), &e) == nil {
				entries = append(entries, e)
			}
		}
	}

	out := make([]statusService, 0, len(entries))
	for _, e := range entries {
		out = append(out, statusService(e))
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Service != out[j].Service {
			return out[i].Service < out[j].Service
		}
		return out[i].Name < out[j].Name
	})
	return out
}
