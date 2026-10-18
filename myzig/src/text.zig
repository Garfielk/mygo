//! Text: the system's own engine shapes and draws it (Pango on Linux), as
//! in MyGo's internal/text, behind a small interface the UI measures and
//! the renderer draws through.
const std = @import("std");
const builtin = @import("builtin");
const gfx = @import("gfx.zig");
const platform = @import("platform.zig");

pub const Weight = enum(u16) {
    regular = 400,
    medium = 500,
    semibold = 600,
    bold = 700,
    _,
};

pub const Font = struct {
    /// In DIPs.
    size: f32,
    weight: Weight = .regular,
};

/// The text system of this platform.
pub const Native = switch (builtin.os.tag) {
    .linux => @import("text/pango.zig").Pango,
    else => Fixed,
};

/// A text system: layout shapes a text, which draw draws in a frame.
/// Runs live until the frame after the last one that laid them out ends.
pub const System = struct {
    ptr: *anyopaque,
    vtable: *const VTable,

    pub const VTable = struct {
        layout: *const fn (ptr: *anyopaque, s: []const u8, font: Font) error{OutOfMemory}!gfx.TextRun,
        draw: *const fn (ptr: *anyopaque, target: *const platform.Target, run: gfx.TextRun, x: f32, y: f32, color: gfx.Color) void,
        end_frame: *const fn (ptr: *anyopaque) void,
    };

    pub fn layout(ts: System, s: []const u8, font: Font) error{OutOfMemory}!gfx.TextRun {
        return ts.vtable.layout(ts.ptr, s, font);
    }
    pub fn draw(ts: System, target: *const platform.Target, run: gfx.TextRun, x: f32, y: f32, color: gfx.Color) void {
        ts.vtable.draw(ts.ptr, target, run, x, y, color);
    }
    pub fn endFrame(ts: System) void {
        ts.vtable.end_frame(ts.ptr);
    }
};

/// A text system without fonts, for tests: every character is half the
/// font size wide, and lines 1.25 times its size high. It draws nothing.
pub const Fixed = struct {
    _: u8 = 0, // a pointer to it must have an address
    pub fn init(_: std.mem.Allocator, _: ?[]const u8) error{}!Fixed {
        return .{};
    }
    pub fn deinit(_: *Fixed) void {}

    pub fn system(f: *Fixed) System {
        return .{ .ptr = f, .vtable = &.{ .layout = layout, .draw = draw, .end_frame = endFrame } };
    }

    fn layout(_: *anyopaque, s: []const u8, font: Font) error{OutOfMemory}!gfx.TextRun {
        const n: f32 = @floatFromInt(std.unicode.utf8CountCodepoints(s) catch s.len);
        return .{ .handle = 0, .width = n * font.size / 2, .height = font.size * 1.25 };
    }
    fn draw(_: *anyopaque, _: *const platform.Target, _: gfx.TextRun, _: f32, _: f32, _: gfx.Color) void {}
    fn endFrame(_: *anyopaque) void {}
};

test {
    _ = Native;
}
