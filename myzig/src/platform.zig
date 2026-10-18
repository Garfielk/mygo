//! The contract between the core and the backends, as MyGo's
//! internal/platform: the core owns every policy, and a backend only
//! translates these calls and events to and from the native toolkit.
//!
//! The backend is chosen at compile time, so calls to it are direct and
//! what a program does not use is not compiled in.
const std = @import("std");
const builtin = @import("builtin");

const impl = switch (builtin.os.tag) {
    .linux => @import("platform/linux.zig"),
    else => @import("platform/unsupported.zig"),
};
pub const Backend = impl.Backend;
pub const NativeWindow = impl.Window;

pub const Point = struct { x: f32 = 0, y: f32 = 0 };

/// Keys the core tells apart. Others arrive as `unknown`, with their text.
pub const Key = enum {
    unknown,
    up,
    down,
    left,
    right,
    tab,
    enter,
    space,
    escape,
    backspace,
    delete,
    home,
    end,
    page_up,
    page_down,
};

pub const Mods = packed struct(u8) {
    shift: bool = false,
    ctrl: bool = false,
    alt: bool = false,
    super: bool = false,
    _: u4 = 0,

    pub fn eql(a: Mods, b: Mods) bool {
        return @as(u8, @bitCast(a)) == @as(u8, @bitCast(b));
    }
};

pub const MouseButton = enum { left, middle, right, other };

/// Input of a window's content, in device-independent pixels.
pub const Event = union(enum) {
    pointer_move: Point,
    pointer_down: struct { pos: Point, button: MouseButton, mods: Mods = .{} },
    pointer_up: struct { pos: Point, button: MouseButton, mods: Mods = .{} },
    pointer_leave,
    key_down: struct { key: Key, mods: Mods = .{} },
    key_up: struct { key: Key, mods: Mods = .{} },
    focus: bool,
};

/// Pixels to draw a frame into: premultiplied BGRA in native-endian
/// 32-bit words (cairo's ARGB32), `stride` words a row.
pub const Target = struct {
    pixels: []u32,
    width: u32,
    height: u32,
    stride: u32,
    /// Device pixels per DIP.
    scale: f32,
};

pub const WindowOptions = struct {
    title: [:0]const u8 = "",
    width: u32 = 800,
    height: u32 = 600,
};

/// What the core implements to hear from a native window.
pub const Handler = struct {
    ptr: *anyopaque,
    vtable: *const VTable,

    pub const VTable = struct {
        /// Input arrived.
        event: *const fn (ptr: *anyopaque, ev: Event) void,
        /// The window shows a frame: draw it into target, sized in DIPs.
        draw: *const fn (ptr: *anyopaque, target: *const Target, width: f32, height: f32) void,
        /// The native window is gone.
        closed: *const fn (ptr: *anyopaque) void,
    };

    pub fn event(h: Handler, ev: Event) void {
        h.vtable.event(h.ptr, ev);
    }
    pub fn draw(h: Handler, target: *const Target, width: f32, height: f32) void {
        h.vtable.draw(h.ptr, target, width, height);
    }
    pub fn closed(h: Handler) void {
        h.vtable.closed(h.ptr);
    }
};

/// Work for the main thread (`App.post`). Embed it in a struct of your
/// own and get back to that with @fieldParentPtr, as std's intrusive nodes.
pub const Task = struct {
    run: *const fn (task: *Task) void,
};

/// A monotonic clock in nanoseconds, for frame statistics and animations.
pub fn now() u64 {
    if (builtin.os.tag == .windows) {
        const ntdll = std.os.windows.ntdll;
        var count: std.os.windows.LARGE_INTEGER = undefined;
        var freq: std.os.windows.LARGE_INTEGER = undefined;
        _ = ntdll.RtlQueryPerformanceCounter(&count);
        _ = ntdll.RtlQueryPerformanceFrequency(&freq);
        return @intCast(@divTrunc(@as(i128, count) * std.time.ns_per_s, freq));
    }
    var ts: std.c.timespec = undefined;
    _ = std.c.clock_gettime(.MONOTONIC, &ts);
    return @as(u64, @intCast(ts.sec)) * std.time.ns_per_s + @as(u64, @intCast(ts.nsec));
}
