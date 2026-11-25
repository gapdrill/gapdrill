// Copyright The gapdrill Authors
// SPDX-License-Identifier: Apache-2.0

// Package learn works out which images a release pulled during a learn run,
// from the nodes' image stores before and after the drill. The image store,
// unlike the registry log, only lists pulls that succeeded.
package learn

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/gapdrill/gapdrill/internal/mirror"
)

// ImagesCommand lists a node's images as JSON.
var ImagesCommand = []string{"crictl", "images", "--output", "json"}

// ParseImages returns the canonical references in the output of
// ImagesCommand. An image is listed by each of its tags, or by its digest
// when it has no tag.
func ParseImages(out string) ([]string, error) {
	var list struct {
		Images []struct {
			RepoTags    []string `json:"repoTags"`
			RepoDigests []string `json:"repoDigests"`
		} `json:"images"`
	}
	if err := json.Unmarshal([]byte(out), &list); err != nil {
		return nil, fmt.Errorf("parse the node's image list: %w", err)
	}
	var refs []string
	for _, img := range list.Images {
		names := img.RepoTags
		if len(names) == 0 {
			names = img.RepoDigests
		}
		for _, n := range names {
			if strings.HasPrefix(n, "<none>") {
				continue
			}
			if parsed, err := mirror.Parse(n); err == nil {
				refs = append(refs, parsed.String())
			}
		}
	}
	return refs, nil
}

// New returns the references in after that aren't in before, sorted and
// without duplicates: the images pulled in between.
func New(before, after []string) []string {
	had := make(map[string]bool, len(before))
	for _, r := range before {
		had[r] = true
	}
	seen := map[string]bool{}
	var out []string
	for _, r := range after {
		if !had[r] && !seen[r] {
			seen[r] = true
			out = append(out, r)
		}
	}
	sort.Strings(out)
	return out
}
