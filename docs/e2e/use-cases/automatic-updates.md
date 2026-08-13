# macOS automatic updates — E2E use cases

## AU-CLI-01 — Build and inspect the updater archive

- **ID:** AU-CLI-01.
- **Actor:** Loqui release maintainer.
- **Scenario:** The maintainer prepares a candidate release and must prove that the self-update archive is safe to consume before publishing it.
- **Interface:** CLI.
- **Intent:** Confirm that the updater ZIP contains one signed-app root, survives extraction, and can be rebuilt after app stapling.
- **Setup:** Use a throwaway local fixture; do not contact GitHub, create a tag, or write to `Applications`.
- **Steps:**
  1. Run `scripts/tests/update-zip-test.sh` to create the candidate archive, extract it, and inspect its top-level entries.
  2. Run the release publication contract fixture and compare the DMG/ZIP/checksum asset set with the expected four names.
- **Verification:** The fixture reports `update-zip-test: PASS`, extraction shows exactly `Loqui.app`, the app signature verifier is called, and the publication fixture reports `github-release-test: PASS` without a remote mutation.
- **Persistence:** The second fixture observes the generated archive and checksum contract independently from the first extraction step; all temporary paths are removed on exit.

## AU-UI-01 — Review and explicitly install an available update

- **ID:** AU-UI-01.
- **Actor:** Loqui user.
- **Scenario:** An installed packaged build reports a newer release and the user chooses whether to install and restart it.
- **Interface:** UI.
- **Intent:** Confirm that discovery is visible, installation requires an explicit confirmation, and restart is offered only after a verified download.
- **App root:** `frontend`.
- **App URL:** N/A — this is the native Wails macOS window; a standalone browser cannot exercise the updater host or signed app replacement.
- **Persistence mechanism:** N/A — the update state is held by the running native app and the installed bundle, not browser storage.
- **Setup:** Launch a packaged Developer ID build against a local updater fixture containing a newer signed ZIP; keep the installed app outside the repository.
- **Steps:**
  1. Open **About → Updates**, choose **Buscar actualizaciones**, and inspect the reported version before selecting install.
  2. Confirm **Instalar actualización**, wait for the ready state, then choose **Reiniciar y aplicar** and inspect the relaunched version.
- **Verification:** The About row shows an available version before installation, no download starts before confirmation, the row says the update is ready before restart, and the relaunched app reports the new version.
- **Persistence:** Read the version from the relaunched packaged app after the restart; it remains the updated version.

## AU-UI-02 — Disable scheduled checks without disabling manual checks

- **ID:** AU-UI-02.
- **Actor:** Loqui user.
- **Scenario:** A user opts out of background checks but still wants to check manually from About or the tray menu.
- **Interface:** UI.
- **Intent:** Confirm the preference is persisted and the manual action remains available.
- **App root:** `frontend`.
- **App URL:** N/A — the preference is stored by the native Wails app and is not browser-local state.
- **Persistence mechanism:** N/A — persistence is the app's settings file, outside the supported browser-storage mechanisms.
- **Setup:** Launch a packaged build with the default automatic-check preference enabled.
- **Steps:**
  1. Turn off **Settings → System → Automatic update checks**, close and relaunch Loqui, and read the checkbox.
  2. Open **About → Updates** and trigger a manual check while the scheduled preference remains disabled.
- **Verification:** After relaunch the checkbox remains off, no scheduled check is started, and the manual check still reaches a no-update, available, or generic error state.
- **Persistence:** The second launch reads the setting from the app-owned settings file rather than from the previous webview DOM.

## Interface coverage

- **API:** N/A — the supported runtime interface is the Wails service, not a public HTTP API.
- **CLI:** AU-CLI-01 is executable without remote mutation.
- **UI:** AU-UI-01 and AU-UI-02 require native macOS Wails execution; their headless browser equivalent is not applicable.
