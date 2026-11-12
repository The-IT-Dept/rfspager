#!/usr/bin/env python3
"""Compose App Store / Play marketing cards and the README strip.

Input: raw simulator screenshots (1320x2868, iPhone 17 Pro Max) and a
manifest of headlines. Output: one card per screenshot at App Store 6.9"
size, plus a downscaled strip of all cards for the README.

    python3 apps/mobile/scripts/compose-store-screenshots.py \\
        --manifest apps/mobile/store/screenshots.json \\
        --raw ~/rfs-shots/raw --out ~/rfs-shots/cards \\
        --strip docs/images/app-screenshots.png

The manifest is a list of {"file", "headline", "subhead"}; cards are written
in manifest order as 01-<file>.png, 02-... . Needs Pillow. Fonts are the
bundled Poppins (OFL, see assets/fonts/FONT-NOTICES.md).
"""
import argparse
import json
from pathlib import Path

from PIL import Image, ImageDraw, ImageFilter, ImageFont

W, H = 1320, 2868
BRAND = (255, 16, 3)
FONTS = Path(__file__).resolve().parent.parent / "assets" / "fonts"


def gradient_background() -> Image.Image:
    top, bottom = (28, 6, 8), (10, 10, 12)
    bg = Image.new("RGB", (W, H))
    d = ImageDraw.Draw(bg)
    for y in range(H):
        t = y / (H - 1)
        d.line([(0, y), (W, y)], fill=tuple(round(top[i] + (bottom[i] - top[i]) * t) for i in range(3)))
    # Red glow behind the phone.
    glow = Image.new("RGBA", (W, H), (0, 0, 0, 0))
    ImageDraw.Draw(glow).ellipse((-200, 900, W + 200, 2600), fill=BRAND + (120,))
    glow = glow.filter(ImageFilter.GaussianBlur(260))
    bg.paste(glow, (0, 0), glow)
    return bg


def wrap(draw, text, font, max_width):
    words, lines, line = text.split(), [], ""
    for w in words:
        candidate = (line + " " + w).strip()
        if draw.textlength(candidate, font=font) <= max_width:
            line = candidate
        else:
            lines.append(line)
            line = w
    if line:
        lines.append(line)
    return lines


def balanced(draw, text, font, max_width):
    """Wrap, then even out a two-line wrap so no word dangles on its own."""
    lines = wrap(draw, text, font, max_width)
    if len(lines) != 2:
        return lines
    words = text.split()
    best = None
    for i in range(1, len(words)):
        a, b = " ".join(words[:i]), " ".join(words[i:])
        w = max(draw.textlength(a, font=font), draw.textlength(b, font=font))
        if w <= max_width and (best is None or w < best[0]):
            best = (w, [a, b])
    return best[1] if best else lines


def rounded_mask(size, radius):
    m = Image.new("L", size, 0)
    ImageDraw.Draw(m).rounded_rectangle((0, 0, size[0] - 1, size[1] - 1), radius, fill=255)
    return m


def phone(screen: Image.Image, width: int) -> Image.Image:
    """Screenshot inside a dark bezel with rounded corners."""
    scale = width / screen.width
    sw, sh = width, round(screen.height * scale)
    screen = screen.convert("RGB").resize((sw, sh), Image.LANCZOS)
    bezel = round(width * 0.035)
    radius = round(width * 0.16)
    fw, fh = sw + 2 * bezel, sh + 2 * bezel
    frame = Image.new("RGBA", (fw, fh), (0, 0, 0, 0))
    d = ImageDraw.Draw(frame)
    d.rounded_rectangle((0, 0, fw - 1, fh - 1), radius + bezel, fill=(22, 22, 25, 255),
                        outline=(70, 70, 76, 255), width=4)
    frame.paste(screen, (bezel, bezel), rounded_mask((sw, sh), radius))
    return frame


def card(screen: Image.Image, headline: str, subhead: str) -> Image.Image:
    img = gradient_background().convert("RGBA")
    d = ImageDraw.Draw(img)
    h_font = ImageFont.truetype(str(FONTS / "Poppins-Bold.ttf"), 112)
    s_font = ImageFont.truetype(str(FONTS / "Poppins-Regular.ttf"), 52)

    y = 190
    for line in balanced(d, headline, h_font, W - 160):
        d.text((W / 2, y), line, font=h_font, fill="white", anchor="ma")
        y += 136
    y += 24
    for line in balanced(d, subhead, s_font, W - 180):
        d.text((W / 2, y), line, font=s_font, fill=(205, 205, 212), anchor="ma")
        y += 74

    device = phone(screen, 1000)
    top = max(y + 90, 780)
    shadow = Image.new("RGBA", img.size, (0, 0, 0, 0))
    sx = (W - device.width) // 2
    ImageDraw.Draw(shadow).rounded_rectangle(
        (sx + 10, top + 40, sx + device.width - 10, top + device.height + 40), 190, fill=(0, 0, 0, 170))
    shadow = shadow.filter(ImageFilter.GaussianBlur(50))
    img = Image.alpha_composite(img, shadow)
    img.paste(device, (sx, top), device)  # runs off the bottom edge on purpose
    return img.convert("RGB")


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--manifest", required=True)
    ap.add_argument("--raw", required=True, help="directory of raw screenshots")
    ap.add_argument("--out", required=True, help="directory for full-size cards")
    ap.add_argument("--strip", help="README strip PNG to write")
    ap.add_argument("--strip-width", type=int, default=1800)
    args = ap.parse_args()

    entries = json.loads(Path(args.manifest).read_text())
    out = Path(args.out)
    out.mkdir(parents=True, exist_ok=True)
    cards = []
    for i, e in enumerate(entries, 1):
        c = card(Image.open(Path(args.raw) / e["file"]), e["headline"], e["subhead"])
        path = out / f"{i:02d}-{Path(e['file']).stem}.png"
        c.save(path, optimize=True)
        cards.append(c)
        print("wrote", path)

    if args.strip:
        gap = 24
        cw = (args.strip_width - gap * (len(cards) - 1)) // len(cards)
        ch = round(H * cw / W)
        strip = Image.new("RGBA", (cw * len(cards) + gap * (len(cards) - 1), ch), (0, 0, 0, 0))
        for i, c in enumerate(cards):
            t = c.resize((cw, ch), Image.LANCZOS).convert("RGBA")
            strip.paste(t, (i * (cw + gap), 0), rounded_mask((cw, ch), round(cw * 0.06)))
        Path(args.strip).parent.mkdir(parents=True, exist_ok=True)
        strip.save(args.strip, optimize=True)
        print("wrote", args.strip)


if __name__ == "__main__":
    main()
