from pathlib import Path

try:
    application = Path(defines["app"])
except (KeyError, TypeError) as error:
    raise ValueError("settings require -D app=/absolute/path/Loqui.app") from error
try:
    asset_root = Path(defines["assets"])
except (KeyError, TypeError) as error:
    raise ValueError("settings require -D assets=/absolute/path/to/dmg-assets") from error

if not application.is_absolute():
    raise ValueError("app path must be absolute")
if application.name != "Loqui.app":
    raise ValueError("app path must end in Loqui.app")
if not asset_root.is_absolute():
    raise ValueError("assets path must be absolute")

background = str(asset_root / "background.png")

format = "UDZO"
filesystem = "HFS+"
files = [str(application)]
symlinks = {"Applications": "/Applications"}
hide = [".background.tiff"]
hide_extensions = []

window_rect = ((100, 100), (660, 384))
default_view = "icon-view"
show_status_bar = False
show_tab_view = False
show_toolbar = False
show_pathbar = False
show_sidebar = False
show_icon_preview = False

arrange_by = None
label_pos = "bottom"
text_size = 14
icon_size = 128
icon_locations = {
    "Loqui.app": (160, 215),
    "Applications": (500, 215),
}
