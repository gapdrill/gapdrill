// Copyright The gapdrill Authors
// SPDX-License-Identifier: Apache-2.0

// Package evidence defines what gapdrill observes while a drill runs.
package evidence

import (
	"net/netip"
	"time"
)

// Kind is the kind of an Item.
type Kind string

// Evidence kinds.
const (
	// ImagePullFailed: a container is waiting because its image couldn't be
	// pulled.
	ImagePullFailed Kind = "image-pull-failed"
	// ContainerFailed: a container exited with a non-zero code.
	ContainerFailed Kind = "container-failed"
	// DNSQuery: a pod looked up a name outside the cluster.
	DNSQuery Kind = "dns-query"
	// ManifestNotFound: the bundle registry was asked for an image it doesn't
	// have.
	ManifestNotFound Kind = "manifest-not-found"
)

// Pod identifies a pod and what owns it.
type Pod struct {
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
	UID       string `json:"-"`
	// Owner is a readable owner chain, such as "CronJob demo-cleanup" or
	// "Job demo-post-install (Helm post-install hook)".
	Owner string `json:"owner"`
}

// Item is one piece of evidence.
type Item struct {
	Kind Kind
	Time time.Time
	// Pod is nil for registry evidence and for DNS queries until attributed.
	Pod *Pod

	// Image is the reference as the pod or registry request gave it.
	Image   string
	Reason  string
	Message string

	Container string
	ExitCode  int32
	Logs      []string

	Client netip.Addr
	QName  string
	QType  string
}
