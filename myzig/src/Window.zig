//! A window of the app. Its content is native UI (`Options.content`),
//! drawn on the CPU into the native window's frame.
const std = @import("std");
const platform = @import("platform.zig");
const gfx = @import("gfx.zig");
const ui = @import("ui.zig");
const App = @import("App.zig");

const Window = @This();

app: *App,
native: *platform.NativeWindow,
runtime: ?ui.Runtime,
/// The pixels the last frame was painted into.
painted: ?[*]u32 = null,
stats: ?FrameStats = null,

pub const Options = struct {
    title: [:0]const u8 = "",
    /// The size of the content, in DIPs.
    width: u32 = 800,
    height: u32 = 600,
    /// What the window shows.
    content: ?ui.View = null,
};

/// Closes the window. Its memory goes with it.
pub fn close(w: *Window) void {
    w.native.close();
}

/// Asks for a frame, as after a change of the state outside input.
pub fn requestFrame(w: *Window) void {
    if (w.runtime) |*rt| rt.dirty = true;
    w.native.requestFrame();
}

pub fn handler(w: *Window) platform.Handler {
    return .{ .ptr = w, .vtable = &.{ .event = event, .draw = draw, .closed = closed } };
}

fn event(ptr: *anyopaque, ev: platform.Event) void {
    const w: *Window = @ptrCast(@alignCast(ptr));
    const rt = &(w.runtime orelse return);
    if (rt.handle(ev)) w.native.requestFrame();
}

fn draw(ptr: *anyopaque, target: *const platform.Target, width: f32, height: f32) void {
    const w: *Window = @ptrCast(@alignCast(ptr));
    const rt = &(w.runtime orelse {
        gfx.raster.draw(target, &.{ .background = w.app.theme.background }, w.app.textSystem());
        return;
    });
    if (w.stats) |*s| s.begin();
    const changed = rt.update(width, height);
    if (w.stats) |*s| s.lap(.build_and_layout);
    // An unchanged frame stays in the pixels it was painted into.
    if (!changed and w.painted == target.pixels.ptr) return;
    rt.paint();
    gfx.raster.draw(target, &rt.scene, rt.text_system);
    w.painted = target.pixels.ptr;
    if (w.stats) |*s| {
        s.lap(.paint);
        s.end(rt);
    }
}

fn closed(ptr: *anyopaque) void {
    const w: *Window = @ptrCast(@alignCast(ptr));
    const app = w.app;
    if (w.runtime) |*rt| rt.deinit();
    app.gpa.destroy(w);
    app.windowClosed();
}

/// Frame statistics, with MYZIG_FRAME_STATS set, as MyGo's
/// MYGO_FRAME_STATS: "all" logs every frame, a number of milliseconds
/// those slower than it, anything else those over 8 ms.
pub const FrameStats = struct {
    threshold_ns: u64,
    start: u64 = 0,
    last: u64 = 0,
    laps: [2]u64 = @splat(0),

    const Phase = enum { build_and_layout, paint };

    pub fn fromEnv() ?FrameStats {
        const v = std.mem.span(std.c.getenv("MYZIG_FRAME_STATS") orelse return null);
        if (v.len == 0 or std.mem.eql(u8, v, "0")) return null;
        if (std.mem.eql(u8, v, "all")) return .{ .threshold_ns = 0 };
        const ms = std.fmt.parseFloat(f64, v) catch 8;
        return .{ .threshold_ns = @intFromFloat(ms * std.time.ns_per_ms) };
    }

    fn begin(s: *FrameStats) void {
        s.start = platform.now();
        s.last = s.start;
        s.laps = @splat(0);
    }

    fn lap(s: *FrameStats, phase: Phase) void {
        const t = platform.now();
        s.laps[@intFromEnum(phase)] += t - s.last;
        s.last = t;
    }

    fn end(s: *FrameStats, rt: *const ui.Runtime) void {
        const total = platform.now() - s.start;
        if (total < s.threshold_ns) return;
        const ms = struct {
            fn f(ns: u64) f64 {
                return @as(f64, @floatFromInt(ns)) / std.time.ns_per_ms;
            }
        }.f;
        std.log.info("frame {d} took {d:.3} ms: build and layout {d:.3} ({d} passes), paint {d:.3}", .{
            rt.frame, ms(total), ms(s.laps[0]), rt.passes, ms(s.laps[1]),
        });
    }
};
