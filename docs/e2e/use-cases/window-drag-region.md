# Movable settings window — E2E use cases

## WDR-UI-01 — Move the settings window from its top strip

- **ID:** WDR-UI-01.
- **Actor:** Loqui user.
- **Scenario:** The user holds the primary mouse button on the empty strip at the top of the settings content and drags the window to another screen position.
- **Interface:** UI.
- **Intent:** Confirm that the main Loqui window can be repositioned while retaining its dimensions.
- **App root:** `frontend`.
- **App URL:** N/A — this is the production-shaped native Wails WKWebView; a standalone browser cannot exercise the host window's macOS drag behavior or Go bindings.
- **Persistence mechanism:** N/A — moving the active window is immediate, session-scoped behavior; restoring its position after restart is outside this fix.
- **Setup:** Build and launch `bin/loqui.dev.app` from the current branch after closing the installed Loqui process to avoid its single-instance guard. Record the native window's initial position and size.
- **Steps:**
  1. Hold the primary mouse button on the empty top content strip and drag it by a visible horizontal and vertical offset.
  2. Read the same native window's final position and size after releasing the button.
- **Verification:** The final position differs from the initial position in the requested direction, the final size exactly matches the initial size, and the window remains responsive.
- **Persistence:** Read the position and size a second time before closing the development app; they remain equal to the post-drag observation.

## WDR-UI-02 — Interactive sidebar controls do not become drag handles

- **ID:** WDR-UI-02.
- **Actor:** Loqui user.
- **Scenario:** After moving the window, the user clicks a sidebar navigation item that inherits from the draggable sidebar container.
- **Interface:** UI.
- **Intent:** Confirm that interactive descendants retain their click behavior and do not move the host window.
- **App root:** `frontend`.
- **App URL:** N/A — navigation and host-window movement are coupled to the native Wails runtime.
- **Persistence mechanism:** N/A — this verifies immediate interaction behavior and performs no settings write.
- **Setup:** WDR-UI-01 completed and the development window remains open at its post-drag position.
- **Steps:**
  1. Click a sidebar navigation item with the primary mouse button without moving the pointer.
  2. Read the selected view and the native window position and size.
- **Verification:** The requested view becomes active, while position and size remain unchanged from the post-drag observation.
- **Persistence:** Repeat the native geometry read before closing the development app; it remains unchanged.
