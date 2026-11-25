// Copyright The gapdrill Authors
// SPDX-License-Identifier: Apache-2.0

// Package report turns a drill's evidence into findings per operation and
// renders them as Markdown and JSON.
package report

import (
	"encoding/json"
	"time"

	"github.com/gapdrill/gapdrill/internal/evidence"
)

// SchemaVersion identifies the JSON report format.
const SchemaVersion = "gapdrill/report/v1alpha1"

// Verdict is the drill's overall result.
type Verdict string

// Verdicts.
const (
	Passed Verdict = "passed"
	Failed Verdict = "failed"
	// Errored means the drill itself couldn't run, so nothing is known about
	// the release.
	Errored Verdict = "error"
)

// Status is one operation's result.
type Status string

// Operation statuses.
const (
	StatusPassed Status = "passed"
	StatusFailed Status = "failed"
	// StatusSkipped means there was nothing to run, such as tests for a chart
	// that has none.
	StatusSkipped Status = "skipped"
)

// Report is the result of a drill.
type Report struct {
	SchemaVersion   string      `json:"schemaVersion"`
	Drill           string      `json:"drill"`
	GapdrillVersion string      `json:"gapdrillVersion"`
	StartedAt       time.Time   `json:"startedAt"`
	FinishedAt      time.Time   `json:"finishedAt"`
	Verdict         Verdict     `json:"verdict"`
	Error           string      `json:"error,omitempty"`
	Environment     Environment `json:"environment"`
	Bundle          Bundle      `json:"bundle"`
	Isolation       Isolation   `json:"isolation"`
	Operations      []Operation `json:"operations"`
	// Unattributed holds findings outside every operation's window.
	Unattributed []Finding `json:"unattributedFindings,omitempty"`
}

// Environment describes where the drill ran.
type Environment struct {
	NodeImage string `json:"nodeImage"`
	Platform  string `json:"platform"`
	Helm      string `json:"helm,omitempty"`
}

// Bundle lists the images the offline site had.
type Bundle struct {
	Images []string `json:"images"`
	// NotPulled lists the bundle images no node pulled during the drill.
	// Operations the drill didn't run may still need them.
	NotPulled []string `json:"notPulled,omitempty"`
}

// Isolation records how the cluster was cut off and whether that was
// verified.
type Isolation struct {
	// Verified is true when the probe URL was unreachable from every node
	// after isolation.
	Verified bool   `json:"verified"`
	ProbeURL string `json:"probeURL"`
	// ReachableBefore is true when the probe URL answered before isolation,
	// which proves the probe could have succeeded.
	ReachableBefore bool     `json:"reachableBefore"`
	RejectedPackets int64    `json:"rejectedPackets"`
	Warnings        []string `json:"warnings,omitempty"`
}

// Operation is one operation's result.
type Operation struct {
	Name            string    `json:"name"`
	Type            string    `json:"type"`
	Status          Status    `json:"status"`
	StartedAt       time.Time `json:"startedAt"`
	DurationSeconds float64   `json:"durationSeconds"`
	// Summary is a one-line diagnosis for failures, or a note about warnings.
	Summary  string    `json:"summary,omitempty"`
	Error    string    `json:"error,omitempty"`
	Output   string    `json:"output,omitempty"`
	Findings []Finding `json:"findings,omitempty"`
}

// FindingKind classifies a finding.
type FindingKind string

// Finding kinds, strongest cause first.
const (
	MissingImage    FindingKind = "missing-image"
	NetworkLookup   FindingKind = "network-lookup"
	ImagePullFailed FindingKind = "image-pull-failed"
	ContainerFailed FindingKind = "container-failed"
)

var precedence = map[FindingKind]int{MissingImage: 0, NetworkLookup: 1, ImagePullFailed: 2, ContainerFailed: 3}

// Finding is something the drill saw that explains, or may explain, a
// failure.
type Finding struct {
	Kind      FindingKind   `json:"kind"`
	Message   string        `json:"message"`
	Image     string        `json:"image,omitempty"`
	Host      string        `json:"host,omitempty"`
	Container string        `json:"container,omitempty"`
	ExitCode  int32         `json:"exitCode,omitempty"`
	Pod       *evidence.Pod `json:"pod,omitempty"`
	FirstSeen time.Time     `json:"firstSeen"`
	// Count is how many times the evidence was seen (for lookups, the number
	// of queries).
	Count int      `json:"count"`
	Logs  []string `json:"logs,omitempty"`
}

// JSON renders the report as indented JSON.
func (r *Report) JSON() ([]byte, error) {
	b, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(b, '\n'), nil
}

// RanCount returns the number of operations that ran, that is, weren't
// skipped.
func (r *Report) RanCount() int {
	n := 0
	for _, op := range r.Operations {
		if op.Status != StatusSkipped {
			n++
		}
	}
	return n
}

// FailedCount returns the number of failed operations.
func (r *Report) FailedCount() int {
	n := 0
	for _, op := range r.Operations {
		if op.Status == StatusFailed {
			n++
		}
	}
	return n
}
