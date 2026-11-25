// Copyright The gapdrill Authors
// SPDX-License-Identifier: Apache-2.0

package report

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/gapdrill/gapdrill/internal/dnsmon"
	"github.com/gapdrill/gapdrill/internal/evidence"
	"github.com/gapdrill/gapdrill/internal/mirror"
)

// OperationRun is what the runner recorded about one operation.
type OperationRun struct {
	Name, Type string
	Start, End time.Time
	Settle     time.Duration
	Err        error
	Output     string
	// Skipped is why the operation didn't run, when it didn't.
	Skipped string
}

// Input is everything a report is built from.
type Input struct {
	Drill, Version    string
	Started, Finished time.Time
	Environment       Environment
	Isolation         Isolation
	// Bundle holds canonical image references (see mirror.Image.String).
	Bundle     []string
	Operations []OperationRun
	// Evidence should have DNS queries already attributed to pods.
	Evidence      []evidence.Item
	SearchDomains []string
	// HarnessError is set when the drill itself couldn't run.
	HarnessError error
	// Pulled lists the image references nodes pulled from the bundle
	// registry during the drill.
	Pulled []string
	// BundleDigests maps bundle images to their digests in the bundle
	// registry, so a pull by digest matches an image listed by tag.
	BundleDigests map[string]string
	// LearnedPulled lists the images the nodes pulled during a learn run.
	LearnedPulled []string
}

// clockSlack allows for small differences between the host's clock, which
// times operations, and the node's, which timestamps some evidence.
const clockSlack = time.Second

// Build attributes evidence to operations and diagnoses each one.
func Build(in Input) *Report {
	r := &Report{
		SchemaVersion:   SchemaVersion,
		Drill:           in.Drill,
		GapdrillVersion: in.Version,
		StartedAt:       in.Started,
		FinishedAt:      in.Finished,
		Environment:     in.Environment,
		Bundle:          Bundle{Images: in.Bundle},
		Isolation:       in.Isolation,
		Operations:      []Operation{},
	}

	for _, run := range in.Operations {
		op := Operation{
			Name:            run.Name,
			Type:            run.Type,
			Status:          StatusPassed,
			StartedAt:       run.Start,
			DurationSeconds: run.End.Sub(run.Start).Round(time.Second).Seconds(),
			Output:          run.Output,
		}
		switch {
		case run.Skipped != "":
			op.Status = StatusSkipped
			op.Summary = "Skipped: " + run.Skipped + "."
		case run.Err != nil:
			op.Status = StatusFailed
			op.Error = run.Err.Error()
		}
		r.Operations = append(r.Operations, op)
	}

	// Only operations that ran have windows: nothing can be blamed on one that
	// didn't.
	var ran []OperationRun
	var index []int
	for i, run := range in.Operations {
		if run.Skipped == "" {
			ran = append(ran, run)
			index = append(index, i)
		}
	}
	for _, f := range findings(in) {
		f.Fix = fixFor(f)
		if w := window(ran, f.FirstSeen); w >= 0 {
			i := index[w]
			r.Operations[i].Findings = append(r.Operations[i].Findings, f)
		} else {
			r.Unattributed = append(r.Unattributed, f)
		}
	}
	for i := range r.Operations {
		if r.Operations[i].Status == StatusSkipped {
			continue
		}
		sortFindings(r.Operations[i].Findings)
		r.Operations[i].Summary = summarize(r.Operations[i])
	}
	sortFindings(r.Unattributed)

	// Without operations that ran, every image would look unused.
	if in.HarnessError == nil && r.RanCount() > 0 {
		r.Bundle.NotPulled = notPulled(in.Bundle, in.BundleDigests, in.Pulled)
		if in.Isolation.Learn {
			r.Learned = learnedImages(in.Bundle, in.BundleDigests, in.LearnedPulled)
		}
	}

	switch {
	case in.HarnessError != nil:
		r.Verdict, r.Error = Errored, in.HarnessError.Error()
	case r.FailedCount() > 0:
		r.Verdict = Failed
	default:
		r.Verdict = Passed
	}
	return r
}

// notPulled returns the bundle images no node pulled. An image counts as
// pulled when a pull named its repository with its tag, its digest, or the
// digest it has in the bundle registry.
func notPulled(bundle []string, digests map[string]string, pulled []string) []string {
	type key struct{ repo, ref string }
	seen := map[key]bool{}
	for _, p := range pulled {
		img, err := mirror.Parse(p)
		if err != nil {
			continue
		}
		repo := img.Registry + "/" + img.Repository
		if img.Tag != "" {
			seen[key{repo, img.Tag}] = true
		}
		if img.Digest != "" {
			seen[key{repo, img.Digest}] = true
		}
	}
	var out []string
	for _, b := range bundle {
		img, err := mirror.Parse(b)
		if err != nil {
			continue
		}
		repo := img.Registry + "/" + img.Repository
		used := (img.Tag != "" && seen[key{repo, img.Tag}]) ||
			(img.Digest != "" && seen[key{repo, img.Digest}]) ||
			(digests[b] != "" && seen[key{repo, digests[b]}])
		if !used {
			out = append(out, b)
		}
	}
	return out
}

// window returns the index of the operation whose window contains t, or -1.
//
// Windows partition the drill, so no evidence can fall into two of them:
// operation i owns [its start, the next operation's start). The runner waits
// out each operation's settle period before starting the next, so that span
// includes the settle period. Clock slack applies only at the outer edges:
// before the first operation and after the last one settles.
func window(runs []OperationRun, t time.Time) int {
	for i, run := range runs {
		start := run.Start
		if i == 0 {
			start = start.Add(-clockSlack)
		}
		end := run.End.Add(run.Settle + clockSlack)
		if i+1 < len(runs) {
			end = runs[i+1].Start
		}
		if !t.Before(start) && t.Before(end) {
			return i
		}
	}
	return -1
}

func summarize(op Operation) string {
	if len(op.Findings) == 0 {
		if op.Status == StatusFailed {
			return firstLine(op.Error)
		}
		return ""
	}
	top := op.Findings[0].Message
	if op.Status == StatusFailed {
		return top
	}
	if len(op.Findings) == 1 {
		return "Passed with a warning: " + top
	}
	return fmt.Sprintf("Passed with %d warnings. First: %s", len(op.Findings), top)
}

func sortFindings(fs []Finding) {
	sort.SliceStable(fs, func(i, j int) bool {
		if precedence[fs[i].Kind] != precedence[fs[j].Kind] {
			return precedence[fs[i].Kind] < precedence[fs[j].Kind]
		}
		return fs[i].FirstSeen.Before(fs[j].FirstSeen)
	})
}

// findings turns raw evidence into de-duplicated findings.
func findings(in Input) []Finding {
	inBundle := make(map[string]bool, len(in.Bundle))
	for _, b := range in.Bundle {
		inBundle[b] = true
	}
	acc := &accumulator{index: map[string]int{}}

	// Pod-level evidence first: it knows who needed what.
	missing := map[string]bool{}
	for _, e := range in.Evidence {
		switch e.Kind {
		case evidence.ImagePullFailed:
			ref := canonical(e.Image)
			if inBundle[ref] {
				acc.add("pull/"+ref+"/"+owner(e.Pod), Finding{
					Kind: ImagePullFailed, Image: ref, Pod: e.Pod, FirstSeen: e.Time,
					Message: fmt.Sprintf("%s couldn't pull %s, although it is in the bundle: %s", who(e.Pod), ref, oneLine(e.Message)),
				})
				continue
			}
			missing[ref] = true
			acc.add("missing/"+ref+"/"+owner(e.Pod), Finding{
				Kind: MissingImage, Image: ref, Pod: e.Pod, FirstSeen: e.Time,
				Message: fmt.Sprintf("%s needs %s, which isn't in the bundle.", who(e.Pod), ref),
			})
		case evidence.ContainerFailed:
			msg := fmt.Sprintf("%s: container %s exited with code %d", who(e.Pod), e.Container, e.ExitCode)
			if e.Reason != "" && e.Reason != "Error" {
				msg += " (" + e.Reason + ")"
			}
			acc.add(fmt.Sprintf("exit/%s/%s/%d", owner(e.Pod), e.Container, e.ExitCode), Finding{
				Kind: ContainerFailed, Pod: e.Pod, FirstSeen: e.Time, Logs: e.Logs, Message: msg + ".",
				Container: e.Container, ExitCode: e.ExitCode,
			})
		}
	}

	// Registry requests catch missing images no pod reported, such as pulls
	// by pods deleted before they were seen.
	for _, e := range in.Evidence {
		if e.Kind != evidence.ManifestNotFound {
			continue
		}
		ref := canonical(e.Image)
		if missing[ref] || inBundle[ref] {
			continue
		}
		missing[ref] = true
		acc.add("missing/"+ref+"/", Finding{
			Kind: MissingImage, Image: ref, FirstSeen: e.Time,
			Message: fmt.Sprintf("Something in the cluster requested %s, which isn't in the bundle.", ref),
		})
	}

	// DNS queries, grouped by pod so search-domain expansions can be folded
	// into the name that was actually looked up.
	type client struct {
		pod   *evidence.Pod
		names []string
		first map[string]time.Time
		count map[string]int
	}
	var order []string
	clients := map[string]*client{}
	for _, e := range in.Evidence {
		if e.Kind != evidence.DNSQuery || e.QName == "" {
			continue
		}
		key := e.Client.String()
		if e.Pod != nil {
			key = e.Pod.UID + "/" + e.Pod.Name
		}
		c := clients[key]
		if c == nil {
			c = &client{pod: e.Pod, first: map[string]time.Time{}, count: map[string]int{}}
			clients[key] = c
			order = append(order, key)
		}
		if _, ok := c.first[e.QName]; !ok {
			c.names = append(c.names, e.QName)
			c.first[e.QName] = e.Time
		}
		c.count[e.QName]++
	}
	for _, key := range order {
		c := clients[key]
		subject := who(c.pod)
		if c.pod == nil {
			subject = "A pod at " + strings.SplitN(key, "/", 2)[0]
		}
		for _, name := range dnsmon.DropSearchExpansions(c.names, in.SearchDomains) {
			acc.addCount("dns/"+key+"/"+name, c.count[name], Finding{
				Kind: NetworkLookup, Host: name, Pod: c.pod, FirstSeen: c.first[name],
				Message: fmt.Sprintf("%s looked up %s, which doesn't resolve offline.", subject, name),
			})
		}
	}
	return acc.list
}

type accumulator struct {
	list  []Finding
	index map[string]int
}

func (a *accumulator) add(key string, f Finding) { a.addCount(key, 1, f) }

func (a *accumulator) addCount(key string, n int, f Finding) {
	if i, ok := a.index[key]; ok {
		a.list[i].Count += n
		if f.FirstSeen.Before(a.list[i].FirstSeen) {
			a.list[i].FirstSeen = f.FirstSeen
		}
		if len(a.list[i].Logs) == 0 {
			a.list[i].Logs = f.Logs
		}
		return
	}
	f.Count = n
	a.index[key] = len(a.list)
	a.list = append(a.list, f)
}

func canonical(ref string) string {
	if img, err := mirror.Parse(ref); err == nil {
		return img.String()
	}
	return ref
}

func owner(p *evidence.Pod) string {
	if p == nil {
		return ""
	}
	return p.Owner
}

func who(p *evidence.Pod) string {
	if p == nil || p.Owner == "" {
		return "A pod"
	}
	return p.Owner
}

func firstLine(s string) string {
	line, _, _ := strings.Cut(strings.TrimSpace(s), "\n")
	return line
}

func oneLine(s string) string {
	return strings.Join(strings.Fields(s), " ")
}
