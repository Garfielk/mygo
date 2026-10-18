//! Layout: a subset of CSS flexbox, as MyGo's ui lays out, with sizes of
//! border boxes. Measuring goes up the tree, from what texts need, and
//! placing down it, from the window's size.
const std = @import("std");
const gfx = @import("../gfx.zig");
const Node = @import("Runtime.zig").Node;
const none = @import("Runtime.zig").none;

/// Lays out the tree of nodes from root, which fills rect.
pub fn layout(nodes: []Node, root: u32, rect: gfx.Rect) void {
    measure(nodes, root);
    place(nodes, root, rect);
}

/// Finds what each node needs at most, without wrapping: its max-content
/// size, which sizes nodes their parents give no size to.
fn measure(nodes: []Node, i: u32) void {
    const n = &nodes[i];
    var main: f32 = 0;
    var cross: f32 = 0;
    switch (n.kind) {
        .text => {
            main, cross = .{ n.run.width, n.run.height };
        },
        .box => {
            var count: f32 = 0;
            var child = n.first_child;
            while (child != none) : (child = nodes[child].next) {
                measure(nodes, child);
                const ch = &nodes[child];
                const cw = fixedOr(ch.width, ch.natural_w);
                const chh = fixedOr(ch.height, ch.natural_h);
                const cm, const cc = if (n.dir == .row) .{ cw, chh } else .{ chh, cw };
                main += cm;
                cross = @max(cross, cc);
                count += 1;
            }
            if (count > 1) main += n.gap * (count - 1);
        },
    }
    const content_w, const content_h = if (n.kind == .text or n.dir == .row) .{ main, cross } else .{ cross, main };
    n.natural_w = fixedOr(n.width, content_w + n.padX());
    n.natural_h = fixedOr(n.height, content_h + n.padY());
}

fn fixedOr(l: Node.Length, natural: f32) f32 {
    return switch (l) {
        .px => |v| v,
        else => natural,
    };
}

/// The size a length gives within a parent's content of size of, if any.
fn resolve(l: Node.Length, of: f32) ?f32 {
    return switch (l) {
        .auto => null,
        .px => |v| v,
        .percent => |p| of * p / 100,
    };
}

fn place(nodes: []Node, i: u32, rect: gfx.Rect) void {
    const n = &nodes[i];
    n.rect = rect;
    if (n.kind == .text or n.first_child == none) return;
    const content: gfx.Rect = .{
        .x = rect.x + n.padding[3] + n.border_width,
        .y = rect.y + n.padding[0] + n.border_width,
        .w = @max(rect.w - n.padX(), 0),
        .h = @max(rect.h - n.padY(), 0),
    };
    const row = n.dir == .row;
    const avail_main = if (row) content.w else content.h;
    const avail_cross = if (row) content.h else content.w;

    // The sizes of the children, along the axes of the parent.
    var used: f32 = 0;
    var count: f32 = 0;
    var child = n.first_child;
    while (child != none) : (child = nodes[child].next) {
        const ch = &nodes[child];
        const main_len, const cross_len = if (row) .{ ch.width, ch.height } else .{ ch.height, ch.width };
        const main_nat, const cross_nat = if (row) .{ ch.natural_w, ch.natural_h } else .{ ch.natural_h, ch.natural_w };
        ch.main = resolve(main_len, avail_main) orelse main_nat;
        ch.cross = resolve(cross_len, avail_cross) orelse if (n.align_items == .stretch) avail_cross else cross_nat;
        used += ch.main;
        count += 1;
    }
    used += n.gap * @max(count - 1, 0);
    const free = avail_main - used;
    var pos: f32 = switch (n.justify) {
        .start, .space_between => 0,
        .center => free / 2,
        .end => free,
    };
    const between = n.gap + if (n.justify == .space_between and count > 1) @max(free, 0) / (count - 1) else 0;

    child = n.first_child;
    while (child != none) : (child = nodes[child].next) {
        const ch = &nodes[child];
        const off: f32 = switch (n.align_items) {
            .start, .stretch => 0,
            .center => (avail_cross - ch.cross) / 2,
            .end => avail_cross - ch.cross,
        };
        const r: gfx.Rect = if (row)
            .{ .x = content.x + pos, .y = content.y + off, .w = ch.main, .h = ch.cross }
        else
            .{ .x = content.x + off, .y = content.y + pos, .w = ch.cross, .h = ch.main };
        place(nodes, child, r);
        pos += ch.main + between;
    }
}
