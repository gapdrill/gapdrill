// Copyright The gapdrill Authors
// SPDX-License-Identifier: Apache-2.0

package report

import (
	"strings"
	"testing"
)

func TestFixFor(t *testing.T) {
	for _, tt := range []struct {
		name string
		f    Finding
		want string // a substring of the fix; "" means no fix
	}{
		{"missing image", Finding{Kind: MissingImage, Image: "docker.io/library/busybox:1.37.0"}, "Add `docker.io/library/busybox:1.37.0` to the bundle's image list."},
		{"pypi", Finding{Kind: NetworkLookup, Host: "pypi.org"}, "PIP_INDEX_URL"},
		{"pypi files, fully qualified", Finding{Kind: NetworkLookup, Host: "files.pythonhosted.org."}, "PIP_INDEX_URL"},
		{"npm", Finding{Kind: NetworkLookup, Host: "registry.npmjs.org"}, "npm config set registry"},
		{"release asset", Finding{Kind: NetworkLookup, Host: "objects.githubusercontent.com"}, "OCI artifact"},
		{"registry", Finding{Kind: NetworkLookup, Host: "europe-docker.pkg.dev"}, "container registry"},
		{"label boundary", Finding{Kind: NetworkLookup, Host: "notgithub.com"}, "Nothing answers this name offline"},
		{"unknown host", Finding{Kind: NetworkLookup, Host: "telemetry.example.com"}, "Make the call optional"},
		{"container failure", Finding{Kind: ContainerFailed}, ""},
	} {
		got := fixFor(tt.f)
		if tt.want == "" && got != "" || tt.want != "" && !strings.Contains(got, tt.want) {
			t.Errorf("%s: fixFor = %q, want it to contain %q", tt.name, got, tt.want)
		}
		if got != "" && strings.ToUpper(got[:1]) != got[:1] {
			t.Errorf("%s: a fix is a sentence and starts with a capital: %q", tt.name, got)
		}
	}
}
