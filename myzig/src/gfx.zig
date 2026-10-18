//! Colors, geometry and the display list the UI paints into, which a
//! renderer draws: MyGo's internal/scene. Only the CPU renderer exists so
//! far (`raster`), as MyGo's internal/raster.
const std = @import("std");

pub const raster = @import("gfx/raster.zig");

/// An sRGB color, not premultiplied.
pub const Color = packed struct(u32) {
    b: u8 = 0,
    g: u8 = 0,
    r: u8 = 0,
    a: u8 = 0,

    pub const transparent: Color = .{};
    pub const white = hex("#ffffff");
    pub const black = hex("#000000");

    pub fn rgb(r: u8, g: u8, b: u8) Color {
        return .{ .r = r, .g = g, .b = b, .a = 255 };
    }

    /// A color with an alpha from 0 to 1, as CSS's rgba().
    pub fn rgba(r: u8, g: u8, b: u8, alpha: f32) Color {
        return .{ .r = r, .g = g, .b = b, .a = unitToByte(alpha) };
    }

    /// Parses "#rgb", "#rrggbb" or "#rrggbbaa" at compile time.
    pub fn hex(comptime s: []const u8) Color {
        return comptime parseHex(s);
    }

    fn parseHex(comptime s: []const u8) Color {
        if (s.len == 0 or s[0] != '#') @compileError("color must start with #: " ++ s);
        const d = s[1..];
        const digit = struct {
            fn f(ch: u8) u8 {
                return std.fmt.charToDigit(ch, 16) catch @compileError("not a hex color: " ++ s);
            }
        }.f;
        return switch (d.len) {
            3 => .{ .r = digit(d[0]) * 17, .g = digit(d[1]) * 17, .b = digit(d[2]) * 17, .a = 255 },
            6, 8 => .{
                .r = digit(d[0]) * 16 + digit(d[1]),
                .g = digit(d[2]) * 16 + digit(d[3]),
                .b = digit(d[4]) * 16 + digit(d[5]),
                .a = if (d.len == 8) digit(d[6]) * 16 + digit(d[7]) else 255,
            },
            else => @compileError("not a hex color: " ++ s),
        };
    }

    /// The color with its alpha multiplied by f.
    pub fn fade(col: Color, f: f32) Color {
        var out = col;
        out.a = unitToByte(@as(f32, @floatFromInt(col.a)) / 255 * f);
        return out;
    }

    /// The color t of the way to other.
    pub fn mix(col: Color, other: Color, t: f32) Color {
        const lerp = struct {
            fn f(a: u8, b: u8, w: f32) u8 {
                return @intFromFloat(@round(@as(f32, @floatFromInt(a)) * (1 - w) + @as(f32, @floatFromInt(b)) * w));
            }
        }.f;
        return .{ .r = lerp(col.r, other.r, t), .g = lerp(col.g, other.g, t), .b = lerp(col.b, other.b, t), .a = lerp(col.a, other.a, t) };
    }

    fn unitToByte(v: f32) u8 {
        return @intFromFloat(@round(std.math.clamp(v, 0, 1) * 255));
    }
};

pub const Rect = struct {
    x: f32 = 0,
    y: f32 = 0,
    w: f32 = 0,
    h: f32 = 0,

    pub fn contains(r: Rect, x: f32, y: f32) bool {
        return x >= r.x and y >= r.y and x < r.x + r.w and y < r.y + r.h;
    }

    pub fn inset(r: Rect, d: f32) Rect {
        return .{ .x = r.x + d, .y = r.y + d, .w = @max(r.w - 2 * d, 0), .h = @max(r.h - 2 * d, 0) };
    }
};

/// A shaped text a text system made, which it draws.
pub const TextRun = struct {
    /// The text system's own handle.
    handle: usize,
    width: f32,
    height: f32,
};

/// What a frame draws, in DIPs, in order.
pub const Op = union(enum) {
    /// A rectangle with rounded corners, filled, with a border inside it.
    rect: struct {
        rect: Rect,
        radius: f32 = 0,
        fill: Color = .transparent,
        border_width: f32 = 0,
        border: Color = .transparent,
    },
    text: struct {
        x: f32,
        y: f32,
        run: TextRun,
        color: Color,
    },
};

pub const Scene = struct {
    ops: std.ArrayList(Op) = .empty,
    background: Color = .white,

    pub fn deinit(s: *Scene, gpa: std.mem.Allocator) void {
        s.ops.deinit(gpa);
    }

    pub fn reset(s: *Scene, background: Color) void {
        s.ops.clearRetainingCapacity();
        s.background = background;
    }
};

test "hex colors" {
    try std.testing.expectEqual(Color{ .r = 0x25, .g = 0x63, .b = 0xeb, .a = 255 }, Color.hex("#2563eb"));
    try std.testing.expectEqual(Color{ .r = 255, .g = 255, .b = 255, .a = 255 }, Color.hex("#fff"));
    try std.testing.expectEqual(@as(u8, 128), Color.hex("#00000080").a);
    try std.testing.expectEqual(@as(u8, 64), Color.rgba(37, 99, 235, 0.25).a);
}
