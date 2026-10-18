//! Counter is a counter in native UI: a window MyZig draws itself, with no
//! web page. The arrow keys count too. MyGo's examples/counter-native.
//!
//!     zig build counter
const std = @import("std");
const myzig = @import("myzig");
const ui = myzig.ui;

/// The state the window shows.
const Counter = struct {
    n: i64 = 0,

    /// Builds the interface from the state, on the main thread, whenever
    /// the window needs a frame.
    fn view(s: *Counter, c: *ui.Context) void {
        if (c.shortcut(.{}, .up)) s.n += 1;
        if (c.shortcut(.{}, .down)) s.n -= 1;
        const t = c.theme();
        c.column(.{ .fill = true, .center = true, .gap = 16 });
        defer c.end();
        _ = c.textf("{d}", .{s.n}, .{ .font_size = 56, .bold = true });
        {
            c.row(.{ .gap = 8 });
            defer c.end();
            if (c.button("−", .{ .key = "decrement", .label = "Decrement", .width = 44 }).clicked()) s.n -= 1;
            if (c.button("Reset", .{ .key = "reset", .disabled = s.n == 0 }).clicked()) s.n = 0;
            if (c.button("+", .{ .key = "increment", .label = "Increment", .width = 44, .variant = .primary }).clicked()) s.n += 1;
        }
        _ = c.text("↑ and ↓ count too", .{ .font_size = 12, .color = t.text_muted });
    }
};

pub fn main(init: std.process.Init) !void {
    var counter: Counter = .{};
    var app: myzig.App = try .init(init.gpa);
    defer app.deinit();
    _ = try app.createWindow(.{
        .title = "Counter",
        .width = 360,
        .height = 260,
        .content = ui.view(&counter, Counter.view),
    });
    app.run();
}

// The view runs without a window in tests, which click and press keys as a
// user would.
test "counter" {
    var s: Counter = .{};
    var tt: ui.Tester = .init(std.testing.allocator, ui.view(&s, Counter.view), 360, 260);
    defer tt.deinit();
    for ([_][]const u8{ "Increment", "Increment", "Decrement" }) |label| try tt.click(label);
    try std.testing.expectEqual(1, s.n);
    try std.testing.expect(tt.hasText("1"));
    tt.key(.{}, .up);
    tt.key(.{}, .up);
    tt.key(.{}, .down);
    try std.testing.expectEqual(2, s.n);
    try tt.click("Reset");
    try std.testing.expectEqual(0, s.n);
}
