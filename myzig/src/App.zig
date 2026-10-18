//! The application: its windows and the event loop of the main thread.
//!
//!     var app = try myzig.App.init(gpa);
//!     defer app.deinit();
//!     _ = try app.createWindow(.{ .title = "Counter", .content = ui.view(&state, view) });
//!     app.run();
//!
//! An App stays where init put it while it has windows, which point to it.
const std = @import("std");
const platform = @import("platform.zig");
const text = @import("text.zig");
const ui = @import("ui.zig");
const Window = @import("Window.zig");

const App = @This();
const Allocator = std.mem.Allocator;

gpa: Allocator,
backend: platform.Backend,
/// The text system of every window, made with the first one.
text_system: ?text.Native = null,
theme: ui.Theme = .light,
windows: usize = 0,
/// Quit once the last window closes, as apps do on Linux and Windows.
quit_when_closed: bool = true,

pub const InitError = platform.Backend.InitError;

/// Connects to the display. Call it, and every method, on the main thread
/// but post, which works from any.
pub fn init(gpa: Allocator) InitError!App {
    return .{ .gpa = gpa, .backend = try .init(gpa) };
}

pub fn deinit(app: *App) void {
    if (app.text_system) |*t| t.deinit();
}

/// Runs the event loop until quit, or until the last window closes.
pub fn run(app: *App) void {
    app.backend.run();
}

pub fn quit(app: *App) void {
    app.backend.quit();
}

/// Runs task on the main thread soon. Safe from any thread.
pub fn post(app: *App, task: *platform.Task) void {
    app.backend.post(task);
}

/// Creates a window, which shows once created.
pub fn createWindow(app: *App, opts: Window.Options) !*Window {
    const ts = try app.ensureText();
    const w = try app.gpa.create(Window);
    errdefer app.gpa.destroy(w);
    w.* = .{
        .app = app,
        .native = undefined,
        .runtime = if (opts.content) |v| ui.Runtime.init(app.gpa, v, ts, app.theme) else null,
        .stats = Window.FrameStats.fromEnv(),
    };
    errdefer if (w.runtime) |*rt| rt.deinit();
    w.native = try app.backend.createWindow(.{ .title = opts.title, .width = opts.width, .height = opts.height }, w.handler());
    app.windows += 1;
    w.native.show();
    return w;
}

pub fn textSystem(app: *App) text.System {
    return app.text_system.?.system();
}

fn ensureText(app: *App) !text.System {
    if (app.text_system == null) {
        const family = app.backend.uiFontFamily(app.gpa);
        defer if (family) |f| app.gpa.free(f);
        app.text_system = try text.Native.init(app.gpa, family);
    }
    return app.text_system.?.system();
}

/// A window closed: the backend destroyed it.
pub fn windowClosed(app: *App) void {
    app.windows -= 1;
    if (app.windows == 0 and app.quit_when_closed) app.quit();
}
