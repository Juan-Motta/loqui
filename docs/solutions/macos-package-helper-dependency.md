# macOS packages must build their helper closure

## Symptom

`darwin:package`, `darwin:package:universal`, and `darwin:run` assembled an app from
`helpers/bin` without first producing that directory. A checkout could therefore package only when
an earlier manual helper build had left ambient outputs behind.

## Root cause

The bundle task consumed `helpers/bin`, but its Taskfile graph had no dependency on
`scripts/build-macos-helpers.sh`. The fresh-machine README encoded the same hidden assumption by
building only `globe-listener` before invoking `package`.

## Fix

`darwin:build:helpers` is now the reusable owner of the complete helper/dylib build. Bundle
assembly depends on it, so both package variants inherit the dependency, while `run` declares it
directly. Task's dependency graph executes it once per invocation and always before
`macos-bundle.sh`. The helper output directory is also shared with the bundler when overridden,
which makes the orchestration hermetic to test.

## Verification

`scripts/tests/darwin-taskflow-test.sh` starts with no helper output, runs the real `darwin:run`
task against narrow fakes, and requires the literal sequence
`helpers -> plists -> bundle -> sign -> launch`. Its rendered-task checks also cover both package
variants and `run`, requiring exactly one helper build before the bundler. The complete
`test:macos-release` suite passes with the regression test included.
