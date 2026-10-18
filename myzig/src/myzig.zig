//! MyZig: desktop apps in Zig, with interfaces drawn natively, after MyGo.
//!
//! The layers are MyGo's: this package owns the behavior, the platform
//! backend translates it to the native toolkit (GTK on Linux), and the
//! UI toolkit draws windows' content from the app's state.
pub const App = @import("App.zig");
pub const Window = @import("Window.zig");
pub const ui = @import("ui.zig");
pub const text = @import("text.zig");
pub const gfx = @import("gfx.zig");
pub const platform = @import("platform.zig");

test {
    @import("std").testing.refAllDecls(@This());
    _ = gfx.raster;
}
