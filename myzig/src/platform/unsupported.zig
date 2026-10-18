//! The backend of platforms MyZig does not run on yet: everything
//! compiles, and App.init fails with error.Unsupported.
const std = @import("std");
const platform = @import("../platform.zig");

pub const Backend = struct {
    pub const InitError = error{Unsupported};

    pub fn init(_: std.mem.Allocator) InitError!Backend {
        return error.Unsupported;
    }
    pub fn run(_: *Backend) void {}
    pub fn quit(_: *Backend) void {}
    pub fn post(_: *Backend, _: *platform.Task) void {}
    pub fn uiFontFamily(_: *Backend, _: std.mem.Allocator) ?[:0]u8 {
        return null;
    }
    pub fn createWindow(_: *Backend, _: platform.WindowOptions, _: platform.Handler) std.mem.Allocator.Error!*Window {
        unreachable; // init failed
    }
};

pub const Window = struct {
    pub fn show(_: *Window) void {}
    pub fn requestFrame(_: *Window) void {}
    pub fn close(_: *Window) void {}
};
