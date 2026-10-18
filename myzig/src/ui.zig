//! Native UI: interfaces MyZig draws itself, built from the app's state by
//! a view every frame, as MyGo's package ui.
//!
//!     fn view(s: *Counter, c: *ui.Context) void {
//!         c.column(.{ .fill = true, .center = true });
//!         defer c.end();
//!         c.textf("{d}", .{s.n}, .{ .font_size = 56 });
//!         if (c.button("+", .{}).clicked()) s.n += 1;
//!     }
const std = @import("std");

pub const Context = @import("ui/Context.zig");
pub const Element = Context.Element;
pub const Padding = Context.Padding;
pub const Theme = @import("ui/Theme.zig");
pub const Runtime = @import("ui/Runtime.zig");
pub const Tester = @import("ui/Tester.zig");
pub const Align = Runtime.Align;
pub const Justify = Runtime.Justify;
pub const Color = @import("gfx.zig").Color;
pub const Key = @import("platform.zig").Key;
pub const Mods = @import("platform.zig").Mods;

/// What a window shows: a function building the interface from a state.
pub const View = struct {
    ptr: *anyopaque,
    build_fn: *const fn (ptr: *anyopaque, c: *Context) void,

    pub fn build(v: View, c: *Context) void {
        v.build_fn(v.ptr, c);
    }
};

/// The view build_fn makes of state, a pointer, every frame.
pub fn view(state: anytype, comptime build_fn: fn (@TypeOf(state), *Context) void) View {
    const State = @TypeOf(state);
    if (@typeInfo(State) != .pointer) @compileError("ui.view takes a pointer to the state");
    const erased = struct {
        fn build(ptr: *anyopaque, c: *Context) void {
            build_fn(@ptrCast(@alignCast(ptr)), c);
        }
    };
    return .{ .ptr = @ptrCast(@constCast(state)), .build_fn = erased.build };
}

test {
    std.testing.refAllDecls(@This());
    _ = Runtime;
    _ = @import("ui/layout.zig");
}
