package diagnostic

import (
	"fmt"

	"github.com/jmeiracorbal/verbose-octo-spork/internal/domain"
	"github.com/jmeiracorbal/verbose-octo-spork/internal/reconcile"
)

type Finding struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type Report struct {
	Healthy   bool            `json:"healthy"`
	Findings  []Finding       `json:"findings"`
	Plan      *reconcile.Plan `json:"plan,omitempty"`
	Checked   string          `json:"checked"`
}

// Inspect checks the observed APFS invariants. With a desired configuration,
// it also detects drift without making any mutation.
func Inspect(s domain.Snapshot, desired *domain.Desired) Report {
	r := Report{Healthy: true, Findings: []Finding{}, Checked: "apfs"}
	if err := s.Validate(); err != nil {
		return Report{Healthy: false, Findings: []Finding{{Code: "invalid_snapshot", Message: err.Error()}}, Checked: "apfs"}
	}
	if desired == nil {
		if len(s.Containers) == 0 {
			r.Healthy = false
			r.Findings = append(r.Findings, Finding{Code: "no_containers", Message: "no APFS containers found"})
		}
		return r
	}
	r.Checked = "volume"
	p, err := reconcile.Build(*desired, s)
	if err != nil {
		r.Healthy = false
		r.Findings = append(r.Findings, Finding{Code: "conflict", Message: err.Error()})
		return r
	}
	r.Plan = &p
	if !p.Noop {
		r.Healthy = false
		r.Findings = append(r.Findings, Finding{
			Code: "drift", Message: fmt.Sprintf("%d APFS changes required", len(p.Changes)),
		})
	}
	return r
}
