package controlplane

import (
	"testing"
	"time"

	"github.com/agentloops-dev/agent-loops/Go/wire"
)

func fl(v float64) *float64 { return &v }

// mach is a running machine with the project, the runtime and a fresh figure.
func mach(id string, level wire.CapacityLevel, load float64, now time.Time) autoMachine {
	return autoMachine{ID: id, Name: id, HasFolder: true, Online: true, RuntimeReady: true,
		Level: level, Load5: fl(load), Heard: now.Add(-10 * time.Second)}
}

func TestAutoPick(t *testing.T) {
	now := time.Now()
	ok, near, full := wire.CapacityLevelOk, wire.CapacityLevelNear, wire.CapacityLevelFull
	stale := mach("stale", ok, 0.0, now)
	stale.Heard = now.Add(-3 * time.Minute)
	unknown := mach("unknown", ok, 0, now)
	unknown.Level, unknown.Load5, unknown.Heard = "", nil, time.Time{}
	sleeping := mach("a", ok, 0.1, now)
	sleeping.Online = false
	noRuntime := mach("b", ok, 0.1, now)
	noRuntime.RuntimeReady = false
	noFolder := mach("c", ok, 0.1, now)
	noFolder.HasFolder = false

	cases := []struct {
		name     string
		machines []autoMachine
		last     string
		machine  string
		text     string
		none     wire.AutoMachineNone
	}{
		{"last used in ok", []autoMachine{mach("mini", ok, 0.9, now), mach("studio", ok, 0.1, now)}, "mini", "mini",
			"Auto chose mini: you last started here.", ""},
		{"least loaded in ok", []autoMachine{mach("mini", ok, 0.4, now), mach("studio", ok, 0.9, now)}, "", "mini",
			"Auto chose mini: least busy, load 0.4 per core.", ""},
		{"last used is not in the best tier", []autoMachine{mach("mini", near, 1.8, now), mach("studio", ok, 0.9, now)}, "mini", "studio",
			"Auto chose studio: least busy, load 0.9 per core.", ""},
		{"near when none is ok", []autoMachine{mach("mini", near, 1.8, now), mach("studio", full, 2.9, now)}, "", "mini",
			"Auto chose mini: the least busy, though near capacity (load 1.8 per core).", ""},
		{"full when only full", []autoMachine{mach("mini", full, 2.9, now)}, "", "mini",
			"Auto chose mini: the least busy, though at capacity (load 2.9 per core).", ""},
		{"ties go by name", []autoMachine{mach("b", ok, 0.5, now), mach("a", ok, 0.5, now)}, "", "a",
			"Auto chose a: least busy, load 0.5 per core.", ""},
		{"a stale figure counts as ok, last", []autoMachine{stale, mach("mini", ok, 0.9, now)}, "", "mini",
			"Auto chose mini: least busy, load 0.9 per core.", ""},
		{"no figure yet counts as ok, last", []autoMachine{unknown, mach("studio", ok, 0.9, now)}, "", "studio",
			"Auto chose studio: least busy, load 0.9 per core.", ""},
		{"a stale near is ok", []autoMachine{func() autoMachine { m := mach("old", near, 2, now); m.Heard = now.Add(-5 * time.Minute); return m }(),
			mach("studio", near, 1.7, now)}, "", "old", "Auto chose old: least busy.", ""},
		{"no machine has the project", []autoMachine{noFolder}, "", "", "No machine has this project. Add a folder on one.", wire.AutoMachineNoneNoFolder},
		{"no machines at all", nil, "", "", "No machine has this project. Add a folder on one.", wire.AutoMachineNoneNoFolder},
		{"no runtime", []autoMachine{noRuntime, mach("studio", ok, 0.1, now)}, "", "studio", "Auto chose studio: least busy, load 0.1 per core.", ""},
		{"nobody has the runtime", []autoMachine{noRuntime, func() autoMachine { m := noRuntime; m.ID, m.Name = "z", "z"; return m }()}, "", "",
			"No machine with this project has claude installed and signed in.", wire.AutoMachineNoneNoRuntime},
		{"every machine offline", []autoMachine{sleeping, func() autoMachine { m := sleeping; m.ID, m.Name = "d", "d"; return m }()}, "", "",
			"Every machine with this project is offline: a, d.", wire.AutoMachineNoneOffline},
	}
	for _, c := range cases {
		got := autoPick(c.machines, c.last, "claude", now)
		var machine string
		if got.Machine != nil {
			machine = string(*got.Machine)
		}
		var text string
		if got.Text != nil {
			text = *got.Text
		}
		var none wire.AutoMachineNone
		if got.None != nil {
			none = *got.None
		}
		if machine != c.machine || text != c.text || none != c.none {
			t.Errorf("%s: machine %q text %q none %q; want %q %q %q", c.name, machine, text, none, c.machine, c.text, c.none)
		}
	}
}

func TestAutoPickSingleMachine(t *testing.T) {
	now := time.Now()
	one := autoPick([]autoMachine{mach("mini", wire.CapacityLevelOk, 0.2, now), {ID: "x", Name: "x"}}, "", "claude", now)
	if one.Single == nil || !*one.Single {
		t.Fatalf("one machine with the folder: single = %v", one.Single)
	}
	two := autoPick([]autoMachine{mach("a", wire.CapacityLevelOk, 0.2, now), mach("b", wire.CapacityLevelOk, 0.2, now)}, "", "claude", now)
	if two.Single == nil || *two.Single {
		t.Fatalf("two machines: single = %v", two.Single)
	}
	// A single machine that fails the test says why.
	m := mach("mini", wire.CapacityLevelOk, 0.2, now)
	m.Online = false
	off := autoPick([]autoMachine{m}, "", "claude", now)
	if off.None == nil || *off.None != wire.AutoMachineNoneOffline || !*off.Single {
		t.Fatalf("one offline machine: %+v", off)
	}
}

func TestAutoBookLastStart(t *testing.T) {
	var b autoBook
	if b.lastStart("p", "proj") != "" {
		t.Fatal("nothing started yet")
	}
	b.noteStart("p", "proj", "mini")
	if b.lastStart("p", "proj") != "mini" || b.lastStart("q", "proj") != "" {
		t.Fatal("the last start is the person's own")
	}
}
