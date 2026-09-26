import math
import os
import numpy as np
from PIL import Image, ImageDraw

def render_vector_icon(size=1024, style='cyan_coral', shape='squircle'):
    scale = 4
    dim = size * scale
    
    y, x = np.ogrid[:dim, :dim]
    cx, cy = dim / 2.0, dim / 2.0
    
    pad = 72 * scale
    r_corner = 200 * scale
    
    # 1. Background image
    bg_img = Image.new('RGBA', (dim, dim), (0, 0, 0, 0))
    draw_bg = ImageDraw.Draw(bg_img)
    
    # Background slate: #0B1120 / #0F172A
    slate_fill = (15, 23, 42, 255)
    border_col = (30, 41, 59, 255) # slate-800
    
    if shape == 'squircle':
        rect_box = [pad, pad, dim - pad, dim - pad]
        draw_bg.rounded_rectangle(rect_box, radius=r_corner, fill=slate_fill)
        draw_bg.rounded_rectangle(rect_box, radius=r_corner, outline=border_col, width=int(14 * scale))
    else:
        circle_r = (dim / 2.0) - pad
        draw_bg.ellipse([cx - circle_r, cy - circle_r, cx + circle_r, cy + circle_r], fill=slate_fill, outline=border_col, width=int(14 * scale))
        
    bg_arr = np.array(bg_img, dtype=np.float32) / 255.0
    
    # 2. Geometry
    dist = np.sqrt((x - cx)**2 + (y - cy)**2)
    # Clock angle: 0 at 12 o'clock, increasing clockwise to 2*pi
    angle = np.mod(np.arctan2(x - cx, cy - y), 2 * np.pi)
    
    mid_r = 295.0 * scale
    stroke_w = 90.0 * scale
    stroke_half = stroke_w / 2.0
    
    dist_ring = np.abs(dist - mid_r)
    ring_aa = np.clip(0.5 - (dist_ring - stroke_half) / (1.5 * scale), 0.0, 1.0)
    
    # Track base color
    track_color = np.array([30, 41, 59, 255], dtype=np.float32) / 255.0
    
    # 70% active = 0.70 * 2 * pi = 252 degrees
    total_active_rad = 0.70 * 2 * np.pi
    
    # End caps centers
    # 12 o'clock cap (angle = 0)
    cap0_dist = np.sqrt((x - cx)**2 + (y - (cy - mid_r))**2)
    cap0_mask = np.clip(0.5 - (cap0_dist - stroke_half) / (1.5 * scale), 0.0, 1.0)
    
    # 70% cap (angle = total_active_rad)
    cap70_x = cx + mid_r * np.sin(total_active_rad)
    cap70_y = cy - mid_r * np.cos(total_active_rad)
    cap70_dist = np.sqrt((x - cap70_x)**2 + (y - cap70_y)**2)
    cap70_mask = np.clip(0.5 - (cap70_dist - stroke_half) / (1.5 * scale), 0.0, 1.0)
    
    # Base track is full circle
    gauge_layer = track_color * ring_aa[:, :, None]
    
    if style == 'cyan_coral':
        # Electric Cyan: #00F0FF (0, 240, 255)
        # Coral Accent:  #FF5C4D (255, 92, 77)
        col_main = np.array([0, 240, 255, 255], dtype=np.float32) / 255.0
        col_accent = np.array([255, 92, 77, 255], dtype=np.float32) / 255.0
        split_rad = 0.52 * 2 * np.pi  # 0% to 52% cyan, 52% to 70% coral warning
        
        main_mask = ring_aa * np.clip(1.0 - (angle - split_rad) / 0.005, 0.0, 1.0) * np.clip((angle - 0) / 0.005, 0.0, 1.0)
        main_mask = np.maximum(main_mask, cap0_mask)
        
        accent_mask = ring_aa * np.clip((angle - split_rad) / 0.005, 0.0, 1.0) * np.clip(1.0 - (angle - total_active_rad) / 0.005, 0.0, 1.0)
        accent_mask = np.maximum(accent_mask, cap70_mask)
        
        gauge_layer = gauge_layer * (1.0 - main_mask[:, :, None]) + col_main * main_mask[:, :, None]
        gauge_layer = gauge_layer * (1.0 - accent_mask[:, :, None]) + col_accent * accent_mask[:, :, None]
        
    elif style == 'mint_amber':
        # Mint Green:    #10F090 (16, 240, 144)
        # Amber Warning: #F59E0B (245, 158, 11)
        col_main = np.array([16, 240, 144, 255], dtype=np.float32) / 255.0
        col_accent = np.array([245, 158, 11, 255], dtype=np.float32) / 255.0
        split_rad = 0.52 * 2 * np.pi
        
        main_mask = ring_aa * np.clip(1.0 - (angle - split_rad) / 0.005, 0.0, 1.0) * np.clip((angle - 0) / 0.005, 0.0, 1.0)
        main_mask = np.maximum(main_mask, cap0_mask)
        
        accent_mask = ring_aa * np.clip((angle - split_rad) / 0.005, 0.0, 1.0) * np.clip(1.0 - (angle - total_active_rad) / 0.005, 0.0, 1.0)
        accent_mask = np.maximum(accent_mask, cap70_mask)
        
        gauge_layer = gauge_layer * (1.0 - main_mask[:, :, None]) + col_main * main_mask[:, :, None]
        gauge_layer = gauge_layer * (1.0 - accent_mask[:, :, None]) + col_accent * accent_mask[:, :, None]

    elif style == 'electric_cyan_pip':
        # Electric cyan throughout 70%, with a crisp amber/coral indicator pip at 70%
        col_main = np.array([0, 240, 255, 255], dtype=np.float32) / 255.0
        col_accent = np.array([255, 92, 77, 255], dtype=np.float32) / 255.0
        
        active_mask = ring_aa * np.clip(1.0 - (angle - total_active_rad) / 0.005, 0.0, 1.0) * np.clip((angle - 0) / 0.005, 0.0, 1.0)
        active_mask = np.maximum(active_mask, cap0_mask)
        active_mask = np.maximum(active_mask, cap70_mask)
        
        gauge_layer = gauge_layer * (1.0 - active_mask[:, :, None]) + col_main * active_mask[:, :, None]
        gauge_layer = gauge_layer * (1.0 - cap70_mask[:, :, None]) + col_accent * cap70_mask[:, :, None]

    # Composite gauge over background
    final_arr = bg_arr.copy()
    gauge_alpha = gauge_layer[:, :, 3:4]
    final_arr[:, :, :3] = final_arr[:, :, :3] * (1.0 - gauge_alpha) + gauge_layer[:, :, :3] * gauge_alpha
    final_arr[:, :, 3:4] = np.maximum(final_arr[:, :, 3:4], gauge_alpha)
    final_arr[:, :, 3] = final_arr[:, :, 3] * bg_arr[:, :, 3]
    
    img_large = Image.fromarray((np.clip(final_arr, 0.0, 1.0) * 255.0).astype(np.uint8), mode='RGBA')
    img_out = img_large.resize((size, size), Image.Resampling.LANCZOS)
    return img_out

def save_all_variants():
    build_dir = r'D:\Users\language_projects\go_projects\glmquotawatch-gui\build'
    brain_dir = r'C:\Users\29580\.gemini\antigravity-acp\brain\0ed9efac-7410-4b0e-b220-47f0c320d977'
    os.makedirs(brain_dir, exist_ok=True)
    
    designs = [
        ('icon_cyan_coral.png', 'cyan_coral', 'squircle'),
        ('icon_mint_amber.png', 'mint_amber', 'squircle'),
        ('icon_cyan_pip.png', 'electric_cyan_pip', 'squircle'),
        ('icon_cyan_coral_circle.png', 'cyan_coral', 'circle')
    ]
    
    for filename, style, shape in designs:
        im = render_vector_icon(1024, style=style, shape=shape)
        p1 = os.path.join(build_dir, filename)
        p2 = os.path.join(brain_dir, filename)
        im.save(p1, 'PNG')
        im.save(p2, 'PNG')
        print(f'Saved {filename} (1024x1024 RGBA)')
        
    # Also generate ICO file for the primary recommended design (icon_cyan_coral.png)
    prim = Image.open(os.path.join(brain_dir, 'icon_cyan_coral.png'))
    ico_sizes = [(16, 16), (24, 24), (32, 32), (48, 48), (64, 64), (128, 128), (256, 256)]
    ico_path = os.path.join(brain_dir, 'glmquotawatch.ico')
    ico_path_build = os.path.join(build_dir, 'glmquotawatch.ico')
    prim.save(ico_path, format='ICO', sizes=ico_sizes)
    prim.save(ico_path_build, format='ICO', sizes=ico_sizes)
    print(f'Generated multi-res ICO with sizes: {ico_sizes}')
    
    # Generate small preview grid to verify 16px, 24px, 32px, 48px tray legibility
    preview = Image.new('RGBA', (600, 160), (30, 30, 30, 255))
    sizes = [16, 24, 32, 48, 64, 128]
    cur_x = 20
    draw_prev = ImageDraw.Draw(preview)
    for sz in sizes:
        resized = prim.resize((sz, sz), Image.Resampling.LANCZOS)
        y_pos = (160 - sz) // 2
        preview.paste(resized, (cur_x, y_pos), resized)
        cur_x += sz + 20
    preview.save(os.path.join(brain_dir, 'tray_legibility_test.png'), 'PNG')
    preview.save(os.path.join(build_dir, 'tray_legibility_test.png'), 'PNG')
    print('Generated tray_legibility_test.png')

if __name__ == '__main__':
    save_all_variants()
