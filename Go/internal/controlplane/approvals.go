package controlplane

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"regexp"
	"strings"

	"github.com/agentloops-dev/agent-loops/Go/internal/controlplane/store"
	"github.com/agentloops-dev/agent-loops/Go/wire"
)

// A person's approvals (#5462, epic #5281): what a machine approved, signed by that
// machine, kept at v1/approvals/<person>/<kind>-<digest>.json. The control plane keeps and
// sends them and signs nothing: a machine takes one only after it checks the signature
// against its own pinned machine lock, so this store cannot make an approval up. The first
// approval of a kind and digest is kept; a later one is dropped.

const (
	approvalsPrefix     = "v1/approvals/"
	approvalsPutMost    = 50
	approvalsKeptMost   = 2000
	approvalFieldMost   = 256
	approvalSignedBytes = 256
)

var approvalDigest = regexp.MustCompile(`^[0-9a-f]{64}$`)

func approvalsOf(person string) string {
	sum := sha256.Sum256([]byte(person))
	return approvalsPrefix + hex.EncodeToString(sum[:16]) + "/"
}

func approvalKey(person string, a wire.PersonApproval) string {
	return approvalsOf(person) + a.Kind + "-" + a.Digest + ".json"
}

func checkPersonApproval(a wire.PersonApproval) error {
	switch {
	case a.Kind != "workflow" && a.Kind != "connector":
		return fail(invalidParams, "An approval is of a workflow or a connector.")
	case !approvalDigest.MatchString(a.Digest):
		return fail(invalidParams, "An approval names a file by its SHA-256 digest.")
	case a.Host == "" || len(a.Host) > approvalFieldMost || len(deref(a.HostName)) > approvalFieldMost || len(deref(a.Name)) > approvalFieldMost:
		return fail(invalidParams, "An approval names the machine, and a name is at most %d characters.", approvalFieldMost)
	case len(a.Key) == 0 || len(a.Key) > approvalSignedBytes || len(a.Sig) == 0 || len(a.Sig) > approvalSignedBytes:
		return fail(invalidParams, "An approval is signed.")
	}
	return nil
}

// listApprovals is approvals/list: the person's, oldest key first.
func (s *Service) listApprovals(ctx context.Context, person string) (wire.PersonApprovals, error) {
	out := wire.PersonApprovals{Approvals: []wire.PersonApproval{}}
	keys, err := s.cfg.Store.List(ctx, approvalsOf(person))
	if err != nil {
		return out, fail(storeUnavailable, "The approvals could not be read: %v", err)
	}
	for _, k := range keys[:min(len(keys), approvalsKeptMost)] {
		o, err := s.cfg.Store.Get(ctx, k.Key)
		if err != nil {
			return out, fail(storeUnavailable, "The approvals could not be read: %v", err)
		}
		var a wire.PersonApproval
		if o == nil || json.Unmarshal(o.Data, &a) != nil || checkPersonApproval(a) != nil || !strings.HasSuffix(k.Key, a.Kind+"-"+a.Digest+".json") {
			continue
		}
		out.Approvals = append(out.Approvals, a)
	}
	return out, nil
}

// putApprovals is approvals/put: each new kind and digest is kept, for the machine's person.
func (s *Service) putApprovals(ctx context.Context, host string, p wire.PersonApprovalsPut) (wire.PersonApprovals, error) {
	if s.records.Host(host) == nil {
		return wire.PersonApprovals{}, fail(noSuchHost, "No machine is called %s.", host)
	}
	if len(p.Approvals) > approvalsPutMost {
		return wire.PersonApprovals{}, fail(invalidParams, "Send at most %d approvals at a time.", approvalsPutMost)
	}
	person := s.ownerOf(host)
	for _, a := range p.Approvals {
		if err := checkPersonApproval(a); err != nil {
			return wire.PersonApprovals{}, err
		}
	}
	keys, err := s.cfg.Store.List(ctx, approvalsOf(person))
	if err != nil {
		return wire.PersonApprovals{}, fail(storeUnavailable, "The approvals could not be written: %v", err)
	}
	room := approvalsKeptMost - len(keys)
	for _, a := range p.Approvals {
		if room <= 0 {
			break
		}
		data, _ := json.Marshal(a)
		_, err := s.cfg.Store.Put(ctx, approvalKey(person, a), data, store.Absent)
		switch {
		case err == nil:
			room--
		case store.IsConflict(err):
		default:
			return wire.PersonApprovals{}, fail(storeUnavailable, "The approvals could not be written: %v", err)
		}
	}
	return s.listApprovals(ctx, person)
}
