package config

import "testing"

func TestHubBranchTask_RoundTrips(t *testing.T) {
	fs := seedHopJSON(t, cloneShapedHopJSON)
	loader, writer := NewLoader(fs), NewWriter(fs)

	cfg, err := loader.LoadHubConfig("/hub")
	if err != nil {
		t.Fatal(err)
	}
	cfg.Branches["feat/x"] = HubBranch{Path: "/hub/hops/feat/x", HopspaceBranch: "feat/x", Task: "T-1"}
	if err := writer.WriteHubConfig("/hub", cfg); err != nil {
		t.Fatal(err)
	}

	if got := obj(t, readHopJSON(t, fs), "branches", "feat/x")["task"]; got != "T-1" {
		t.Errorf("branches.feat/x.task on disk = %v, want T-1", got)
	}
	reloaded, err := loader.LoadHubConfig("/hub")
	if err != nil {
		t.Fatal(err)
	}
	if got := reloaded.Branches["feat/x"].Task; got != "T-1" {
		t.Errorf("reloaded Task = %q, want T-1", got)
	}
	if _, ok := obj(t, readHopJSON(t, fs), "branches", "main")["task"]; ok {
		t.Error("branches.main gained a task key; an unset task must be omitted")
	}
}

// Local mode shares one hop.json between hub and hopspace; a hopspace
// write (which does not model task) must not drop it.
func TestHubBranchTask_SurvivesHopspaceWrite(t *testing.T) {
	fs := seedHopJSON(t, cloneShapedHopJSON)
	loader, writer := NewLoader(fs), NewWriter(fs)

	hub, err := loader.LoadHubConfig("/hub")
	if err != nil {
		t.Fatal(err)
	}
	b := hub.Branches["main"]
	b.Task = "T-2"
	hub.Branches["main"] = b
	if err := writer.WriteHubConfig("/hub", hub); err != nil {
		t.Fatal(err)
	}

	hs, err := loader.LoadHopspaceConfig("/hub")
	if err != nil {
		t.Fatal(err)
	}
	if err := writer.WriteHopspaceConfig("/hub", hs); err != nil {
		t.Fatal(err)
	}

	if got := obj(t, readHopJSON(t, fs), "branches", "main")["task"]; got != "T-2" {
		t.Errorf("branches.main.task after hopspace write = %v, want T-2", got)
	}
}

// Clearing a modeled task must remove it from disk, not resurrect it
// from the previous file contents.
func TestHubBranchTask_ClearedStaysCleared(t *testing.T) {
	fs := seedHopJSON(t, cloneShapedHopJSON)
	loader, writer := NewLoader(fs), NewWriter(fs)

	cfg, _ := loader.LoadHubConfig("/hub")
	b := cfg.Branches["main"]
	b.Task = "T-3"
	cfg.Branches["main"] = b
	if err := writer.WriteHubConfig("/hub", cfg); err != nil {
		t.Fatal(err)
	}
	b.Task = ""
	cfg.Branches["main"] = b
	if err := writer.WriteHubConfig("/hub", cfg); err != nil {
		t.Fatal(err)
	}
	if _, ok := obj(t, readHopJSON(t, fs), "branches", "main")["task"]; ok {
		t.Error("cleared task still on disk")
	}
}
