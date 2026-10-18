//! The engine of a window's native UI: it runs the view to build the tree
//! of nodes, lays it out, takes the input against the last layout, and
//! paints a scene. A node exists for one frame; what lasts from frame to
//! frame is keyed by node IDs (hover, press, focus, pending clicks), as in
//! MyGo's ui, where the view rebuilds the interface from the state
//! whenever the window needs a frame.
const std = @import("std");
const gfx = @import("../gfx.zig");
const text = @import("../text.zig");
const platform = @import("../platform.zig");
const layout = @import("layout.zig");
const Theme = @import("Theme.zig");
const Context = @import("Context.zig");
const View = @import("../ui.zig").View;

const Runtime = @This();
const Allocator = std.mem.Allocator;

pub const none = std.math.maxInt(u32);

pub const Node = struct {
    id: u64,
    parent: u32,
    first_child: u32 = none,
    last_child: u32 = none,
    next: u32 = none,
    children: u32 = 0,
    kind: Kind = .box,

    // Layout.
    dir: Direction = .column,
    width: Length = .auto,
    height: Length = .auto,
    /// Top, right, bottom, left.
    padding: [4]f32 = @splat(0),
    gap: f32 = 0,
    justify: Justify = .start,
    align_items: Align = .stretch,

    // Looks. Text colors and fonts are inherited from the parent.
    background: gfx.Color = .transparent,
    border_width: f32 = 0,
    border: gfx.Color = .transparent,
    radius: f32 = 0,
    opacity: f32 = 1,
    text_color: gfx.Color,
    font: text.Font,
    str: []const u8 = "",
    run: gfx.TextRun = .{ .handle = 0, .width = 0, .height = 0 },

    // Behavior.
    /// What assistive technology and tests find it by, when not its text.
    label: []const u8 = "",
    clickable: bool = false,
    focusable: bool = false,
    /// Disabled, itself or by an ancestor.
    disabled: bool = false,

    // What layout found.
    natural_w: f32 = 0,
    natural_h: f32 = 0,
    main: f32 = 0,
    cross: f32 = 0,
    rect: gfx.Rect = .{},

    pub const Kind = enum { box, text };
    pub const Direction = enum { row, column };
    pub const Length = union(enum) { auto, px: f32, percent: f32 };

    pub fn padX(n: *const Node) f32 {
        return n.padding[1] + n.padding[3] + 2 * n.border_width;
    }
    pub fn padY(n: *const Node) f32 {
        return n.padding[0] + n.padding[2] + 2 * n.border_width;
    }
};

pub const Align = enum { start, center, end, stretch };
pub const Justify = enum { start, center, end, space_between };

const KeyPress = struct { key: platform.Key, mods: platform.Mods };

gpa: Allocator,
view: View,
text_system: text.System,
theme: Theme,
/// The view's handle on the runtime, through @fieldParentPtr.
ctx: Context = .{},
/// What a build allocates, freed when the next one starts.
arena: std.heap.ArenaAllocator,
nodes: std.ArrayList(Node) = .empty,
/// The containers being built, innermost last.
open: std.ArrayList(u32) = .empty,
scene: gfx.Scene = .{},

// Input, by node IDs.
hovered: u64 = 0,
pressed: u64 = 0,
focused: u64 = 0,
focus_visible: bool = false,
window_focused: bool = true,
pointer: ?platform.Point = null,
/// Clicks and keys not taken yet, which the next build takes.
clicks: std.ArrayList(u64) = .empty,
keys: std.ArrayList(KeyPress) = .empty,
/// A build took input, so the state may have changed: build again
/// before painting, so that no frame shows the state before the input.
took_input: bool = false,
/// A build ran out of memory: the frame is dropped.
failed: bool = false,

dirty: bool = true,
width: f32 = 0,
height: f32 = 0,
/// Frames built, and the build passes of the last one.
frame: u64 = 0,
passes: u32 = 0,

pub fn init(gpa: Allocator, view: View, ts: text.System, theme: Theme) Runtime {
    return .{ .gpa = gpa, .view = view, .text_system = ts, .theme = theme, .arena = .init(gpa) };
}

pub fn deinit(rt: *Runtime) void {
    rt.arena.deinit();
    rt.nodes.deinit(rt.gpa);
    rt.open.deinit(rt.gpa);
    rt.scene.deinit(rt.gpa);
    rt.clicks.deinit(rt.gpa);
    rt.keys.deinit(rt.gpa);
}

/// Takes an event, and tells whether the window needs a frame.
pub fn handle(rt: *Runtime, ev: platform.Event) bool {
    switch (ev) {
        .pointer_move => |p| {
            rt.pointer = p;
            const h = rt.hit(p);
            if (h == rt.hovered) return false;
            rt.hovered = h;
        },
        .pointer_leave => {
            rt.pointer = null;
            if (rt.hovered == 0) return false;
            rt.hovered = 0;
        },
        .pointer_down => |e| {
            if (e.button != .left) return false;
            const h = rt.hit(e.pos);
            rt.hovered, rt.pressed = .{ h, h };
            // A click focuses what it presses, without showing it.
            rt.focused, rt.focus_visible = .{ if (rt.isFocusable(h)) h else 0, false };
        },
        .pointer_up => |e| {
            if (e.button != .left or rt.pressed == 0) return false;
            if (rt.hit(e.pos) == rt.pressed) rt.clicks.append(rt.gpa, rt.pressed) catch {};
            rt.pressed = 0;
        },
        .key_down => |k| {
            const plain = k.mods.eql(.{});
            if (k.key == .tab and (plain or k.mods.eql(.{ .shift = true }))) {
                rt.moveFocus(!k.mods.shift);
            } else if (plain and (k.key == .enter or k.key == .space) and rt.focused != 0 and rt.isClickable(rt.focused)) {
                // A focused button takes Enter and Space before shortcuts.
                rt.clicks.append(rt.gpa, rt.focused) catch {};
            } else {
                rt.keys.append(rt.gpa, .{ .key = k.key, .mods = k.mods }) catch {};
            }
        },
        .key_up => return false,
        .focus => |f| rt.window_focused = f,
    }
    rt.dirty = true;
    return true;
}

/// Builds and lays out a frame of width by height DIPs, if the state or
/// the size changed since the last one, and tells whether it did.
pub fn update(rt: *Runtime, width: f32, height: f32) bool {
    if (!rt.dirty and width == rt.width and height == rt.height) return false;
    rt.width, rt.height = .{ width, height };
    rt.build();
    rt.layoutFrame();
    rt.dirty = false;
    return true;
}

/// Runs the view until it takes no more input, at most three times.
pub fn build(rt: *Runtime) void {
    rt.passes = 0;
    while (true) {
        rt.buildPass();
        rt.passes += 1;
        if (!rt.took_input or rt.passes == 3) break;
    }
    rt.frame += 1;
    // Input no element took goes away with this frame.
    rt.clicks.clearRetainingCapacity();
    rt.keys.clearRetainingCapacity();
    if (rt.focused != 0 and rt.find(rt.focused) == null) rt.focused = 0;
    rt.text_system.endFrame();
}

fn buildPass(rt: *Runtime) void {
    _ = rt.arena.reset(.retain_capacity);
    rt.nodes.clearRetainingCapacity();
    rt.open.clearRetainingCapacity();
    rt.took_input, rt.failed = .{ false, false };
    rt.nodes.append(rt.gpa, .{
        .id = 0x9e3779b97f4a7c15,
        .parent = none,
        .text_color = rt.theme.text,
        .font = .{ .size = rt.theme.font_size },
        .width = .{ .percent = 100 },
        .height = .{ .percent = 100 },
    }) catch {
        rt.failed = true;
        return;
    };
    rt.open.append(rt.gpa, 0) catch {
        rt.failed = true;
        return;
    };
    rt.view.build(&rt.ctx);
}

pub fn layoutFrame(rt: *Runtime) void {
    if (rt.nodes.items.len == 0) return;
    layout.layout(rt.nodes.items, 0, .{ .w = rt.width, .h = rt.height });
}

/// Adds a node under the innermost open container, with the ID key gives
/// it among its siblings, or its place among them.
pub fn add(rt: *Runtime, kind: Node.Kind, key: ?[]const u8) ?u32 {
    if (rt.failed) return null;
    const pi = rt.open.items[rt.open.items.len - 1];
    const parent = &rt.nodes.items[pi];
    const id = if (key) |k| std.hash.Wyhash.hash(parent.id, k) else std.hash.Wyhash.hash(parent.id ^ 0x5bd1e995, std.mem.asBytes(&parent.children));
    const i: u32 = @intCast(rt.nodes.items.len);
    rt.nodes.append(rt.gpa, .{
        .id = id,
        .parent = pi,
        .kind = kind,
        .text_color = parent.text_color,
        .font = parent.font,
        .disabled = parent.disabled,
    }) catch {
        rt.failed = true;
        return null;
    };
    const p = &rt.nodes.items[pi];
    if (p.last_child == none) p.first_child = i else rt.nodes.items[p.last_child].next = i;
    p.last_child = i;
    p.children += 1;
    return i;
}

pub fn push(rt: *Runtime, i: u32) void {
    rt.open.append(rt.gpa, i) catch rt.fail();
}

pub fn pop(rt: *Runtime) void {
    if (rt.open.items.len > 1) _ = rt.open.pop();
}

pub fn fail(rt: *Runtime) void {
    rt.failed = true;
}

/// Takes a click on the node with the ID, if one is pending.
pub fn takeClick(rt: *Runtime, id: u64) bool {
    const i = std.mem.indexOfScalar(u64, rt.clicks.items, id) orelse return false;
    _ = rt.clicks.orderedRemove(i);
    rt.took_input = true;
    return true;
}

/// Takes a press of key with exactly mods, if one is pending.
pub fn takeKey(rt: *Runtime, mods: platform.Mods, key: platform.Key) bool {
    for (rt.keys.items, 0..) |k, i| {
        if (k.key == key and k.mods.eql(mods)) {
            _ = rt.keys.orderedRemove(i);
            rt.took_input = true;
            return true;
        }
    }
    return false;
}

pub fn find(rt: *const Runtime, id: u64) ?*const Node {
    for (rt.nodes.items) |*n| if (n.id == id) return n;
    return null;
}

fn isClickable(rt: *const Runtime, id: u64) bool {
    const n = rt.find(id) orelse return false;
    return n.clickable and !n.disabled;
}

fn isFocusable(rt: *const Runtime, id: u64) bool {
    const n = rt.find(id) orelse return false;
    return n.focusable and !n.disabled;
}

/// The clickable node at p in the last layout, the topmost: nodes paint
/// in the order they were built, children over their parents.
fn hit(rt: *const Runtime, p: platform.Point) u64 {
    var i = rt.nodes.items.len;
    while (i > 0) {
        i -= 1;
        const n = &rt.nodes.items[i];
        if (n.clickable and !n.disabled and n.rect.contains(p.x, p.y)) return n.id;
    }
    return 0;
}

/// Moves the focus to the next focusable node in the order they were
/// built, or the previous one, around the end.
fn moveFocus(rt: *Runtime, forward: bool) void {
    const nodes = rt.nodes.items;
    var current: ?usize = null;
    for (nodes, 0..) |n, i| if (n.id == rt.focused) {
        current = i;
    };
    const len = nodes.len;
    if (len == 0) return;
    var step: usize = 0;
    var i: usize = current orelse if (forward) len - 1 else 0;
    while (step < len) : (step += 1) {
        i = if (forward) (i + 1) % len else (i + len - 1) % len;
        const n = nodes[i];
        if (n.focusable and !n.disabled) {
            rt.focused, rt.focus_visible = .{ n.id, true };
            return;
        }
    }
}

/// Paints the last layout into the scene.
pub fn paint(rt: *Runtime) void {
    rt.scene.reset(rt.theme.background);
    if (rt.nodes.items.len == 0) return;
    rt.paintNode(0, 1);
}

fn paintNode(rt: *Runtime, i: u32, alpha: f32) void {
    const n = rt.nodes.items[i];
    const a = alpha * n.opacity;
    if (n.background.a > 0 or (n.border_width > 0 and n.border.a > 0)) {
        rt.addOp(.{ .rect = .{
            .rect = n.rect,
            .radius = n.radius,
            .fill = n.background.fade(a),
            .border_width = n.border_width,
            .border = n.border.fade(a),
        } });
    }
    if (n.kind == .text and n.run.handle != 0) {
        rt.addOp(.{ .text = .{
            .x = n.rect.x + n.padding[3] + n.border_width,
            .y = n.rect.y + n.padding[0] + n.border_width,
            .run = n.run,
            .color = n.text_color.fade(a),
        } });
    }
    var child = n.first_child;
    while (child != none) : (child = rt.nodes.items[child].next) rt.paintNode(child, a);
    if (n.id == rt.focused and rt.focus_visible and rt.window_focused) {
        // MyGo's focus ring: 2 DIPs wide, 1 DIP out from the element.
        const w = 2;
        rt.addOp(.{ .rect = .{
            .rect = n.rect.inset(-(w + 1)),
            .radius = n.radius + w + 1,
            .border_width = w,
            .border = rt.theme.focus,
        } });
    }
}

fn addOp(rt: *Runtime, op: gfx.Op) void {
    rt.scene.ops.append(rt.gpa, op) catch {};
}

test "IDs are stable across frames and keys" {
    const ui = @import("../ui.zig");
    const S = struct {
        fn view(_: *@This(), c: *Context) void {
            c.column(.{});
            defer c.end();
            _ = c.text("a", .{});
            _ = c.button("b", .{ .key = "b" });
        }
    };
    var s: S = .{};
    var fixed: text.Fixed = .{};
    var rt = init(std.testing.allocator, ui.view(&s, S.view), fixed.system(), Theme.light);
    defer rt.deinit();
    _ = rt.update(100, 100);
    const ids = .{ rt.nodes.items[2].id, rt.nodes.items[3].id };
    rt.dirty = true;
    _ = rt.update(100, 100);
    try std.testing.expectEqual(ids, .{ rt.nodes.items[2].id, rt.nodes.items[3].id });
    try std.testing.expect(ids[0] != ids[1]);
}
