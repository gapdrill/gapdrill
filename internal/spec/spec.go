// Copyright The gapdrill Authors
// SPDX-License-Identifier: Apache-2.0

// Package spec defines the drill file format (gapdrill/v1alpha1).
package spec

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

const (
	// APIVersion is the only drill file version this release understands.
	APIVersion = "gapdrill/v1alpha1"
	// Kind is the drill file's kind.
	Kind = "Drill"
)

// Default timeouts and settle period, used when a drill doesn't set them.
const (
	DefaultHelmInstallTimeout = 5 * time.Minute
	DefaultOperationTimeout   = 3 * time.Minute
	DefaultSettle             = 5 * time.Second
)

// Drill describes one offline drill: the release, the bundle it ships with,
// and the operations to exercise.
type Drill struct {
	APIVersion string      `json:"apiVersion"`
	Kind       string      `json:"kind"`
	Metadata   Metadata    `json:"metadata"`
	Cluster    Cluster     `json:"cluster,omitempty"`
	Bundle     Bundle      `json:"bundle"`
	Release    Release     `json:"release"`
	Operations []Operation `json:"operations"`

	// Dir is the directory holding the drill file. Relative paths in the drill
	// resolve against it. It is not part of the file format.
	Dir string `json:"-"`
}

// Metadata names the drill.
type Metadata struct {
	Name string `json:"name"`
}

// Cluster configures the offline cluster.
type Cluster struct {
	// NodeImage overrides the kind node image, and with it the Kubernetes
	// version. The default is the image pinned by gapdrill's kind version.
	NodeImage string `json:"nodeImage,omitempty"`
}

// Bundle lists the images that exist at the offline site.
type Bundle struct {
	// ImagesFile holds one image reference per line. '#' starts a comment.
	ImagesFile string `json:"imagesFile,omitempty"`
	// Images are added to the images in ImagesFile.
	Images []string `json:"images,omitempty"`
}

// Release is the Helm release under test.
type Release struct {
	// Chart is a local chart directory (relative to the drill file) or any
	// chart reference Helm accepts, such as oci://… or repo/chart.
	Chart       string            `json:"chart"`
	Name        string            `json:"name"`
	Namespace   string            `json:"namespace,omitempty"`
	ValuesFiles []string          `json:"valuesFiles,omitempty"`
	Set         map[string]string `json:"set,omitempty"`
}

// Operation is one step users depend on. Exactly one of the typed fields is
// set.
type Operation struct {
	Name string `json:"name"`
	// Settle is how long evidence keeps being attributed to this operation
	// after it ends, to catch effects that show up a moment later.
	Settle *Duration `json:"settle,omitempty"`

	HelmInstall *HelmInstallOp `json:"helmInstall,omitempty"`
	HelmTest    *HelmTestOp    `json:"helmTest,omitempty"`
	RunCronJob  *RunCronJobOp  `json:"runCronJob,omitempty"`
	Exec        *ExecOp        `json:"exec,omitempty"`
}

// HelmInstallOp installs (or upgrades) the release and waits for it.
type HelmInstallOp struct {
	Timeout *Duration `json:"timeout,omitempty"`
}

// HelmTestOp runs the chart's test hooks.
type HelmTestOp struct {
	Timeout *Duration `json:"timeout,omitempty"`
}

// RunCronJobOp starts a Job from a CronJob's template, as
// `kubectl create job --from=cronjob/NAME` does, and waits for it.
type RunCronJobOp struct {
	Name string `json:"name"`
	// Namespace defaults to the release namespace.
	Namespace string    `json:"namespace,omitempty"`
	Timeout   *Duration `json:"timeout,omitempty"`
}

// ExecOp runs a command on the host with KUBECONFIG pointing at the offline
// cluster, for steps the other operations don't cover (for example, applying
// a custom resource that an operator reconciles).
type ExecOp struct {
	Command []string  `json:"command"`
	Timeout *Duration `json:"timeout,omitempty"`
}

// Operation type names, as used in reports.
const (
	TypeHelmInstall = "helmInstall"
	TypeHelmTest    = "helmTest"
	TypeRunCronJob  = "runCronJob"
	TypeExec        = "exec"
)

// Type returns the operation's type name, or "" when none is set.
func (o Operation) Type() string {
	switch {
	case o.HelmInstall != nil:
		return TypeHelmInstall
	case o.HelmTest != nil:
		return TypeHelmTest
	case o.RunCronJob != nil:
		return TypeRunCronJob
	case o.Exec != nil:
		return TypeExec
	}
	return ""
}

func (o Operation) typeCount() int {
	n := 0
	for _, set := range []bool{o.HelmInstall != nil, o.HelmTest != nil, o.RunCronJob != nil, o.Exec != nil} {
		if set {
			n++
		}
	}
	return n
}

// Timeout returns the operation's timeout, or its type's default.
func (o Operation) Timeout() time.Duration {
	switch {
	case o.HelmInstall != nil:
		return o.HelmInstall.Timeout.Or(DefaultHelmInstallTimeout)
	case o.HelmTest != nil:
		return o.HelmTest.Timeout.Or(DefaultOperationTimeout)
	case o.RunCronJob != nil:
		return o.RunCronJob.Timeout.Or(DefaultOperationTimeout)
	case o.Exec != nil:
		return o.Exec.Timeout.Or(DefaultOperationTimeout)
	}
	return DefaultOperationTimeout
}

// SettlePeriod returns the operation's settle period, or the default.
func (o Operation) SettlePeriod() time.Duration { return o.Settle.Or(DefaultSettle) }

// Duration is a time.Duration written as a string such as "90s" or "5m".
type Duration time.Duration

// UnmarshalJSON implements json.Unmarshaler.
func (d *Duration) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return errors.New(`durations are strings such as "90s" or "5m"`)
	}
	v, err := time.ParseDuration(s)
	if err != nil {
		return fmt.Errorf("invalid duration %q: %w", s, err)
	}
	if v <= 0 {
		return fmt.Errorf("duration %q must be positive", s)
	}
	*d = Duration(v)
	return nil
}

// MarshalJSON implements json.Marshaler.
func (d Duration) MarshalJSON() ([]byte, error) { return json.Marshal(time.Duration(d).String()) }

// Or returns the duration, or def when d is nil.
func (d *Duration) Or(def time.Duration) time.Duration {
	if d == nil {
		return def
	}
	return time.Duration(*d)
}
