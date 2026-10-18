//! The Linux backend: GTK 3 windows whose content is a GtkDrawingArea that
//! shows frames drawn in memory, as MyGo's internal/linux draws native UI
//! where OpenGL would not run on a GPU.
const std = @import("std");
const platform = @import("../platform.zig");
const c = @import("linux/c.zig");

const Allocator = std.mem.Allocator;

pub const Backend = struct {
    gpa: Allocator,

    pub const InitError = c.LoadError || error{NoDisplay};

    /// Loads GTK and connects to the display. It must be called on the
    /// thread that runs the event loop, the main thread.
    pub fn init(gpa: Allocator) InitError!Backend {
        try c.loadAll();
        if (c.gtk.init_check(null, null) == 0) return error.NoDisplay;
        return .{ .gpa = gpa };
    }

    /// Runs the event loop until quit.
    pub fn run(_: *Backend) void {
        c.gtk.main();
    }

    pub fn quit(_: *Backend) void {
        c.gtk.main_quit();
    }

    /// Runs task on the main thread soon. Safe from any thread.
    pub fn post(_: *Backend, task: *platform.Task) void {
        _ = c.glib.idle_add_full(0, runTask, task, null);
    }

    fn runTask(data: ?*anyopaque) callconv(.c) c.gboolean {
        const task: *platform.Task = @ptrCast(@alignCast(data.?));
        task.run(task);
        return 0; // G_SOURCE_REMOVE
    }

    /// The family of the interface font the desktop set (GTK's
    /// gtk-font-name), which "system-ui" stands for. The caller owns it.
    pub fn uiFontFamily(_: *Backend, gpa: Allocator) ?[:0]u8 {
        const settings = c.gtk.settings_get_default() orelse return null;
        var name: ?[*:0]u8 = null;
        c.gobject.object_get(settings, "gtk-font-name", &name, @as(?*anyopaque, null));
        const font = name orelse return null;
        defer c.glib.free(font);
        const desc = c.pango.font_description_from_string(font);
        defer c.pango.font_description_free(desc);
        const family = c.pango.font_description_get_family(desc) orelse return null;
        return gpa.dupeZ(u8, std.mem.span(family)) catch null;
    }

    pub fn createWindow(b: *Backend, opts: platform.WindowOptions, handler: platform.Handler) Allocator.Error!*Window {
        const w = try b.gpa.create(Window);
        const win = c.gtk.window_new(.toplevel);
        const area = c.gtk.drawing_area_new();
        w.* = .{ .backend = b, .handler = handler, .window = win, .area = area };
        c.gtk.window_set_title(win, opts.title);
        c.gtk.window_set_default_size(win, @intCast(opts.width), @intCast(opts.height));
        c.gtk.window_set_position(win, .center);
        c.gtk.widget_set_can_focus(area, 1);
        const m = c.event_mask;
        c.gtk.widget_add_events(area, m.pointer_motion | m.button_press | m.button_release | m.key_press |
            m.key_release | m.enter_notify | m.leave_notify | m.focus_change | m.scroll | m.smooth_scroll);
        c.connect(area, "draw", &Window.onDraw, w);
        c.connect(area, "button-press-event", &Window.onButton, w);
        c.connect(area, "button-release-event", &Window.onButton, w);
        c.connect(area, "motion-notify-event", &Window.onMotion, w);
        c.connect(area, "leave-notify-event", &Window.onLeave, w);
        c.connect(area, "key-press-event", &Window.onKey, w);
        c.connect(area, "key-release-event", &Window.onKey, w);
        c.connect(area, "focus-in-event", &Window.onFocusIn, w);
        c.connect(area, "focus-out-event", &Window.onFocusOut, w);
        c.connect(win, "destroy", &Window.onDestroy, w);
        c.gtk.container_add(win, area);
        return w;
    }
};

pub const Window = struct {
    backend: *Backend,
    handler: platform.Handler,
    window: *c.GtkWidget,
    area: *c.GtkWidget,
    /// The frame, in device pixels, and the cairo surface showing it.
    pixels: []u32 = &.{},
    surface: ?*c.cairo_surface_t = null,
    width: u32 = 0,
    height: u32 = 0,

    pub fn show(w: *Window) void {
        c.gtk.widget_show_all(w.window);
        c.gtk.widget_grab_focus(w.area);
    }

    /// Asks for a frame, which draw makes once GTK paints the window.
    pub fn requestFrame(w: *Window) void {
        c.gtk.widget_queue_draw(w.area);
    }

    pub fn close(w: *Window) void {
        c.gtk.widget_destroy(w.window);
    }

    fn onDraw(_: *c.GtkWidget, cr: *c.cairo_t, data: *Window) callconv(.c) c.gboolean {
        const w = data;
        const scale = c.gtk.widget_get_scale_factor(w.area);
        const dw: u32 = @intCast(c.gtk.widget_get_allocated_width(w.area));
        const dh: u32 = @intCast(c.gtk.widget_get_allocated_height(w.area));
        const pw = dw * @as(u32, @intCast(scale));
        const ph = dh * @as(u32, @intCast(scale));
        if (pw == 0 or ph == 0) return 1;
        if (pw != w.width or ph != w.height) {
            w.resize(pw, ph, scale) catch return 1;
        }
        const target: platform.Target = .{ .pixels = w.pixels, .width = pw, .height = ph, .stride = pw, .scale = @floatFromInt(scale) };
        w.handler.draw(&target, @floatFromInt(dw), @floatFromInt(dh));
        const surface = w.surface.?;
        c.cairo.surface_mark_dirty(surface);
        c.cairo.set_source_surface(cr, surface, 0, 0);
        c.cairo.set_operator(cr, .source);
        c.cairo.paint(cr);
        return 1;
    }

    fn resize(w: *Window, pw: u32, ph: u32, scale: c_int) Allocator.Error!void {
        const gpa = w.backend.gpa;
        const pixels = try gpa.alloc(u32, @as(usize, pw) * ph);
        w.freeFrame();
        w.pixels, w.width, w.height = .{ pixels, pw, ph };
        const s = c.cairo.image_surface_create_for_data(@ptrCast(pixels.ptr), .argb32, @intCast(pw), @intCast(ph), @intCast(pw * 4));
        c.cairo.surface_set_device_scale(s, @floatFromInt(scale), @floatFromInt(scale));
        w.surface = s;
    }

    fn freeFrame(w: *Window) void {
        if (w.surface) |s| c.cairo.surface_destroy(s);
        w.backend.gpa.free(w.pixels);
        w.surface, w.pixels, w.width, w.height = .{ null, &.{}, 0, 0 };
    }

    fn onButton(_: *c.GtkWidget, ev: *const c.GdkEventButton, data: *Window) callconv(.c) c.gboolean {
        const pos: platform.Point = .{ .x = @floatCast(ev.x), .y = @floatCast(ev.y) };
        const button: platform.MouseButton = switch (ev.button) {
            1 => .left,
            2 => .middle,
            3 => .right,
            else => .other,
        };
        const mods = modsOf(ev.state);
        switch (ev.type) {
            c.GdkEventButtonType.press => {
                c.gtk.widget_grab_focus(data.area);
                data.handler.event(.{ .pointer_down = .{ .pos = pos, .button = button, .mods = mods } });
            },
            c.GdkEventButtonType.release => data.handler.event(.{ .pointer_up = .{ .pos = pos, .button = button, .mods = mods } }),
            else => {}, // double and triple presses follow presses of their own
        }
        return 1;
    }

    fn onMotion(_: *c.GtkWidget, ev: *const c.GdkEventMotion, data: *Window) callconv(.c) c.gboolean {
        data.handler.event(.{ .pointer_move = .{ .x = @floatCast(ev.x), .y = @floatCast(ev.y) } });
        return 1;
    }

    fn onLeave(_: *c.GtkWidget, _: *anyopaque, data: *Window) callconv(.c) c.gboolean {
        data.handler.event(.pointer_leave);
        return 1;
    }

    fn onKey(_: *c.GtkWidget, ev: *const c.GdkEventKey, data: *Window) callconv(.c) c.gboolean {
        const key = keyOf(ev.keyval);
        const mods = modsOf(ev.state);
        // GTK reports Shift+Tab as ISO_Left_Tab with Shift.
        if (ev.type == 8) { // GDK_KEY_PRESS
            data.handler.event(.{ .key_down = .{ .key = key, .mods = mods } });
        } else {
            data.handler.event(.{ .key_up = .{ .key = key, .mods = mods } });
        }
        return 1; // the content has the keys, Tab too
    }

    fn onFocusIn(_: *c.GtkWidget, _: *anyopaque, data: *Window) callconv(.c) c.gboolean {
        data.handler.event(.{ .focus = true });
        return 0;
    }

    fn onFocusOut(_: *c.GtkWidget, _: *anyopaque, data: *Window) callconv(.c) c.gboolean {
        data.handler.event(.{ .focus = false });
        return 0;
    }

    fn onDestroy(_: *c.GtkWidget, data: *Window) callconv(.c) void {
        const w = data;
        w.freeFrame();
        const handler = w.handler;
        w.backend.gpa.destroy(w);
        handler.closed();
    }
};

fn modsOf(state: c_uint) platform.Mods {
    const m = c.modifier;
    return .{
        .shift = state & m.shift != 0,
        .ctrl = state & m.control != 0,
        .alt = state & m.mod1 != 0,
        .super = state & m.super != 0,
    };
}

fn keyOf(keyval: c_uint) platform.Key {
    return switch (keyval) {
        0xff52, 0xff97 => .up,
        0xff54, 0xff99 => .down,
        0xff51, 0xff96 => .left,
        0xff53, 0xff98 => .right,
        0xff09, 0xfe20 => .tab,
        0xff0d, 0xff8d => .enter,
        0x20, 0xff80 => .space,
        0xff1b => .escape,
        0xff08 => .backspace,
        0xffff, 0xff9f => .delete,
        0xff50, 0xff95 => .home,
        0xff57, 0xff9c => .end,
        0xff55, 0xff9a => .page_up,
        0xff56, 0xff9b => .page_down,
        else => .unknown,
    };
}
