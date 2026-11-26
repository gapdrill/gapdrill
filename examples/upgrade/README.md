# Offline upgrade and rollback

This example installs chart 1.0.0, upgrades the same release to 2.0.0, and rolls it back to revision 1. Only the upgrade runs the migration Job. Its image is missing from the example bundle. Both chart versions use the same registry and the same bundle throughout the drill.

From the repository root, copy the example so you can change its image list and keep both reports.

```sh
trial=$(mktemp -d)
cp -R examples/upgrade "$trial/upgrade"
cd "$trial/upgrade"
gapdrill run -f gapdrill.yaml --report-dir broken-report
```

The expected exit code is 1. In `broken-report/report.md`, `install-v1` passes and `upgrade-v2` reports the missing `docker.io/library/busybox:1.37.0` image. The runner continues after the failed upgrade. Helm rolls back the release, then a test pod mounts the live ConfigMap and reads the original schema. The last two operations should pass.

Add the image shipped for the migration and repeat the same drill.

```sh
printf '%s\n' 'docker.io/library/busybox:1.37.0' >> images.txt
gapdrill run -f gapdrill.yaml --report-dir fixed-report
```

The expected exit code is 0. The migration pod reads the candidate schema before rollback and the old reader reads the restored schema afterwards. To adapt this drill, point the install and upgrade steps at your two local charts, list the images available at the site, and replace the reader with a check for state your old application needs. Helm rollback restores chart-managed resources. Applications that migrate persistent data need a check of that data too.
