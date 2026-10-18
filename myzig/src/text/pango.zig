//! Text on Linux: Pango shapes it, with fontconfig and HarfBuzz under it,
//! as GTK does, and cairo draws its glyphs into the frame.
const std = @import("std");
const c = @import("../platform/linux/c.zig");
const gfx = @import("../gfx.zig");
const platform = @import("../platform.zig");
const text = @import("../text.zig");

const Allocator = std.mem.Allocator;

pub const Pango = struct {
    gpa: Allocator,
    context: *c.PangoContext,
    family: [:0]const u8,
    fonts: std.ArrayList(FontDesc) = .empty,
    /// Layouts by their text and font, kept while frames draw them.
    layouts: std.HashMapUnmanaged(Key, Entry, KeyContext, 80) = .empty,
    frame: u64 = 0,
    /// The cairo context drawing into the frame's pixels.
    canvas: ?struct { pixels: [*]u32, width: u32, height: u32, scale: f32, surface: *c.cairo_surface_t, cr: *c.cairo_t } = null,
    /// How many layouts Pango made, as MyGo's frame statistics count them.
    layouts_made: u64 = 0,

    const FontDesc = struct { font: text.Font, desc: *c.PangoFontDescription };
    const Key = struct { text: []const u8, size: f32, weight: text.Weight };
    const Entry = struct { layout: *c.PangoLayout, width: f32, height: f32, used: u64 };

    const KeyContext = struct {
        pub fn hash(_: KeyContext, k: Key) u64 {
            var h = std.hash.Wyhash.init(@intFromEnum(k.weight));
            h.update(std.mem.asBytes(&k.size));
            h.update(k.text);
            return h.final();
        }
        pub fn eql(_: KeyContext, a: Key, b: Key) bool {
            return a.size == b.size and a.weight == b.weight and std.mem.eql(u8, a.text, b.text);
        }
    };

    /// A text system drawing with family, or the default font when null,
    /// with the desktop's font options. GTK must be initialized.
    pub fn init(gpa: Allocator, family: ?[]const u8) Allocator.Error!Pango {
        const context = c.pango.font_map_create_context(c.pango_cairo.font_map_get_default());
        if (c.gdk.screen_get_default()) |screen| {
            c.pango_cairo.context_set_font_options(context, c.gdk.screen_get_font_options(screen));
        }
        return .{ .gpa = gpa, .context = context, .family = try gpa.dupeZ(u8, family orelse "Sans") };
    }

    pub fn deinit(p: *Pango) void {
        var it = p.layouts.iterator();
        while (it.next()) |e| {
            c.gobject.object_unref(e.value_ptr.layout);
            p.gpa.free(e.key_ptr.text);
        }
        p.layouts.deinit(p.gpa);
        for (p.fonts.items) |f| c.pango.font_description_free(f.desc);
        p.fonts.deinit(p.gpa);
        p.dropCanvas();
        c.gobject.object_unref(p.context);
        p.gpa.free(p.family);
    }

    pub fn system(p: *Pango) text.System {
        return .{ .ptr = p, .vtable = &.{ .layout = layout, .draw = draw, .end_frame = endFrame } };
    }

    fn fontDesc(p: *Pango, font: text.Font) Allocator.Error!*c.PangoFontDescription {
        for (p.fonts.items) |f| if (f.font.size == font.size and f.font.weight == font.weight) return f.desc;
        const desc = c.pango.font_description_new();
        c.pango.font_description_set_family(desc, p.family);
        c.pango.font_description_set_absolute_size(desc, font.size * c.PANGO_SCALE);
        c.pango.font_description_set_weight(desc, @intFromEnum(font.weight));
        errdefer c.pango.font_description_free(desc);
        try p.fonts.append(p.gpa, .{ .font = font, .desc = desc });
        return desc;
    }

    fn layout(ptr: *anyopaque, s: []const u8, font: text.Font) Allocator.Error!gfx.TextRun {
        const p: *Pango = @ptrCast(@alignCast(ptr));
        const key: Key = .{ .text = s, .size = font.size, .weight = font.weight };
        const gop = try p.layouts.getOrPut(p.gpa, key);
        if (!gop.found_existing) {
            errdefer p.layouts.removeByPtr(gop.key_ptr);
            const desc = try p.fontDesc(font);
            gop.key_ptr.text = try p.gpa.dupe(u8, s);
            const l = c.pango.layout_new(p.context);
            c.pango.layout_set_font_description(l, desc);
            c.pango.layout_set_text(l, s.ptr, @intCast(s.len));
            var w: c_int = 0;
            var h: c_int = 0;
            c.pango.layout_get_size(l, &w, &h);
            gop.value_ptr.* = .{
                .layout = l,
                .width = @as(f32, @floatFromInt(w)) / c.PANGO_SCALE,
                .height = @as(f32, @floatFromInt(h)) / c.PANGO_SCALE,
                .used = p.frame,
            };
            p.layouts_made += 1;
        }
        gop.value_ptr.used = p.frame;
        const e = gop.value_ptr;
        return .{ .handle = @intFromPtr(e.layout), .width = e.width, .height = e.height };
    }

    fn draw(ptr: *anyopaque, target: *const platform.Target, run: gfx.TextRun, x: f32, y: f32, color: gfx.Color) void {
        const p: *Pango = @ptrCast(@alignCast(ptr));
        if (run.handle == 0) return;
        const cr = p.canvasFor(target);
        const l: *c.PangoLayout = @ptrFromInt(run.handle);
        c.cairo.set_source_rgba(cr, @as(f64, @floatFromInt(color.r)) / 255, @as(f64, @floatFromInt(color.g)) / 255, @as(f64, @floatFromInt(color.b)) / 255, @as(f64, @floatFromInt(color.a)) / 255);
        // Whole device pixels down, as GTK puts text.
        c.cairo.move_to(cr, @round(x * target.scale) / target.scale, @round(y * target.scale) / target.scale);
        c.pango_cairo.show_layout(cr, l);
        c.cairo.surface_flush(p.canvas.?.surface);
    }

    /// Frees the layouts no frame drew since the last one.
    fn endFrame(ptr: *anyopaque) void {
        const p: *Pango = @ptrCast(@alignCast(ptr));
        var it = p.layouts.iterator();
        while (it.next()) |e| {
            if (e.value_ptr.used >= p.frame) continue;
            c.gobject.object_unref(e.value_ptr.layout);
            const owned = e.key_ptr.text;
            p.layouts.removeByPtr(e.key_ptr); // leaves a tombstone: the iterator goes on
            p.gpa.free(owned);
        }
        p.frame += 1;
    }

    fn canvasFor(p: *Pango, t: *const platform.Target) *c.cairo_t {
        if (p.canvas) |cv| {
            if (cv.pixels == t.pixels.ptr and cv.width == t.width and cv.height == t.height and cv.scale == t.scale) {
                c.cairo.surface_mark_dirty(cv.surface);
                return cv.cr;
            }
        }
        p.dropCanvas();
        const surface = c.cairo.image_surface_create_for_data(@ptrCast(t.pixels.ptr), .argb32, @intCast(t.width), @intCast(t.height), @intCast(t.stride * 4));
        c.cairo.surface_set_device_scale(surface, t.scale, t.scale);
        const cr = c.cairo.create(surface);
        p.canvas = .{ .pixels = t.pixels.ptr, .width = t.width, .height = t.height, .scale = t.scale, .surface = surface, .cr = cr };
        return cr;
    }

    fn dropCanvas(p: *Pango) void {
        const cv = p.canvas orelse return;
        c.cairo.destroy(cv.cr);
        c.cairo.surface_destroy(cv.surface);
        p.canvas = null;
    }
};
