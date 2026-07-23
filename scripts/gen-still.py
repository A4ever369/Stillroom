#!/usr/bin/env python3
"""Generate the Stillroom hero as deterministic, grid-native pixel art.

The drawing is authored directly on a 96x88 pixel grid.  Pillow is only used
for integer-coordinate raster primitives and nearest-neighbour enlargement;
there is no antialiasing, scaling, randomness, or model-generated imagery.
"""

from __future__ import annotations

import json
from collections import OrderedDict
from pathlib import Path
from typing import Iterable

from PIL import Image, ImageDraw


GRID = (96, 88)
SCALE = 6
ROOT = Path(__file__).resolve().parents[1]
OUTPUT_DIR = ROOT / "cmd" / "stillroom-hub" / "web" / "static"

# Keep this ordered: the JSON doubles as the palette contract for the web UI.
PALETTE = OrderedDict(
    [
        ("ground", "#010102"),
        ("outline", "#08090d"),
        ("glass_dark", "#171d2a"),
        ("glass_mid", "#34405a"),
        ("glass_light", "#d8e2ff"),
        ("glass_rim", "#8290b8"),
        ("metal_dark", "#20232d"),
        ("metal_mid", "#4c5264"),
        ("metal_light", "#aab3ca"),
        ("liquid_dark", "#242458"),
        ("liquid_mid", "#44469a"),
        ("liquid_bright", "#828fff"),
        ("fire_core", "#fff1d6"),
        ("fire_mid", "#ffad5c"),
        ("fire_dim", "#a8464f"),
        ("stone_dark", "#15171d"),
        ("stone_mid", "#34343a"),
        ("stone_light", "#62666d"),
        ("accent", "#5e6ad2"),
        ("spark", "#f7f8f8"),
    ]
)

RGBA = {name: tuple(bytes.fromhex(value[1:])) + (255,) for name, value in PALETTE.items()}


def line(
    draw: ImageDraw.ImageDraw,
    points: Iterable[tuple[int, int]],
    role: str,
    width: int = 1,
) -> None:
    draw.line(list(points), fill=RGBA[role], width=width, joint="curve")


def rect(
    draw: ImageDraw.ImageDraw,
    box: tuple[int, int, int, int],
    role: str,
) -> None:
    draw.rectangle(box, fill=RGBA[role])


def polygon(
    draw: ImageDraw.ImageDraw,
    points: Iterable[tuple[int, int]],
    role: str,
) -> None:
    draw.polygon(list(points), fill=RGBA[role])


def ellipse(
    draw: ImageDraw.ImageDraw,
    box: tuple[int, int, int, int],
    role: str,
) -> None:
    draw.ellipse(box, fill=RGBA[role])


def draw_grounding(draw: ImageDraw.ImageDraw) -> None:
    """Sparse, non-rectangular contact marks keep the apparatus grounded."""
    rect(draw, (7, 83, 52, 84), "stone_dark")
    rect(draw, (11, 82, 47, 82), "metal_dark")
    rect(draw, (70, 84, 93, 84), "stone_dark")
    rect(draw, (74, 83, 91, 83), "metal_dark")
    rect(draw, (4, 83, 5, 84), "glass_dark")
    rect(draw, (55, 84, 59, 84), "glass_dark")
    rect(draw, (65, 84, 67, 84), "glass_dark")


def draw_brazier(draw: ImageDraw.ImageDraw) -> None:
    """Stone brazier, including an empty opening reserved for fire frames."""
    # Top cap and its bevel.
    rect(draw, (13, 59, 49, 61), "outline")
    rect(draw, (14, 60, 48, 63), "stone_dark")
    rect(draw, (16, 60, 46, 60), "stone_light")
    rect(draw, (16, 61, 47, 62), "stone_mid")
    rect(draw, (18, 63, 45, 64), "metal_dark")

    # Splayed stone body.
    polygon(draw, [(17, 64), (45, 64), (48, 79), (13, 79)], "outline")
    polygon(draw, [(19, 65), (43, 65), (46, 77), (16, 77)], "stone_mid")
    polygon(draw, [(19, 65), (23, 65), (20, 77), (16, 77)], "stone_light")
    polygon(draw, [(40, 65), (43, 65), (46, 77), (42, 77)], "stone_dark")

    # Fire chamber: deliberately near-black so animated flame silhouettes pop.
    polygon(draw, [(23, 68), (39, 68), (42, 77), (20, 77)], "outline")
    polygon(draw, [(25, 69), (37, 69), (40, 77), (22, 77)], "ground")
    rect(draw, (21, 76, 40, 78), "stone_dark")
    rect(draw, (13, 78, 49, 81), "outline")
    rect(draw, (14, 78, 48, 79), "stone_light")
    rect(draw, (12, 80, 50, 82), "stone_mid")
    rect(draw, (16, 80, 46, 80), "stone_light")
    rect(draw, (10, 82, 52, 83), "outline")


def draw_flask(draw: ImageDraw.ImageDraw) -> None:
    """Round-bottom flask with clipped liquid and glass reflections."""
    # Flask body, built in nested tones for a substantial glass wall.
    ellipse(draw, (8, 27, 52, 67), "outline")
    ellipse(draw, (10, 29, 50, 65), "glass_rim")
    ellipse(draw, (12, 30, 49, 64), "glass_dark")
    polygon(
        draw,
        [(11, 46), (50, 46), (50, 54), (46, 61), (40, 65), (20, 65), (14, 60), (11, 54)],
        "liquid_dark",
    )
    polygon(
        draw,
        [(13, 49), (48, 49), (47, 56), (43, 62), (37, 64), (21, 64), (16, 59), (13, 54)],
        "liquid_mid",
    )
    rect(draw, (12, 46, 49, 47), "outline")
    rect(draw, (14, 47, 47, 48), "accent")
    rect(draw, (17, 49, 45, 49), "liquid_bright")

    # Reassert the glass outline over the liquid fill.
    line(draw, [(10, 49), (10, 42), (13, 35), (20, 30), (27, 27)], "glass_light")
    line(draw, [(50, 42), (51, 50), (49, 58), (43, 64), (36, 67)], "glass_mid")
    line(draw, [(12, 53), (14, 59), (20, 64), (25, 66)], "glass_rim")
    rect(draw, (15, 38, 17, 43), "glass_light")
    rect(draw, (18, 34, 20, 38), "glass_light")
    rect(draw, (21, 31, 23, 33), "glass_rim")
    rect(draw, (16, 44, 18, 46), "glass_rim")
    rect(draw, (43, 35, 44, 43), "glass_mid")
    rect(draw, (46, 42, 47, 45), "glass_rim")

    # Short neck and heavy rolled rim.
    rect(draw, (25, 21, 36, 31), "outline")
    rect(draw, (27, 21, 34, 30), "glass_dark")
    rect(draw, (28, 22, 29, 29), "glass_light")
    rect(draw, (33, 23, 34, 30), "glass_mid")
    rect(draw, (23, 19, 38, 22), "outline")
    rect(draw, (24, 18, 37, 20), "glass_rim")
    rect(draw, (26, 18, 35, 18), "glass_light")
    rect(draw, (25, 21, 36, 22), "glass_mid")


def draw_swan_neck(draw: ImageDraw.ImageDraw) -> None:
    """Arched glass vapour neck connecting flask to condenser."""
    path = [(30, 20), (30, 14), (34, 8), (40, 5), (62, 5), (68, 8), (71, 13), (71, 20)]
    line(draw, path, "outline", 7)
    line(draw, path, "glass_rim", 5)
    line(draw, path, "glass_mid", 3)
    line(draw, [(31, 18), (31, 14), (35, 9), (41, 7), (61, 7)], "glass_light", 1)
    line(draw, [(63, 7), (67, 10), (69, 14), (69, 18)], "glass_light", 1)
    line(draw, [(39, 8), (61, 8)], "accent", 1)

    # Coupling above the condenser.
    rect(draw, (67, 18, 77, 20), "outline")
    rect(draw, (68, 18, 76, 18), "metal_light")
    rect(draw, (66, 20, 79, 23), "outline")
    rect(draw, (68, 20, 77, 21), "metal_mid")
    rect(draw, (69, 20, 75, 20), "metal_light")


def draw_condenser(draw: ImageDraw.ImageDraw) -> None:
    """A readable continuous cooling coil wrapped around a central tube."""
    # Dark glass body behind the coil establishes the condenser's volume.
    rect(draw, (69, 23, 77, 53), "outline")
    rect(draw, (71, 23, 75, 52), "glass_dark")
    rect(draw, (72, 24, 72, 50), "glass_mid")
    rect(draw, (73, 24, 73, 48), "glass_rim")

    # One continuous serpent: alternate end turns make the spiral unmistakable.
    coil = [
        (69, 24),
        (80, 24),
        (83, 26),
        (83, 28),
        (80, 29),
        (66, 29),
        (63, 31),
        (63, 33),
        (66, 34),
        (80, 34),
        (83, 36),
        (83, 38),
        (80, 39),
        (66, 39),
        (63, 41),
        (63, 43),
        (66, 44),
        (80, 44),
        (83, 46),
        (83, 48),
        (80, 49),
        (67, 49),
        (65, 51),
        (65, 54),
    ]
    line(draw, coil, "outline", 5)
    line(draw, coil, "metal_mid", 3)

    # Bright front faces and dark undersides describe round metal tubing.
    for y in (24, 34, 44):
        line(draw, [(69, y), (79, y)], "metal_light")
    for y in (29, 39, 49):
        line(draw, [(67, y), (79, y)], "metal_light")
    for x1, x2, y in ((70, 77, 25), (68, 75, 30), (70, 77, 35), (68, 75, 40), (70, 77, 45), (68, 75, 50)):
        line(draw, [(x1, y), (x2, y)], "accent")
    for y in (30, 35, 40, 45, 50):
        rect(draw, (67, y, 80, y), "metal_dark")
    for x, y in ((82, 26), (64, 31), (82, 36), (64, 41), (82, 46), (65, 51)):
        rect(draw, (x, y, x, y + 1), "metal_light")

    # Bottom coupling and outlet into the delivery spout.
    rect(draw, (62, 53, 68, 56), "outline")
    rect(draw, (64, 53, 66, 55), "metal_light")
    line(draw, [(66, 55), (74, 55), (77, 57)], "outline", 5)
    line(draw, [(66, 54), (74, 54), (78, 57)], "glass_mid", 3)
    line(draw, [(67, 53), (74, 53), (78, 56)], "glass_rim")


def draw_spout(draw: ImageDraw.ImageDraw) -> None:
    """Bent delivery spout ending directly above the receiver."""
    path = [(78, 56), (82, 57), (84, 60), (84, 64)]
    line(draw, path, "outline", 5)
    line(draw, path, "glass_mid", 3)
    line(draw, [(79, 55), (82, 56), (83, 59)], "glass_light")
    rect(draw, (82, 63, 86, 65), "outline")
    rect(draw, (83, 63, 85, 64), "glass_rim")


def draw_receiver(draw: ImageDraw.ImageDraw) -> None:
    """Small receiving bottle with a visible collected fraction."""
    # Neck and rolled lip.
    rect(draw, (80, 67, 87, 72), "outline")
    rect(draw, (82, 67, 85, 72), "glass_dark")
    rect(draw, (82, 68, 82, 71), "glass_light")
    rect(draw, (78, 65, 89, 68), "outline")
    rect(draw, (79, 65, 88, 66), "glass_rim")
    rect(draw, (81, 65, 86, 65), "glass_light")

    # Squat bottle body.
    polygon(draw, [(79, 70), (88, 70), (92, 74), (92, 84), (75, 84), (75, 74)], "outline")
    polygon(draw, [(80, 71), (87, 71), (90, 74), (90, 82), (77, 82), (77, 74)], "glass_dark")
    polygon(draw, [(77, 77), (90, 77), (90, 82), (77, 82)], "liquid_dark")
    rect(draw, (78, 78, 89, 81), "liquid_mid")
    rect(draw, (79, 77, 88, 77), "accent")
    rect(draw, (79, 73, 80, 79), "glass_light")
    rect(draw, (81, 71, 82, 72), "glass_light")
    rect(draw, (88, 75, 89, 80), "glass_mid")
    rect(draw, (76, 83, 91, 84), "glass_rim")
    rect(draw, (79, 83, 89, 83), "glass_light")


def make_base() -> Image.Image:
    image = Image.new("RGBA", GRID, (0, 0, 0, 0))
    draw = ImageDraw.Draw(image)
    draw_grounding(draw)
    draw_brazier(draw)
    # The vapour neck sits behind the rolled flask mouth at their join.
    draw_swan_neck(draw)
    draw_flask(draw)
    draw_condenser(draw)
    draw_spout(draw)
    draw_receiver(draw)
    return image


def put_spans(
    pixels: dict[tuple[int, int], str],
    role: str,
    spans: dict[int, list[tuple[int, int]]],
) -> None:
    """Paint inclusive horizontal spans into an animation-frame pixel map."""
    for y, ranges in spans.items():
        for x1, x2 in ranges:
            for x in range(x1, x2 + 1):
                pixels[(x, y)] = role


def flame_frame(index: int) -> list[list[int | str]]:
    frames = [
        {
            "dim": {76: [(24, 38)], 75: [(25, 37)], 74: [(25, 36)], 73: [(26, 35)], 72: [(26, 28), (31, 34), (36, 36)], 71: [(27, 27), (31, 33), (36, 36)], 70: [(32, 33)], 69: [(32, 32)]},
            "mid": {76: [(27, 35)], 75: [(28, 34)], 74: [(28, 33)], 73: [(29, 33)], 72: [(29, 30), (32, 33)], 71: [(32, 32)]},
            "core": {76: [(30, 33)], 75: [(30, 32)], 74: [(31, 32)]},
        },
        {
            "dim": {76: [(24, 38)], 75: [(25, 38)], 74: [(26, 37)], 73: [(26, 35), (37, 37)], 72: [(27, 29), (31, 35), (37, 37)], 71: [(28, 28), (32, 35)], 70: [(33, 34)], 69: [(34, 34)]},
            "mid": {76: [(27, 35)], 75: [(28, 35)], 74: [(28, 34)], 73: [(29, 31), (33, 34)], 72: [(30, 30), (33, 34)], 71: [(33, 33)]},
            "core": {76: [(30, 33)], 75: [(30, 33)], 74: [(31, 32)], 73: [(32, 32)]},
        },
        {
            "dim": {76: [(23, 38)], 75: [(24, 37)], 74: [(25, 36)], 73: [(25, 28), (30, 35)], 72: [(26, 28), (31, 34)], 71: [(27, 27), (31, 33)], 70: [(31, 32)], 69: [(31, 31)]},
            "mid": {76: [(27, 35)], 75: [(27, 34)], 74: [(28, 33)], 73: [(28, 29), (31, 32)], 72: [(29, 29), (31, 31)]},
            "core": {76: [(30, 33)], 75: [(30, 32)], 74: [(30, 31)]},
        },
        {
            "dim": {76: [(24, 39)], 75: [(25, 38)], 74: [(25, 37)], 73: [(26, 29), (31, 36)], 72: [(27, 29), (32, 35)], 71: [(28, 28), (32, 34)], 70: [(33, 34)], 69: [(33, 33)]},
            "mid": {76: [(27, 36)], 75: [(28, 35)], 74: [(28, 34)], 73: [(29, 30), (32, 33)], 72: [(30, 30), (32, 32)]},
            "core": {76: [(30, 33)], 75: [(30, 33)], 74: [(31, 32)], 73: [(31, 31)]},
        },
    ]
    pixels: dict[tuple[int, int], str] = {}
    put_spans(pixels, "fire_dim", frames[index]["dim"])
    put_spans(pixels, "fire_mid", frames[index]["mid"])
    put_spans(pixels, "fire_core", frames[index]["core"])
    return [[x, y, role] for (x, y), role in pixels.items()]


def bubble_frame(index: int) -> list[list[int | str]]:
    tracks = [
        [(21, 57), (27, 53), (36, 59), (41, 54), (32, 62)],
        [(21, 55), (27, 51), (36, 57), (41, 52), (32, 60)],
        [(21, 53), (27, 58), (36, 55), (41, 50), (32, 58)],
        [(21, 51), (27, 56), (36, 53), (41, 58), (32, 56)],
    ]
    pixels: dict[tuple[int, int], str] = {}
    for bubble_index, (x, y) in enumerate(tracks[index]):
        pixels[(x, y)] = "liquid_bright"
        if bubble_index in (1, 3):
            pixels[(x + 1, y)] = "glass_light"
    return [[x, y, role] for (x, y), role in pixels.items()]


def drip_frame(index: int) -> list[list[int | str]]:
    frames = [
        [(84, 65, "liquid_bright")],
        [(84, 66, "liquid_bright"), (84, 67, "accent")],
        [(84, 68, "liquid_bright"), (84, 69, "accent")],
        [(82, 70, "accent"), (84, 69, "liquid_bright"), (86, 70, "accent")],
    ]
    return [[x, y, role] for x, y, role in frames[index]]


def make_animation() -> dict[str, object]:
    return {
        "grid": list(GRID),
        "palette": PALETTE,
        "layers": {
            "fire": [flame_frame(i) for i in range(4)],
            "bubbles": [bubble_frame(i) for i in range(4)],
            "drip": [drip_frame(i) for i in range(4)],
        },
    }


def composite_frame(
    base: Image.Image,
    animation: dict[str, object],
    choices: dict[str, int],
) -> Image.Image:
    preview = Image.new("RGBA", GRID, RGBA["ground"])
    preview.alpha_composite(base)
    draw = ImageDraw.Draw(preview)
    layers = animation["layers"]
    assert isinstance(layers, dict)
    for layer_name, frame_index in choices.items():
        frames = layers[layer_name]
        for x, y, role in frames[frame_index]:
            draw.point((x, y), fill=RGBA[role])
    return preview


def main() -> None:
    OUTPUT_DIR.mkdir(parents=True, exist_ok=True)
    base = make_base()
    animation = make_animation()

    base.save(OUTPUT_DIR / "still-base.png", optimize=False)
    (OUTPUT_DIR / "still-anim.json").write_text(
        json.dumps(animation, indent=2) + "\n",
        encoding="utf-8",
    )

    preview = composite_frame(base, animation, {"fire": 1, "bubbles": 2, "drip": 2})
    preview = preview.convert("RGB").resize(
        (GRID[0] * SCALE, GRID[1] * SCALE),
        resample=Image.Resampling.NEAREST,
    )
    preview.save(OUTPUT_DIR / "still-preview.png", optimize=False)


if __name__ == "__main__":
    main()
