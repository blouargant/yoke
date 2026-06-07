"""Python launcher package for the ``yoke-agent`` distribution.

This package contains no application logic — yoke is a Go program. The wheel
bundles the prebuilt ``yoke`` / ``yoke-server`` binaries plus the default
config/registry/web tree under :data:`_dist`, and the console-script entry
points (:mod:`yoke.launcher`) exec the right binary after pointing it at the
bundled assets and seeding ``~/.yoke`` (:mod:`yoke.seed`).
"""

import os

__all__ = ["dist_dir", "bin_dir", "sysconf_dir", "web_dir"]

# Populated from the wheel version at build time (setup.py). The string here is
# only the fallback for source checkouts / editable installs.
__version__ = "0.0.0.dev0"


def dist_dir():
    """Absolute path to the bundled payload directory (``_dist``)."""
    return os.path.join(os.path.dirname(os.path.abspath(__file__)), "_dist")


def bin_dir():
    """Directory holding the bundled ``yoke`` / ``yoke-server`` binaries."""
    return os.path.join(dist_dir(), "bin")


def sysconf_dir():
    """Bundled system-config layer (matches the YOKE_SYSTEM_CONFIG_DIR contract).

    Holds the default config JSONs, ``filters/`` and ``registry/`` — the same
    tree that gets seeded into ``~/.yoke`` on first run.
    """
    return os.path.join(dist_dir(), "sysconf")


def web_dir():
    """Bundled static Web UI assets (pointed at via YOKE_WEB_DIR)."""
    return os.path.join(dist_dir(), "web")
