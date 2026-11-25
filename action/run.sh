#!/usr/bin/env bash
# Copyright The gapdrill Authors
# SPDX-License-Identifier: Apache-2.0
#
# Runs the drill for the GitHub Action, records the action's outputs and exits
# with gapdrill's exit code. gapdrill adds the report to the job summary
# itself.
set -uo pipefail

read -r -a extra <<<"${ARGS:-}"
# ${extra[@]+...} keeps an empty array from tripping `set -u` in bash < 4.4.
gapdrill run -f "$DRILL" --report-dir "$REPORT_DIR" ${extra[@]+"${extra[@]}"}
code=$?

case $code in
0) verdict=passed ;;
1) verdict=failed ;;
2) verdict=error ;;
*) verdict=invalid ;;
esac

{
  echo "exit-code=$code"
  echo "verdict=$verdict"
  echo "report=$REPORT_DIR/report.json"
} >>"$GITHUB_OUTPUT"
exit "$code"
