//! Runs a view without a window, as MyGo's ui.NewTester: tests click and
//! press keys as a user would, and look at what the view shows. Text is
//! measured without fonts (text.Fixed).
const std = @import("std");
const platform = @import("../platform.zig");
const text = @import("../text.zig");
const Runtime = @import("Runtime.zig");
const Theme = @import("Theme.zig");
const View = @import("../ui.zig").View;

const Tester = @This();

rt: Runtime,
width: f32,
height: f32,

var fixed: text.Fixed = .{};

pub fn init(gpa: std.mem.Allocator, view: View, width: f32, height: f32) Tester {
    var tt: Tester = .{ .rt = .init(gpa, view, fixed.system(), Theme.light), .width = width, .height = height };
    _ = tt.rt.update(width, height);
    return tt;
}

pub fn deinit(tt: *Tester) void {
    tt.rt.deinit();
}

/// Builds a frame if the input changed anything.
pub fn update(tt: *Tester) void {
    _ = tt.rt.update(tt.width, tt.height);
}

/// Clicks the middle of the clickable element named name, by its label or
/// its text, and builds the frame that follows.
pub fn click(tt: *Tester, name: []const u8) error{NotFound}!void {
    tt.update();
    const n = tt.findClickable(name) orelse return error.NotFound;
    const p: platform.Point = .{ .x = n.rect.x + n.rect.w / 2, .y = n.rect.y + n.rect.h / 2 };
    _ = tt.rt.handle(.{ .pointer_move = p });
    _ = tt.rt.handle(.{ .pointer_down = .{ .pos = p, .button = .left } });
    _ = tt.rt.handle(.{ .pointer_up = .{ .pos = p, .button = .left } });
    tt.update();
}

/// Presses and releases key with mods, and builds the frame that follows.
pub fn key(tt: *Tester, mods: platform.Mods, k: platform.Key) void {
    _ = tt.rt.handle(.{ .key_down = .{ .key = k, .mods = mods } });
    _ = tt.rt.handle(.{ .key_up = .{ .key = k, .mods = mods } });
    tt.update();
}

/// Whether a text of the frame is s.
pub fn hasText(tt: *Tester, s: []const u8) bool {
    tt.update();
    for (tt.rt.nodes.items) |n| if (n.kind == .text and std.mem.eql(u8, n.str, s)) return true;
    return false;
}

fn findClickable(tt: *Tester, name: []const u8) ?*const Runtime.Node {
    const nodes = tt.rt.nodes.items;
    for (nodes) |*n| {
        if (!n.clickable) continue;
        if (std.mem.eql(u8, n.label, name)) return n;
        // Or the text inside it.
        var child = n.first_child;
        while (child != Runtime.none) : (child = nodes[child].next) {
            if (nodes[child].kind == .text and std.mem.eql(u8, nodes[child].str, name)) return n;
        }
    }
    return null;
}
