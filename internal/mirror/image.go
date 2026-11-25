// Copyright The gapdrill Authors
// SPDX-License-Identifier: Apache-2.0

// Package mirror normalises image references and generates the containerd
// configuration that serves every pull from the drill's bundle registry.
package mirror

import (
	"fmt"
	"strings"

	"github.com/google/go-containerregistry/pkg/name"
)

// Image is a normalised container image reference.
type Image struct {
	// Registry is the registry host as containerd names it, such as
	// "docker.io", "quay.io" or "localhost:5000".
	Registry string
	// Repository is the path within the registry, such as "library/busybox".
	Repository string
	// Tag is empty for digest references.
	Tag string
	// Digest is "sha256:…" for digest references.
	Digest string
}

// Parse normalises ref the way container runtimes do, so that "busybox",
// "docker.io/busybox:latest" and "index.docker.io/library/busybox" are the
// same image.
func Parse(ref string) (Image, error) {
	r, err := name.ParseReference(ref)
	if err != nil {
		return Image{}, fmt.Errorf("invalid image reference %q: %w", ref, err)
	}
	img := Image{
		Registry:   strings.ToLower(r.Context().RegistryStr()),
		Repository: r.Context().RepositoryStr(),
	}
	if img.Registry == name.DefaultRegistry {
		img.Registry = "docker.io"
	}
	switch v := r.(type) {
	case name.Tag:
		img.Tag = v.TagStr()
	case name.Digest:
		img.Digest = v.DigestStr()
	}
	return img, nil
}

// String returns the canonical reference, such as
// "docker.io/library/busybox:1.36.1".
func (i Image) String() string {
	s := i.Registry + "/" + i.Repository
	if i.Tag != "" {
		s += ":" + i.Tag
	}
	if i.Digest != "" {
		s += "@" + i.Digest
	}
	return s
}

// MirrorRepository is the image's repository inside the bundle registry. The
// upstream registry is kept as the first path component, so the same
// repository on two registries can't collide.
func (i Image) MirrorRepository() string { return MirrorPrefix(i.Registry) + "/" + i.Repository }

// MirrorReference is the image's reference inside the bundle registry at addr.
func (i Image) MirrorReference(addr string) string {
	ref := addr + "/" + i.MirrorRepository()
	if i.Digest != "" {
		return ref + "@" + i.Digest
	}
	return ref + ":" + i.Tag
}

// MirrorPrefix is the repository path component that stands for an upstream
// registry. Ports become "_<port>", since ':' isn't allowed in repository
// names.
func MirrorPrefix(registry string) string {
	return strings.ReplaceAll(strings.ToLower(registry), ":", "_")
}
