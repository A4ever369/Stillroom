#!/usr/bin/env python3
"""Build the stillroom.sh hero: a static pixel-art still plus its animated layers.

The illustration is a raster because it has 32 colours of glass, copper and
stone shading — the kind of detail hand-authored SVG rects cannot carry. The
motion is authored separately as pixels on the same integer grid.

Why not generate animation frames as images: an image model redraws the whole
picture every time, so the flask outline shifts a pixel between frames and the
apparatus visibly jitters. One base image never jitters. Everything that moves
is placed here, by coordinate, against that fixed base.

Run:  python3 scripts/gen-hero.py
"""

from __future__ import annotations

import json
import pathlib
import sys

try:
    from PIL import Image
except ImportError:  # pragma: no cover - developer tooling, not shipped
    sys.exit("this script needs Pillow: pip install pillow")

ROOT = pathlib.Path(__file__).resolve().parent.parent
SOURCE = ROOT / ".pixgen" / "codex-still.png"
STATIC = ROOT / "cmd" / "stillroom-hub" / "web" / "static"

GRID = 128        # the real pixel grid the art is quantised onto
COLOURS = 32      # palette size: enough for glass/copper/stone shading, still small

# The drop the source image already drew, in the gap between the spout and the
# receiving bottle. It is erased from the base so the animated one does not
# render on top of a permanent twin.
BAKED_DROP = [(x, y) for x in (89, 90, 91) for y in range(80, 87)]


def build_base() -> Image.Image:
    if not SOURCE.exists():
        sys.exit(f"missing {SOURCE.relative_to(ROOT)} — the source illustration")
    src = Image.open(SOURCE).convert("RGB")
    base = (src.resize((GRID, GRID), Image.LANCZOS)
               .quantize(colors=COLOURS, method=Image.MEDIANCUT)
               .convert("RGBA"))

    px = base.load()
    # Repaint the baked drop with the background it sits on, sampled from a
    # neighbouring empty column so the patch cannot betray itself.
    for x, y in BAKED_DROP:
        px[x, y] = px[x - 6, y]
    return base


# ---- animated layers, in grid coordinates ----------------------------------
#
# Each layer is a list of frames; a frame is a list of (x, y, class) pixels.
# Frames are equal length per layer so a steps() cycle can hold them.

def fire_frames() -> list[list[tuple[int, int, str]]]:
    """Flame tongues over the brazier. Fire has no periodic motion, so it is
    the one layer that genuinely needs hand-drawn frames rather than a path."""
    tongues = [
        # (x, y, class) — 'hot' is the core, 'warm' the falloff
        [(38, 103, "hot"), (39, 104, "hot"), (37, 105, "warm"), (40, 105, "warm"),
         (36, 107, "warm"), (41, 107, "warm"), (38, 101, "warm")],
        [(39, 102, "hot"), (38, 104, "hot"), (40, 104, "warm"), (36, 106, "warm"),
         (42, 106, "warm"), (37, 108, "warm"), (40, 100, "warm")],
        [(37, 103, "hot"), (40, 103, "hot"), (38, 105, "warm"), (41, 105, "warm"),
         (35, 107, "warm"), (42, 108, "warm"), (39, 101, "warm")],
        [(38, 102, "hot"), (41, 104, "hot"), (36, 104, "warm"), (39, 106, "warm"),
         (43, 107, "warm"), (35, 108, "warm"), (37, 100, "warm")],
    ]
    return tongues


def bubble_frames(n: int = 8) -> list[list[tuple[int, int, str]]]:
    """Bubbles rising through the flask liquid and vanishing at the surface.

    Three bubbles on staggered phases, so the liquid looks like it is working
    rather than pulsing in unison.
    """
    surface, floor = 66, 82
    columns = [(27, 0), (36, 3), (45, 5)]  # x, phase offset
    frames: list[list[tuple[int, int, str]]] = []
    for f in range(n):
        frame = []
        for x, phase in columns:
            step = (f + phase) % n
            y = floor - int(round(step * (floor - surface) / (n - 1)))
            if y > surface:  # a bubble that reached the surface has popped
                frame.append((x, y, "bubble"))
        frames.append(frame)
    return frames


def drip_frames(n: int = 8) -> list[list[tuple[int, int, str]]]:
    """One condensed drop leaving the spout and landing in the bottle."""
    x, top, land = 90, 80, 104
    frames = []
    for f in range(n):
        y = top + int(round(f * (land - top) / (n - 1)))
        frame = [(x, y, "drop")]
        if f == n - 1:  # the splash it makes on arrival
            frame += [(x - 1, land, "splash"), (x + 1, land, "splash")]
        frames.append(frame)
    return frames


def main() -> None:
    STATIC.mkdir(parents=True, exist_ok=True)

    base = build_base()
    base.save(STATIC / "still-base.png", optimize=True)

    layers = {
        "fire": fire_frames(),
        "bubbles": bubble_frames(),
        "drip": drip_frames(),
    }
    (STATIC / "still-anim.json").write_text(
        json.dumps({"grid": GRID, "layers": layers}, indent=2) + "\n")

    # An SVG fragment ready to paste into home.html: the base image and every
    # animated pixel share one coordinate system, so alignment cannot drift.
    out = ['<svg class="still" viewBox="0 0 128 128" role="img" aria-labelledby="still-title still-desc">',
           '  <title id="still-title">A still</title>',
           '  <desc id="still-desc">A pixel-art alembic: liquid simmers over a fire, vapour condenses in the coil, and a drop falls into the collecting bottle.</desc>',
           '  <image class="still__art" href="/static/still-base.png" x="0" y="0" width="128" height="128"/>']
    for layer, frames in layers.items():
        for i, frame in enumerate(frames):
            out.append(f'  <g class="lay lay--{layer} f{i}">')
            for x, y, cls in frame:
                out.append(f'    <rect class="p p--{cls}" x="{x}" y="{y}" width="1" height="1"/>')
            out.append("  </g>")
    out.append("</svg>")
    (STATIC / "still.svg.frag").write_text("\n".join(out) + "\n")

    # A 5x preview for eyeballing the result. It lives outside the served
    # directory: it is a development aid, not something the binary should carry.
    preview = ROOT / ".pixgen" / "still-preview.png"
    preview.parent.mkdir(exist_ok=True)
    base.resize((GRID * 5, GRID * 5), Image.NEAREST).save(preview)

    print(f"still-base.png   {(STATIC / 'still-base.png').stat().st_size:>6} bytes")
    print(f"still-anim.json  {(STATIC / 'still-anim.json').stat().st_size:>6} bytes")
    print("frames:", {k: len(v) for k, v in layers.items()})


if __name__ == "__main__":
    main()
