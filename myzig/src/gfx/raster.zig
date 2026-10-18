//! The CPU renderer: draws a scene into memory, shapes with their coverage
//! computed per pixel from signed distances, and text through the text
//! system, which draws into the same pixels.
const std = @import("std");
const gfx = @import("../gfx.zig");
const platform = @import("../platform.zig");
const text = @import("../text.zig");

pub fn draw(target: *const platform.Target, scene: *const gfx.Scene, ts: text.System) void {
    clear(target, scene.background);
    for (scene.ops.items) |op| switch (op) {
        .rect => |r| fillRect(target, r.rect, r.radius, r.fill, r.border_width, r.border),
        .text => |t| ts.draw(target, t.run, t.x, t.y, t.color),
    };
}

fn clear(t: *const platform.Target, col: gfx.Color) void {
    const px = premultiply(col, 1);
    for (0..t.height) |y| @memset(t.pixels[y * t.stride ..][0..t.width], pack(px));
}

/// Fills r, with corners of the radius, and a border inside it, in DIPs.
/// Edges snap to device pixels, as borders and fills look sharp there.
pub fn fillRect(t: *const platform.Target, r: gfx.Rect, radius: f32, fill: gfx.Color, border_width: f32, border: gfx.Color) void {
    const s = t.scale;
    const x0 = @round(r.x * s);
    const y0 = @round(r.y * s);
    const x1 = @round((r.x + r.w) * s);
    const y1 = @round((r.y + r.h) * s);
    if (x1 <= x0 or y1 <= y0) return;
    const hx = (x1 - x0) / 2;
    const hy = (y1 - y0) / 2;
    const cx = x0 + hx;
    const cy = y0 + hy;
    const rad = @min(radius * s, @min(hx, hy));
    const bw: f32 = if (border_width > 0 and border.a > 0) @max(1, @round(border_width * s)) else 0;
    const ihx = @max(hx - bw, 0);
    const ihy = @max(hy - bw, 0);
    const irad = @max(rad - bw, 0);

    const f = premultiply(fill, 1);
    const b = premultiply(border, 1);
    const opaque_fill = fill.a == 255;
    const solid = pack(f);

    const w_f: f32 = @floatFromInt(t.width);
    const h_f: f32 = @floatFromInt(t.height);
    const px0: usize = @intFromFloat(std.math.clamp(@floor(x0), 0, w_f));
    const px1: usize = @intFromFloat(std.math.clamp(@ceil(x1), 0, w_f));
    const py0: usize = @intFromFloat(std.math.clamp(@floor(y0), 0, h_f));
    const py1: usize = @intFromFloat(std.math.clamp(@ceil(y1), 0, h_f));
    for (py0..py1) |y| {
        const dy = @abs(@as(f32, @floatFromInt(y)) + 0.5 - cy);
        const row = t.pixels[y * t.stride ..][0..t.width];
        for (px0..px1) |x| {
            const dx = @abs(@as(f32, @floatFromInt(x)) + 0.5 - cx);
            const outer = coverage(dx, dy, hx, hy, rad);
            if (outer == 0) continue;
            const inner = if (bw > 0) coverage(dx, dy, ihx, ihy, irad) else outer;
            if (inner == 1 and opaque_fill) {
                row[x] = solid;
                continue;
            }
            const edge = outer - inner;
            const src: [4]f32 = .{
                f[0] * inner + b[0] * edge,
                f[1] * inner + b[1] * edge,
                f[2] * inner + b[2] * edge,
                f[3] * inner + b[3] * edge,
            };
            row[x] = over(src, row[x]);
        }
    }
}

/// How much of the pixel at (dx, dy) from the center a rounded rectangle
/// of half sizes hx and hy covers.
fn coverage(dx: f32, dy: f32, hx: f32, hy: f32, r: f32) f32 {
    const qx = dx - hx + r;
    const qy = dy - hy + r;
    const outside = @sqrt(@max(qx, 0) * @max(qx, 0) + @max(qy, 0) * @max(qy, 0));
    const d = outside + @min(@max(qx, qy), 0) - r;
    return std.math.clamp(0.5 - d, 0, 1);
}

/// The color premultiplied, in 0 to 255, alpha last.
fn premultiply(col: gfx.Color, cov: f32) [4]f32 {
    const a = @as(f32, @floatFromInt(col.a)) / 255 * cov;
    return .{ @as(f32, @floatFromInt(col.r)) * a, @as(f32, @floatFromInt(col.g)) * a, @as(f32, @floatFromInt(col.b)) * a, a * 255 };
}

fn pack(p: [4]f32) u32 {
    const r: u32 = @intFromFloat(@round(p[0]));
    const g: u32 = @intFromFloat(@round(p[1]));
    const b: u32 = @intFromFloat(@round(p[2]));
    const a: u32 = @intFromFloat(@round(p[3]));
    return a << 24 | r << 16 | g << 8 | b;
}

fn over(src: [4]f32, dst: u32) u32 {
    const k = 1 - src[3] / 255;
    return pack(.{
        src[0] + @as(f32, @floatFromInt(dst >> 16 & 0xff)) * k,
        src[1] + @as(f32, @floatFromInt(dst >> 8 & 0xff)) * k,
        src[2] + @as(f32, @floatFromInt(dst & 0xff)) * k,
        src[3] + @as(f32, @floatFromInt(dst >> 24)) * k,
    });
}

test "rounded rectangles cover their inside and blend their edges" {
    var pixels: [16 * 16]u32 = undefined;
    const t: platform.Target = .{ .pixels = &pixels, .width = 16, .height = 16, .stride = 16, .scale = 1 };
    clear(&t, .white);
    fillRect(&t, .{ .x = 2, .y = 2, .w = 12, .h = 12 }, 4, gfx.Color.hex("#2563eb"), 0, .transparent);
    try std.testing.expectEqual(@as(u32, 0xff2563eb), pixels[8 * 16 + 8]); // inside
    try std.testing.expectEqual(@as(u32, 0xffffffff), pixels[0]); // outside
    try std.testing.expectEqual(@as(u32, 0xffffffff), pixels[2 * 16 + 2]); // the rounded corner
    const edge = pixels[3 * 16 + 3]; // partly covered
    try std.testing.expect(edge != 0xffffffff and edge != 0xff2563eb);
}

test "borders are inside the rectangle" {
    var pixels: [8 * 8]u32 = undefined;
    const t: platform.Target = .{ .pixels = &pixels, .width = 8, .height = 8, .stride = 8, .scale = 1 };
    clear(&t, .white);
    fillRect(&t, .{ .x = 0, .y = 0, .w = 8, .h = 8 }, 0, .white, 1, .black);
    try std.testing.expectEqual(@as(u32, 0xff000000), pixels[0]);
    try std.testing.expectEqual(@as(u32, 0xff000000), pixels[7 * 8 + 3]);
    try std.testing.expectEqual(@as(u32, 0xffffffff), pixels[1 * 8 + 1]);
}
