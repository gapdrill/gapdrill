// Copyright The gapdrill Authors
// SPDX-License-Identifier: Apache-2.0

// Package runner orchestrates a drill: prepare, isolate, verify, run the
// operations, report and clean up.
package runner

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"k8s.io/client-go/kubernetes"
	"sigs.k8s.io/kind/pkg/apis/config/defaults"

	"github.com/gapdrill/gapdrill/internal/cluster"
	"github.com/gapdrill/gapdrill/internal/docker"
	"github.com/gapdrill/gapdrill/internal/mirror"
	"github.com/gapdrill/gapdrill/internal/observe"
	"github.com/gapdrill/gapdrill/internal/registry"
	"github.com/gapdrill/gapdrill/internal/report"
	"github.com/gapdrill/gapdrill/internal/spec"
	"github.com/gapdrill/gapdrill/internal/version"
)

// Exit codes. They separate "the release failed" from "the drill couldn't
// run", so CI never mistakes one for the other.
const (
	ExitPassed = 0
	ExitFailed = 1
	ExitError  = 2
	ExitUsage  = 3
)

// DefaultProbeURL is fetched from the nodes before and after isolation.
const DefaultProbeURL = "https://1.1.1.1"

const (
	namePrefix = "gapdrill-"
	runLabel   = "io.gapdrill.run"
)

// Options configure a drill run.
type Options struct {
	DrillPath string
	// Drill, when set, is run instead of the drill at DrillPath: it's how
	// `gapdrill run --chart` runs a drill built from flags.
	Drill     *spec.Drill
	ReportDir string
	// Keep leaves the cluster running for debugging.
	Keep     bool
	Timeout  time.Duration
	ProbeURL string
	Verbose  bool
	// Stdout receives the final summary; Stderr receives progress.
	Stdout, Stderr io.Writer
}

type run struct {
	o Options
	p *progress

	drill   *spec.Drill
	images  []mirror.Image
	helmBin string

	id, name   string
	tmpDir     string
	docker     *docker.Client
	networkUp  bool
	cluster    *cluster.Cluster
	registry   *registry.Registry
	cs         kubernetes.Interface
	obs        *observe.Observer
	isolatedAt time.Time

	started       time.Time
	env           report.Environment
	iso           report.Isolation
	runs          []report.OperationRun
	searchDomains []string
	// bundleDigests maps each bundle image to its digest in the bundle
	// registry; pulled lists the references nodes pulled from it.
	bundleDigests map[string]string
	pulled        []string
}

// Run runs a drill and returns the process exit code.
func Run(ctx context.Context, o Options) int {
	if o.ProbeURL == "" {
		o.ProbeURL = DefaultProbeURL
	}
	r := &run{o: o, p: newProgress(o.Stderr, o.Verbose), started: time.Now()}
	if err := r.load(); err != nil {
		r.p.errorf("%v", err)
		return ExitUsage
	}
	r.p.header(r.drill, len(r.images))

	runCtx, cancel := context.WithTimeout(ctx, o.Timeout)
	harnessErr := r.drillPhases(runCtx)
	if errors.Is(runCtx.Err(), context.DeadlineExceeded) {
		harnessErr = fmt.Errorf("the drill didn't finish within %s", o.Timeout)
	}
	cancel()

	r.collect()
	rep := r.buildReport(harnessErr)
	paths, writeErr := r.writeReport(rep)
	r.teardown()

	r.p.summary(o.Stdout, rep, paths)
	if writeErr != nil {
		r.p.errorf("write report: %v", writeErr)
		return ExitError
	}
	switch rep.Verdict {
	case report.Passed:
		return ExitPassed
	case report.Failed:
		return ExitFailed
	default:
		return ExitError
	}
}

// Validate loads a drill and its bundle list without running anything.
func Validate(path string) (*spec.Drill, []string, error) {
	d, err := spec.Load(path)
	if err != nil {
		return nil, nil, err
	}
	refs, err := bundleRefs(d)
	if err != nil {
		return nil, nil, err
	}
	return d, refs, nil
}

// bundleRefs returns the drill's bundle images, checking each reference.
func bundleRefs(d *spec.Drill) ([]string, error) {
	refs, err := d.BundleImages()
	if err != nil {
		return nil, err
	}
	for _, ref := range refs {
		if _, err := mirror.Parse(ref); err != nil {
			return nil, fmt.Errorf("bundle: %w", err)
		}
	}
	return refs, nil
}

func (r *run) load() error {
	d := r.o.Drill
	var refs []string
	var err error
	if d == nil {
		d, refs, err = Validate(r.o.DrillPath)
	} else {
		refs, err = bundleRefs(d)
	}
	if err != nil {
		return fmt.Errorf("invalid drill: %w", err)
	}
	r.drill = d
	for _, ref := range refs {
		img, _ := mirror.Parse(ref)
		r.images = append(r.images, img)
	}
	for _, op := range d.Operations {
		if op.HelmInstall != nil || op.HelmTest != nil {
			if r.helmBin, err = exec.LookPath("helm"); err != nil {
				return errors.New("helm isn't on PATH, and the drill has Helm operations")
			}
			break
		}
	}
	return nil
}

// drillPhases runs everything up to and including the operations. An error
// means the drill itself couldn't run.
func (r *run) drillPhases(ctx context.Context) error {
	var err error
	if r.docker, err = docker.New(ctx); err != nil {
		return err
	}
	r.id = randomID()
	r.name = namePrefix + r.id
	if r.tmpDir, err = os.MkdirTemp("", r.name+"-"); err != nil {
		return err
	}
	if err := r.describeEnvironment(ctx); err != nil {
		return err
	}

	r.p.step("Creating an isolated network")
	if _, err := r.docker.Run(ctx, "network", "create", "--label", runLabel+"="+r.id, r.name); err != nil {
		return err
	}
	r.networkUp = true

	r.p.step("Copying %d bundle image%s and creating the cluster", len(r.images), plural(len(r.images)))
	if err := r.prepare(ctx); err != nil {
		return err
	}

	r.p.step("Cutting the cluster off the internet")
	if err := r.isolate(ctx); err != nil {
		return err
	}

	r.p.step("Verifying the cluster is offline")
	if err := r.verify(ctx); err != nil {
		return err
	}

	r.p.step("Running %d operation%s", len(r.drill.Operations), plural(len(r.drill.Operations)))
	return r.operations(ctx)
}

func (r *run) describeEnvironment(ctx context.Context) error {
	arch, err := r.docker.Architecture(ctx)
	if err != nil {
		return err
	}
	r.env.Platform = "linux/" + arch
	image := r.drill.Cluster.NodeImage
	if image == "" {
		image = defaults.Image
	}
	r.env.NodeImage, _, _ = strings.Cut(image, "@")
	if r.helmBin != "" {
		out, err := exec.CommandContext(ctx, r.helmBin, "version", "--short").Output()
		if err == nil {
			r.env.Helm, _, _ = strings.Cut(strings.TrimSpace(string(out)), "+")
		}
	}
	return nil
}

func (r *run) buildReport(harnessErr error) *report.Report {
	bundle := make([]string, len(r.images))
	for i, img := range r.images {
		bundle[i] = img.String()
	}
	in := report.Input{
		Drill:         r.drill.Metadata.Name,
		Version:       version.String(),
		Started:       r.started,
		Finished:      time.Now(),
		Environment:   r.env,
		Isolation:     r.iso,
		Bundle:        bundle,
		Operations:    r.runs,
		SearchDomains: r.searchDomains,
		HarnessError:  harnessErr,
		Pulled:        r.pulled,
		BundleDigests: r.bundleDigests,
	}
	if r.obs != nil {
		in.Evidence = r.attributedEvidence()
	}
	return report.Build(in)
}

func (r *run) writeReport(rep *report.Report) ([]string, error) {
	if err := os.MkdirAll(r.o.ReportDir, 0o755); err != nil {
		return nil, err
	}
	js, err := rep.JSON()
	if err != nil {
		return nil, err
	}
	md := rep.Markdown()
	mdPath := filepath.Join(r.o.ReportDir, "report.md")
	jsPath := filepath.Join(r.o.ReportDir, "report.json")
	if err := os.WriteFile(mdPath, []byte(md), 0o644); err != nil {
		return nil, err
	}
	if err := os.WriteFile(jsPath, js, 0o644); err != nil {
		return nil, err
	}
	paths := []string{mdPath, jsPath}
	// A drill that couldn't run observed nothing, and an empty inventory would
	// wrongly say the release contacts nothing.
	if rep.Verdict != report.Errored {
		bom, err := rep.EgressBOM("urn:uuid:" + uuidV4())
		if err != nil {
			return nil, err
		}
		bomPath := filepath.Join(r.o.ReportDir, "egress.cdx.json")
		if err := os.WriteFile(bomPath, bom, 0o644); err != nil {
			return nil, err
		}
		paths = append(paths, bomPath)
	}
	if summary := os.Getenv("GITHUB_STEP_SUMMARY"); summary != "" {
		f, err := os.OpenFile(summary, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
		if err == nil {
			_, err = f.WriteString(md + "\n")
			// A failed close can mean a failed write; don't drop its error.
			if cerr := f.Close(); err == nil {
				err = cerr
			}
		}
		if err != nil {
			r.p.warnf("couldn't append to GITHUB_STEP_SUMMARY: %v", err)
		}
	}
	return paths, nil
}

// teardown removes everything the run created, unless --keep.
func (r *run) teardown() {
	if r.docker == nil {
		return
	}
	if r.o.Keep && r.cluster != nil {
		r.p.kept(r.name, r.cluster.KubeconfigPath)
		return
	}
	if r.cluster != nil {
		if err := r.cluster.Delete(); err != nil {
			r.p.warnf("delete cluster %s: %v (run `gapdrill cleanup`)", r.name, err)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	if r.name != "" {
		_, _ = r.docker.Run(ctx, "rm", "--force", r.name+"-registry")
	}
	if r.networkUp {
		if _, err := r.docker.Run(ctx, "network", "rm", r.name); err != nil {
			r.p.warnf("remove network %s: %v (run `gapdrill cleanup`)", r.name, err)
		}
	}
	if r.tmpDir != "" {
		_ = os.RemoveAll(r.tmpDir)
	}
}

func randomID() string {
	b := make([]byte, 3)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// uuidV4 returns a random RFC 9562 version 4 UUID.
func uuidV4() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}
