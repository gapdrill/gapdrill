# gapdrill: demo

**✗ Failed:** 2 of 3 operations failed.

gapdrill v0.1.0 · kindest/node:v1.37.0 · linux/arm64 · Helm v3.22.1 · 2 bundle images

| Operation | Result | Time | Why |
|---|---|---|---|
| install | ✗ failed | 34s | Job demo-post-install (Helm post-install hook) looked up pypi.org, which doesn't resolve offline. |
| scheduled-cleanup | ✗ failed | 1m | CronJob demo-cleanup needs docker.io/library/busybox:1.37.0, which isn't in the bundle. |
| chart-tests | ✓ passed | 6s |  |

## install: failed

Job demo-post-install (Helm post-install hook) looked up pypi.org, which doesn't resolve offline.

- **Blocked lookup** `pypi.org` · Job demo-post-install (Helm post-install hook) · pod `demo/demo-post-install-x7k2p` · 6 queries
- **Container failed** `post-install` exited with code 1 · Job demo-post-install (Helm post-install hook) · pod `demo/demo-post-install-x7k2p`

  ```text
  WARNING: Retrying (Retry(total=0...)) after connection broken by 'NameResolutionError("Failed to resolve 'pypi.org'")'
  ERROR: Could not find a version that satisfies the requirement requests (from versions: none)
  ```

Error: `helm failed: INSTALLATION FAILED: failed post-install: 1 error occurred: * job demo-post-install failed: BackoffLimitExceeded`

<details><summary>helmInstall output</summary>

```text
Release "demo" does not exist. Installing it now.
Error: INSTALLATION FAILED: failed post-install: 1 error occurred:
	* job demo-post-install failed: BackoffLimitExceeded
```

</details>

## scheduled-cleanup: failed

CronJob demo-cleanup needs docker.io/library/busybox:1.37.0, which isn't in the bundle.

- **Missing image** `docker.io/library/busybox:1.37.0` · CronJob demo-cleanup · pod `demo/demo-cleanup-gapdrill-1234-q9v`

Error: `job demo-cleanup-gapdrill-1234 didn't finish within 1m0s`

## How the cluster was isolated

- Nodes and pods could reach only the cluster and the bundle registry (iptables and ip6tables rules inside each node).
- Names outside the cluster got NXDOMAIN from CoreDNS.
- Every registry, including ones not in the bundle, was served by the bundle registry, with no upstream fallback.
- Verified: https://1.1.1.1 was reachable from the node before isolation and unreachable after.
- 12 outbound packets were rejected during the drill.
