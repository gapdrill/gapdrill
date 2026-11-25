# Demo

A small chart with two bugs that only show up offline. Both are common in
real charts.

| Bug | Where | Why an online test misses it |
|---|---|---|
| The post-install hook runs `pip install requests` | `chart/templates/post-install-job.yaml` | Every image is in the bundle; the hook needs PyPI, not an image. |
| The cleanup CronJob uses `busybox:1.37.0`, which isn't in `images.txt` | `chart/values.yaml` | Nothing pulls it until the job runs. |

Run the failing drill (about three minutes, most of it creating the cluster):

```sh
gapdrill run -f gapdrill.yaml
```

It reports `install` failing because the hook looked up `pypi.org`, and
`scheduled-cleanup` failing because `busybox:1.37.0` isn't in the bundle.
`chart-tests` passes.

Then run the fixed drill:

```sh
gapdrill run -f gapdrill-fixed.yaml
```

It adds the image to the bundle, and `values-fixed.yaml` makes the hook use
Python's standard library instead of installing a package. Every operation
passes.
