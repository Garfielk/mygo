//! The C libraries of the Linux backend, loaded at run time.
//!
//! Each library is a struct of function pointers named after its functions
//! less the library's prefix, which `load` fills in from the shared object
//! by comptime reflection: declaring a field is all binding a function
//! takes, and calls read as Zig's (`gtk.window_new(.toplevel)`). Optional
//! fields are symbols that older versions lack.
//!
//! Nothing links against these libraries, so a program builds on any
//! machine for any other, and one that never opens a window never loads
//! GTK: MyGo binds them with purego for the same reasons.
const std = @import("std");

pub const gboolean = c_int;
pub const gulong = c_ulong;
pub const GCallback = *const fn () callconv(.c) void;
pub const GSourceFunc = *const fn (?*anyopaque) callconv(.c) gboolean;
pub const GDestroyNotify = *const fn (?*anyopaque) callconv(.c) void;

pub const GObject = opaque {};
pub const GtkWidget = opaque {};
pub const GtkSettings = opaque {};
pub const GdkWindow = opaque {};
pub const GdkScreen = opaque {};
pub const GdkDevice = opaque {};
pub const cairo_t = opaque {};
pub const cairo_surface_t = opaque {};
pub const cairo_font_options_t = opaque {};
pub const PangoContext = opaque {};
pub const PangoLayout = opaque {};
pub const PangoFontMap = opaque {};
pub const PangoFontDescription = opaque {};

pub const PANGO_SCALE = 1024;

pub const GtkWindowType = enum(c_int) { toplevel = 0, popup = 1 };
pub const GtkWindowPosition = enum(c_int) { none = 0, center = 1, mouse = 2 };
pub const cairo_format_t = enum(c_int) { argb32 = 0, rgb24 = 1, a8 = 2 };
pub const cairo_operator_t = enum(c_int) { clear = 0, source = 1, over = 2 };

/// GdkEventMask bits the content's drawing area asks for.
pub const event_mask = struct {
    pub const pointer_motion = 1 << 2;
    pub const button_press = 1 << 8;
    pub const button_release = 1 << 9;
    pub const key_press = 1 << 10;
    pub const key_release = 1 << 11;
    pub const enter_notify = 1 << 12;
    pub const leave_notify = 1 << 13;
    pub const focus_change = 1 << 14;
    pub const scroll = 1 << 21;
    pub const smooth_scroll = 1 << 23;
};

/// GdkModifierType bits.
pub const modifier = struct {
    pub const shift = 1 << 0;
    pub const control = 1 << 2;
    pub const mod1 = 1 << 3; // Alt
    pub const super = 1 << 26;
};

/// GDK's event structs, which signals pass, laid out as C lays them out.
pub const GdkEventButton = extern struct {
    type: c_int,
    window: ?*GdkWindow,
    send_event: i8,
    time: u32,
    x: f64,
    y: f64,
    axes: ?*f64,
    state: c_uint,
    button: c_uint,
    device: ?*GdkDevice,
    x_root: f64,
    y_root: f64,
};

pub const GdkEventMotion = extern struct {
    type: c_int,
    window: ?*GdkWindow,
    send_event: i8,
    time: u32,
    x: f64,
    y: f64,
    axes: ?*f64,
    state: c_uint,
    is_hint: i16,
    device: ?*GdkDevice,
    x_root: f64,
    y_root: f64,
};

pub const GdkEventKey = extern struct {
    type: c_int,
    window: ?*GdkWindow,
    send_event: i8,
    time: u32,
    state: c_uint,
    keyval: c_uint,
    length: c_int,
    string: ?[*:0]u8,
    hardware_keycode: u16,
    group: u8,
};

pub const GdkEventButtonType = struct {
    pub const press = 4;
    pub const double_press = 5;
    pub const triple_press = 6;
    pub const release = 7;
};

pub const GLib = struct {
    pub const soname = "libglib-2.0.so.0";
    pub const prefix = "g_";
    idle_add_full: *const fn (priority: c_int, function: GSourceFunc, data: ?*anyopaque, notify: ?GDestroyNotify) callconv(.c) c_uint,
    free: *const fn (mem: ?*anyopaque) callconv(.c) void,
};

pub const GObjectLib = struct {
    pub const soname = "libgobject-2.0.so.0";
    pub const prefix = "g_";
    signal_connect_data: *const fn (instance: *anyopaque, signal: [*:0]const u8, handler: GCallback, data: ?*anyopaque, destroy: ?*anyopaque, flags: c_int) callconv(.c) gulong,
    object_get: *const fn (object: *anyopaque, first_property: [*:0]const u8, ...) callconv(.c) void,
    object_unref: *const fn (object: *anyopaque) callconv(.c) void,
};

pub const Gdk = struct {
    pub const soname = "libgdk-3.so.0";
    pub const prefix = "gdk_";
    screen_get_default: *const fn () callconv(.c) ?*GdkScreen,
    screen_get_font_options: *const fn (screen: *GdkScreen) callconv(.c) ?*const cairo_font_options_t,
    keyval_to_unicode: *const fn (keyval: c_uint) callconv(.c) u32,
};

pub const Gtk = struct {
    pub const soname = "libgtk-3.so.0";
    pub const prefix = "gtk_";
    init_check: *const fn (argc: ?*c_int, argv: ?*anyopaque) callconv(.c) gboolean,
    main: *const fn () callconv(.c) void,
    main_quit: *const fn () callconv(.c) void,
    window_new: *const fn (kind: GtkWindowType) callconv(.c) *GtkWidget,
    window_set_title: *const fn (window: *GtkWidget, title: [*:0]const u8) callconv(.c) void,
    window_set_default_size: *const fn (window: *GtkWidget, width: c_int, height: c_int) callconv(.c) void,
    window_set_position: *const fn (window: *GtkWidget, position: GtkWindowPosition) callconv(.c) void,
    drawing_area_new: *const fn () callconv(.c) *GtkWidget,
    container_add: *const fn (container: *GtkWidget, child: *GtkWidget) callconv(.c) void,
    widget_set_can_focus: *const fn (widget: *GtkWidget, can: gboolean) callconv(.c) void,
    widget_add_events: *const fn (widget: *GtkWidget, events: c_int) callconv(.c) void,
    widget_queue_draw: *const fn (widget: *GtkWidget) callconv(.c) void,
    widget_get_allocated_width: *const fn (widget: *GtkWidget) callconv(.c) c_int,
    widget_get_allocated_height: *const fn (widget: *GtkWidget) callconv(.c) c_int,
    widget_get_scale_factor: *const fn (widget: *GtkWidget) callconv(.c) c_int,
    widget_show_all: *const fn (widget: *GtkWidget) callconv(.c) void,
    widget_grab_focus: *const fn (widget: *GtkWidget) callconv(.c) void,
    widget_destroy: *const fn (widget: *GtkWidget) callconv(.c) void,
    settings_get_default: *const fn () callconv(.c) ?*GtkSettings,
};

pub const Cairo = struct {
    pub const soname = "libcairo.so.2";
    pub const prefix = "cairo_";
    image_surface_create_for_data: *const fn (data: [*]u8, format: cairo_format_t, width: c_int, height: c_int, stride: c_int) callconv(.c) *cairo_surface_t,
    surface_destroy: *const fn (surface: *cairo_surface_t) callconv(.c) void,
    surface_flush: *const fn (surface: *cairo_surface_t) callconv(.c) void,
    surface_mark_dirty: *const fn (surface: *cairo_surface_t) callconv(.c) void,
    surface_set_device_scale: *const fn (surface: *cairo_surface_t, x: f64, y: f64) callconv(.c) void,
    create: *const fn (target: *cairo_surface_t) callconv(.c) *cairo_t,
    destroy: *const fn (cr: *cairo_t) callconv(.c) void,
    set_source_surface: *const fn (cr: *cairo_t, surface: *cairo_surface_t, x: f64, y: f64) callconv(.c) void,
    set_source_rgba: *const fn (cr: *cairo_t, r: f64, g: f64, b: f64, a: f64) callconv(.c) void,
    set_operator: *const fn (cr: *cairo_t, op: cairo_operator_t) callconv(.c) void,
    paint: *const fn (cr: *cairo_t) callconv(.c) void,
    move_to: *const fn (cr: *cairo_t, x: f64, y: f64) callconv(.c) void,
};

pub const Pango = struct {
    pub const soname = "libpango-1.0.so.0";
    pub const prefix = "pango_";
    font_description_new: *const fn () callconv(.c) *PangoFontDescription,
    font_description_free: *const fn (desc: *PangoFontDescription) callconv(.c) void,
    font_description_from_string: *const fn (s: [*:0]const u8) callconv(.c) *PangoFontDescription,
    font_description_get_family: *const fn (desc: *const PangoFontDescription) callconv(.c) ?[*:0]const u8,
    font_description_set_family: *const fn (desc: *PangoFontDescription, family: [*:0]const u8) callconv(.c) void,
    font_description_set_absolute_size: *const fn (desc: *PangoFontDescription, size: f64) callconv(.c) void,
    font_description_set_weight: *const fn (desc: *PangoFontDescription, weight: c_int) callconv(.c) void,
    font_map_create_context: *const fn (map: *PangoFontMap) callconv(.c) *PangoContext,
    layout_new: *const fn (context: *PangoContext) callconv(.c) *PangoLayout,
    layout_set_text: *const fn (layout: *PangoLayout, text: [*]const u8, length: c_int) callconv(.c) void,
    layout_set_font_description: *const fn (layout: *PangoLayout, desc: ?*const PangoFontDescription) callconv(.c) void,
    layout_get_size: *const fn (layout: *PangoLayout, width: *c_int, height: *c_int) callconv(.c) void,
};

pub const PangoCairo = struct {
    pub const soname = "libpangocairo-1.0.so.0";
    pub const prefix = "pango_cairo_";
    font_map_get_default: *const fn () callconv(.c) *PangoFontMap,
    context_set_font_options: *const fn (context: *PangoContext, options: ?*const cairo_font_options_t) callconv(.c) void,
    show_layout: *const fn (cr: *cairo_t, layout: *PangoLayout) callconv(.c) void,
};

pub var glib: GLib = undefined;
pub var gobject: GObjectLib = undefined;
pub var gdk: Gdk = undefined;
pub var gtk: Gtk = undefined;
pub var cairo: Cairo = undefined;
pub var pango: Pango = undefined;
pub var pango_cairo: PangoCairo = undefined;

pub const LoadError = error{ LibraryNotFound, SymbolNotFound };

/// Loads every library, once. The libraries stay loaded for the life of
/// the process, as GTK cannot be unloaded.
pub fn loadAll() LoadError!void {
    if (loaded) return;
    try load(&glib);
    try load(&gobject);
    try load(&gdk);
    try load(&gtk);
    try load(&cairo);
    try load(&pango);
    try load(&pango_cairo);
    loaded = true;
}

var loaded = false;

/// Fills api, a struct of function pointers, from its library.
pub fn load(api: anytype) LoadError!void {
    const Api = @TypeOf(api.*);
    var lib = std.DynLib.openZ(Api.soname) catch {
        std.log.err("myzig: cannot load {s}: install GTK 3", .{Api.soname});
        return error.LibraryNotFound;
    };
    inline for (@typeInfo(Api).@"struct".fields) |field| {
        const name = Api.prefix ++ field.name;
        switch (@typeInfo(field.type)) {
            .optional => |o| @field(api, field.name) = lib.lookup(o.child, name),
            else => @field(api, field.name) = lib.lookup(field.type, name) orelse {
                std.log.err("myzig: {s} has no {s}", .{ Api.soname, name });
                return error.SymbolNotFound;
            },
        }
    }
}

/// Connects a typed handler to a signal of a GObject.
pub fn connect(instance: *anyopaque, signal: [*:0]const u8, handler: anytype, data: ?*anyopaque) void {
    _ = gobject.signal_connect_data(instance, signal, @ptrCast(handler), data, null, 0);
}
