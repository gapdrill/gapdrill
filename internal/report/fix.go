// Copyright The gapdrill Authors
// SPDX-License-Identifier: Apache-2.0

package report

import (
	"fmt"
	"strings"
)

// hostFixes suggest a fix for a blocked lookup by the kind of service the
// host belongs to. A host matches a domain when it's the domain or one of
// its subdomains. The first match wins.
var hostFixes = []struct {
	domains []string
	fix     string
}{
	{
		[]string{"pypi.org", "pythonhosted.org"},
		"Install Python packages when the image is built, or point pip at a package mirror inside the site (`PIP_INDEX_URL`).",
	},
	{
		[]string{"registry.npmjs.org", "registry.yarnpkg.com"},
		"Install npm packages when the image is built, or point npm at a package mirror inside the site (`npm config set registry`).",
	},
	{
		[]string{"proxy.golang.org", "sum.golang.org"},
		"Download Go modules when the image is built, or point `GOPROXY` at a module mirror inside the site.",
	},
	{
		[]string{"repo.maven.apache.org", "repo1.maven.org", "plugins.gradle.org"},
		"Resolve Java dependencies when the image is built, or point Maven or Gradle at a repository mirror inside the site.",
	},
	{
		[]string{"rubygems.org"},
		"Install gems when the image is built, or point Bundler at a gem mirror inside the site.",
	},
	{
		[]string{"github.com", "githubusercontent.com"},
		"Download the file when the image is built, or ship it in the bundle (for example as an OCI artifact).",
	},
	{
		[]string{"dl-cdn.alpinelinux.org", "deb.debian.org", "security.debian.org", "archive.ubuntu.com", "security.ubuntu.com", "fedoraproject.org"},
		"Install OS packages when the image is built, or point the package manager at a mirror inside the site.",
	},
	{
		[]string{"docker.io", "ghcr.io", "quay.io", "gcr.io", "registry.k8s.io", "pkg.dev", "public.ecr.aws", "mcr.microsoft.com"},
		"A workload talks to a container registry itself, not through the node: ship what it fetches in the bundle, or point it at the site's registry.",
	},
}

// fixFor returns the likely fix for a finding, or "".
func fixFor(f Finding) string {
	switch f.Kind {
	case MissingImage:
		return fmt.Sprintf("Add `%s` to the bundle's image list.", f.Image)
	case NetworkLookup:
		host := strings.ToLower(strings.TrimSuffix(f.Host, "."))
		for _, hf := range hostFixes {
			for _, d := range hf.domains {
				if host == d || strings.HasSuffix(host, "."+d) {
					return hf.fix
				}
			}
		}
		return "Nothing answers this name offline. Make the call optional, remove it, or point it at a service inside the site."
	}
	return ""
}
