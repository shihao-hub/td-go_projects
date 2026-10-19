"""Generate high-quality icon.ico for docsearch-gui."""
from pathlib import Path
from PIL import Image, ImageDraw

def create_docsearch_icon(size: int = 512) -> Image.Image:
    img = Image.new("RGBA", (size, size), (0, 0, 0, 0))
    draw = ImageDraw.Draw(img)

    # 1. Base rounded rectangle (Dark background)
    pad = int(size * 0.04)
    radius = int(size * 0.22)
    bg_box = [pad, pad, size - pad, size - pad]
    draw.rounded_rectangle(bg_box, radius=radius, fill=(30, 30, 36, 255), outline=(50, 50, 62, 255), width=max(2, int(size * 0.01)))

    # 2. Document card
    doc_x0 = int(size * 0.20)
    doc_y0 = int(size * 0.18)
    doc_x1 = int(size * 0.65)
    doc_y1 = int(size * 0.78)
    draw.rounded_rectangle([doc_x0, doc_y0, doc_x1, doc_y1], radius=int(size * 0.04), fill=(40, 40, 50, 255), outline=(70, 70, 85, 255), width=max(1, int(size * 0.008)))

    # Document text lines
    line_x0 = doc_x0 + int(size * 0.06)
    line_w1 = int(size * 0.26)
    line_w2 = int(size * 0.32)
    line_h = max(2, int(size * 0.022))
    line_spacing = int(size * 0.055)
    cur_y = doc_y0 + int(size * 0.09)

    for i, w in enumerate([line_w1, line_w2, line_w2 - int(size * 0.04), line_w1, line_w2 - int(size * 0.06)]):
        draw.rounded_rectangle([line_x0, cur_y, line_x0 + w, cur_y + line_h], radius=line_h // 2, fill=(80, 85, 100, 220))
        cur_y += line_spacing

    # 3. Magnifying glass (Semantic search symbol)
    lens_cx = int(size * 0.62)
    lens_cy = int(size * 0.60)
    lens_r = int(size * 0.20)
    lens_box = [lens_cx - lens_r, lens_cy - lens_r, lens_cx + lens_r, lens_cy + lens_r]

    # Outer lens circle (Blue accent)
    draw.ellipse(lens_box, fill=(24, 24, 32, 240), outline=(59, 130, 246, 255), width=max(4, int(size * 0.035)))

    # Lightning / AI Spark in center of lens
    # Points scaled relative to center
    spark_scale = size * 0.0018
    spark_cx, spark_cy = lens_cx, lens_cy - int(size * 0.01)
    pts = [
        (spark_cx + 8 * spark_scale, spark_cy - 40 * spark_scale),
        (spark_cx - 24 * spark_scale, spark_cy + 5 * spark_scale),
        (spark_cx + 0 * spark_scale, spark_cy + 5 * spark_scale),
        (spark_cx - 15 * spark_scale, spark_cy + 42 * spark_scale),
        (spark_cx + 28 * spark_scale, spark_cy - 10 * spark_scale),
        (spark_cx + 5 * spark_scale, spark_cy - 10 * spark_scale),
    ]
    draw.polygon(pts, fill=(96, 165, 250, 255))

    # Magnifying glass handle
    handle_x0 = lens_cx + int(lens_r * 0.72)
    handle_y0 = lens_cy + int(lens_r * 0.72)
    handle_x1 = handle_x0 + int(size * 0.16)
    handle_y1 = handle_y0 + int(size * 0.16)
    draw.line([handle_x0, handle_y0, handle_x1, handle_y1], fill=(59, 130, 246, 255), width=max(6, int(size * 0.055)))

    return img

def main():
    out_dir = Path("D:/Users/language_projects/go_projects/docsearch-gui")
    out_dir.mkdir(parents=True, exist_ok=True)
    
    img_512 = create_docsearch_icon(512)
    img_512.save(out_dir / "icon.png", format="PNG")
    
    # Save multi-size ICO
    sizes = [(256, 256), (128, 128), (64, 64), (48, 48), (32, 32), (24, 24), (16, 16)]
    ico_images = [img_512.resize(s, Image.Resampling.LANCZOS) for s in sizes]
    ico_images[0].save(
        out_dir / "icon.ico",
        format="ICO",
        sizes=sizes,
        append_images=ico_images[1:],
    )
    print("Icon generated successfully:", out_dir / "icon.ico")

if __name__ == "__main__":
    main()
