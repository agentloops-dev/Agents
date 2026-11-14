package controlplane

import (
	"strings"
	"testing"

	"github.com/agentloops-dev/agent-loops/Go/internal/controllink"
	"github.com/agentloops-dev/agent-loops/Go/internal/controlplane/store"
	"github.com/agentloops-dev/agent-loops/Go/wire"
)

// #5462: a machine's approval is kept for its person; the first of a kind and digest stays,
// and a malformed one is refused.
func TestApprovalsAreKeptForThePersonAndTheFirstStays(t *testing.T) {
	t.Parallel()
	p := startPlane(t, store.NewMemory(), controllink.NewPrivateKey())
	a, b := p.host("MAC-A").member.Host, p.host("MAC-B").member.Host
	p.waitFor(func() bool { return p.s.router.Holds(a) && p.s.router.Holds(b) }, "the machines held")
	digest := strings.Repeat("ab", 32)
	first := wire.PersonApproval{Kind: "workflow", Digest: digest, Host: a, At: 1, Key: wire.Data("k"), Sig: wire.Data("s")}
	second := first
	second.Host, second.At = b, 2
	busSay[wire.PersonApprovals](t, p, a, wire.CallApprovalsPut.Name, wire.PersonApprovalsPut{Approvals: []wire.PersonApproval{first}})
	busSay[wire.PersonApprovals](t, p, b, wire.CallApprovalsPut.Name, wire.PersonApprovalsPut{Approvals: []wire.PersonApproval{second}})
	got := busSay[wire.PersonApprovals](t, p, b, wire.CallApprovalsList.Name, wire.Empty{})
	if len(got.Approvals) != 1 || got.Approvals[0].Host != a {
		t.Fatalf("%+v", got.Approvals)
	}
	raw := []byte(`{"approvals":[{"kind":"workflow","digest":"x","host":"h","at":1,"key":"aw==","sig":"aw=="}]}`)
	if _, err := p.s.methods.HostSaid(a, wire.CallApprovalsPut.Name, raw); err == nil {
		t.Fatal("a malformed approval was kept")
	}
}
