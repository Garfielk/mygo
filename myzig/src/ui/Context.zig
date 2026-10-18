//! What a view builds the interface with, every frame. Containers open
//! with `row` or `column` and close with `end`, which `defer` keeps next
//! to them; options are structs whose defaults are the common case.
//!
//!     c.column(.{ .fill = true, .center = true, .gap = 16 });
//!     defer c.end();
//!     if (c.button("+", .{ .variant = .primary }).clicked()) state.n += 1;
const std = @import("std");
const gfx = @import("../gfx.zig");
const Weight = @import("../text.zig").Weight;
const platform = @import("../platform.zig");
const Runtime = @import("Runtime.zig");
const Theme = @import("Theme.zig");

const Context = @This();
const Node = Runtime.Node;
const Color = gfx.Color;

fn rt(c: *Context) *Runtime {
    return @alignCast(@fieldParentPtr("ctx", c));
}

/// An element of this frame, to ask what the input did to it.
pub const Element = struct {
    rt: *Runtime,
    index: u32,

    pub const none: Element = .{ .rt = undefined, .index = Runtime.none };

    fn id(e: Element) u64 {
        return if (e.index == Runtime.none) 0 else e.rt.nodes.items[e.index].id;
    }

    /// Whether it was clicked, with the pointer, or Enter or Space while
    /// it had the focus. Asking takes the click.
    pub fn clicked(e: Element) bool {
        const i = e.id();
        return i != 0 and e.rt.takeClick(i);
    }

    pub fn hovered(e: Element) bool {
        const i = e.id();
        return i != 0 and e.rt.hovered == i;
    }

    pub fn pressed(e: Element) bool {
        const i = e.id();
        return i != 0 and e.rt.pressed == i;
    }

    pub fn focused(e: Element) bool {
        const i = e.id();
        return i != 0 and e.rt.focused == i;
    }
};

pub const Padding = struct {
    top: f32 = 0,
    right: f32 = 0,
    bottom: f32 = 0,
    left: f32 = 0,

    pub fn all(v: f32) Padding {
        return .{ .top = v, .right = v, .bottom = v, .left = v };
    }
    /// y above and below, x at the sides.
    pub fn xy(x: f32, y: f32) Padding {
        return .{ .top = y, .right = x, .bottom = y, .left = x };
    }
};

pub const BoxOptions = struct {
    /// Identifies the element among its siblings, whatever their order.
    key: ?[]const u8 = null,
    /// Fill the parent, both ways.
    fill: bool = false,
    /// Center the children, both ways.
    center: bool = false,
    width: ?f32 = null,
    height: ?f32 = null,
    gap: f32 = 0,
    padding: Padding = .{},
    justify: ?Runtime.Justify = null,
    align_items: ?Runtime.Align = null,
    background: ?Color = null,
    radius: f32 = 0,
    border_width: f32 = 0,
    border: Color = .transparent,
    /// The color of the texts inside.
    text_color: ?Color = null,
    disabled: bool = false,
};

pub const TextOptions = struct {
    key: ?[]const u8 = null,
    font_size: ?f32 = null,
    bold: bool = false,
    weight: ?Weight = null,
    color: ?Color = null,
    label: ?[]const u8 = null,
};

pub const ButtonOptions = struct {
    key: ?[]const u8 = null,
    /// Names the button for assistive technology and tests, when its text
    /// does not, as "Increment" for "+".
    label: ?[]const u8 = null,
    variant: enum { normal, primary } = .normal,
    width: ?f32 = null,
    height: ?f32 = null,
    disabled: bool = false,
};

pub fn theme(c: *Context) *const Theme {
    return &c.rt().theme;
}

/// Formats into memory that lasts for the frame.
pub fn print(c: *Context, comptime fmt: []const u8, args: anytype) []const u8 {
    const r = c.rt();
    return std.fmt.allocPrint(r.arena.allocator(), fmt, args) catch {
        r.fail();
        return "";
    };
}

/// Whether the key was pressed with exactly mods, wherever the focus is,
/// unless a focused element took it first. Asking takes the key.
pub fn shortcut(c: *Context, mods: platform.Mods, key: platform.Key) bool {
    return c.rt().takeKey(mods, key);
}

/// Opens a column: its children go under each other until `end`.
pub fn column(c: *Context, opts: BoxOptions) void {
    c.container(.column, opts);
}

/// Opens a row: its children go side by side, centered vertically, until
/// `end`.
pub fn row(c: *Context, opts: BoxOptions) void {
    c.container(.row, opts);
}

/// Closes the row or column opened last.
pub fn end(c: *Context) void {
    c.rt().pop();
}

fn container(c: *Context, dir: Node.Direction, opts: BoxOptions) void {
    const r = c.rt();
    const i = r.add(.box, opts.key) orelse {
        r.open.append(r.gpa, 0) catch {}; // keeps end balanced
        return;
    };
    const n = &r.nodes.items[i];
    n.dir = dir;
    if (dir == .row) n.align_items = .center;
    style(n, opts);
    r.push(i);
}

fn style(n: *Node, opts: BoxOptions) void {
    if (opts.fill) n.width, n.height = .{ .{ .percent = 100 }, .{ .percent = 100 } };
    if (opts.width) |w| n.width = .{ .px = w };
    if (opts.height) |h| n.height = .{ .px = h };
    if (opts.center) n.justify, n.align_items = .{ .center, .center };
    if (opts.justify) |j| n.justify = j;
    if (opts.align_items) |a| n.align_items = a;
    n.gap = opts.gap;
    n.padding = .{ opts.padding.top, opts.padding.right, opts.padding.bottom, opts.padding.left };
    if (opts.background) |b| n.background = b;
    n.radius = opts.radius;
    n.border_width = opts.border_width;
    n.border = opts.border;
    if (opts.text_color) |t| n.text_color = t;
    if (opts.disabled) {
        n.disabled = true;
        n.opacity = 0.5;
    }
}

/// A text, on one line.
pub fn text(c: *Context, s: []const u8, opts: TextOptions) Element {
    const r = c.rt();
    const i = r.add(.text, opts.key) orelse return .none;
    const n = &r.nodes.items[i];
    if (opts.font_size) |size| n.font.size = size;
    if (opts.bold) n.font.weight = .bold;
    if (opts.weight) |w| n.font.weight = w;
    if (opts.color) |col| n.text_color = col;
    if (opts.label) |l| n.label = l;
    n.str = s;
    n.run = r.text_system.layout(s, n.font) catch blk: {
        r.fail();
        break :blk .{ .handle = 0, .width = 0, .height = 0 };
    };
    return .{ .rt = r, .index = i };
}

/// A text formatted as std.fmt does.
pub fn textf(c: *Context, comptime fmt: []const u8, args: anytype, opts: TextOptions) Element {
    return c.text(c.print(fmt, args), opts);
}

/// A button showing label. Ask the element whether it was clicked.
pub fn button(c: *Context, label: []const u8, opts: ButtonOptions) Element {
    const r = c.rt();
    const t = &r.theme;
    const i = r.add(.box, opts.key) orelse return .none;
    const b = &r.nodes.items[i];
    b.dir = .row;
    b.justify, b.align_items = .{ .center, .center };
    b.padding = .{ t.space(1.5), t.space(3.5), t.space(1.5), t.space(3.5) };
    b.gap = t.space(1.5);
    b.radius = t.radius;
    b.clickable, b.focusable = .{ true, true };
    if (opts.label) |l| b.label = l;
    if (opts.width) |w| b.width = .{ .px = w };
    if (opts.height) |h| b.height = .{ .px = h };
    if (opts.disabled) {
        b.disabled = true;
        b.opacity = 0.5;
    }
    const base, const hover, const press, const fg, const border = switch (opts.variant) {
        .normal => .{ t.surface, t.surface_hover, t.surface_pressed, t.text, t.border },
        .primary => .{ t.accent, t.accent_hover, t.accent_pressed, t.accent_text, Color.transparent },
    };
    b.background = base;
    if (!b.disabled) {
        if (r.pressed == b.id) b.background = press else if (r.hovered == b.id) b.background = hover;
    }
    b.text_color = fg;
    if (border.a > 0) b.border_width, b.border = .{ 1, border };
    if (label.len > 0) {
        r.push(i);
        _ = c.text(label, .{});
        r.pop();
    }
    return .{ .rt = r, .index = i };
}
