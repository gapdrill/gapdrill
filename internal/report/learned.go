// Copyright The gapdrill Authors
// SPDX-License-Identifier: Apache-2.0

package report

import (
	"fmt"
	"strings"

	"github.com/gapdrill/gapdrill/internal/mirror"
)

// LearnedImage is an image the release pulled during a learn run.
type LearnedImage struct {
	Image string `json:"image"`
	// InBundle is false for an image the bundle lacked, which a node pulled
	// from its own registry.
	InBundle bool `json:"inBundle"`
}

// learnedImages marks each pulled image as in the bundle or not. A pull
// matches a bundle image by tag, by digest, or by the digest the image has in
// the bundle registry.
func learnedImages(bundle []string, digests map[string]string, pulled []string) []LearnedImage {
	type key struct{ repo, ref string }
	inBundle := map[key]bool{}
	for _, b := range bundle {
		img, err := mirror.Parse(b)
		if err != nil {
			continue
		}
		repo := img.Registry + "/" + img.Repository
		for _, ref := range []string{img.Tag, img.Digest, digests[b]} {
			if ref != "" {
				inBundle[key{repo, ref}] = true
			}
		}
	}
	out := make([]LearnedImage, 0, len(pulled))
	for _, p := range pulled {
		img, err := mirror.Parse(p)
		if err != nil {
			continue
		}
		repo := img.Registry + "/" + img.Repository
		in := (img.Tag != "" && inBundle[key{repo, img.Tag}]) || (img.Digest != "" && inBundle[key{repo, img.Digest}])
		out = append(out, LearnedImage{Image: p, InBundle: in})
	}
	return out
}

// LearnedFile renders the images of a learn run as an image list, ready to
// use as a drill's bundle.imagesFile. Images the bundle lacked are marked in
// a comment, which image lists ignore.
func (r *Report) LearnedFile() []byte {
	var b strings.Builder
	fmt.Fprintf(&b, "# The images drill %q pulled during a gapdrill learn run (gapdrill %s).\n", r.Drill, r.GapdrillVersion)
	b.WriteString("# Use this as the bundle's image list. Images marked \"new\" weren't in the bundle.\n")
	for _, img := range r.Learned {
		if img.InBundle {
			fmt.Fprintf(&b, "%s\n", img.Image)
		} else {
			fmt.Fprintf(&b, "%s # new\n", img.Image)
		}
	}
	return []byte(b.String())
}
