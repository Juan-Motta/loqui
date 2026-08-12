#!/usr/bin/env python3
import sys
from pathlib import Path

from ds_store import DSStore
from mac_alias import Alias


MISSING = object()


def format_value(value):
    if value is MISSING:
        return "<missing>"
    if isinstance(value, bool):
        return str(value).lower()
    if isinstance(value, float) and value.is_integer():
        return str(int(value))
    return str(value)


def values_match(actual, expected):
    if isinstance(expected, bool):
        return actual is expected
    return not isinstance(actual, bool) and actual == expected


def fail(field, expected, actual):
    print(
        f"verify-ds-store: {field}: expected {format_value(expected)}, "
        f"got {format_value(actual)}",
        file=sys.stderr,
    )
    raise SystemExit(1)


def check(field, actual, expected):
    if not values_match(actual, expected):
        fail(field, expected, actual)


def record(store, filename, code):
    try:
        return store[filename][code]
    except KeyError:
        return MISSING


def record_field(store, record_name, field):
    value = record(store, ".", record_name)
    if not isinstance(value, dict):
        return MISSING
    return value.get(field, MISSING)


def check_background_alias(value):
    if not isinstance(value, bytes):
        fail("icvp.backgroundImageAlias", "parseable alias", value)
    try:
        alias = Alias.from_bytes(value)
    except (IndexError, TypeError, UnicodeError, ValueError):
        fail("icvp.backgroundImageAlias", "parseable alias", "<unparseable>")
    check(
        "icvp.backgroundImageAlias target",
        alias.target.filename,
        ".background.tiff",
    )
    check(
        "icvp.backgroundImageAlias path",
        alias.target.posix_path,
        "/.background.tiff",
    )


def main():
    if len(sys.argv) != 2:
        print("usage: verify-ds-store.py .DS_Store", file=sys.stderr)
        raise SystemExit(64)

    store_path = Path(sys.argv[1])
    if store_path.is_symlink() or not store_path.is_file():
        print(
            f"verify-ds-store: not a regular non-symlink file: {store_path}",
            file=sys.stderr,
        )
        raise SystemExit(1)

    with DSStore.open(str(store_path), "r") as store:
        check("vSrn", record(store, ".", "vSrn"), (b"long", 1))
        check("icvl", record(store, ".", "icvl"), (b"type", b"icnv"))
        check(
            "bwsp.WindowBounds",
            record_field(store, "bwsp", "WindowBounds"),
            "{{100, 100}, {660, 384}}",
        )
        check(
            "bwsp.ShowStatusBar",
            record_field(store, "bwsp", "ShowStatusBar"),
            False,
        )
        check(
            "bwsp.ShowTabView",
            record_field(store, "bwsp", "ShowTabView"),
            False,
        )
        check(
            "bwsp.ShowToolbar",
            record_field(store, "bwsp", "ShowToolbar"),
            False,
        )
        check(
            "bwsp.ShowPathbar",
            record_field(store, "bwsp", "ShowPathbar"),
            False,
        )
        check(
            "bwsp.ShowSidebar",
            record_field(store, "bwsp", "ShowSidebar"),
            False,
        )
        check("icvp.arrangeBy", record_field(store, "icvp", "arrangeBy"), "none")
        check(
            "icvp.backgroundType",
            record_field(store, "icvp", "backgroundType"),
            2,
        )
        check("icvp.iconSize", record_field(store, "icvp", "iconSize"), 128)
        check("icvp.textSize", record_field(store, "icvp", "textSize"), 14)
        check(
            "icvp.labelOnBottom",
            record_field(store, "icvp", "labelOnBottom"),
            True,
        )
        check_background_alias(
            record_field(store, "icvp", "backgroundImageAlias")
        )
        check("Loqui.app.Iloc", record(store, "Loqui.app", "Iloc"), (160, 215))
        check(
            "Applications.Iloc",
            record(store, "Applications", "Iloc"),
            (500, 215),
        )

    print("verify-ds-store: PASS")


if __name__ == "__main__":
    main()
