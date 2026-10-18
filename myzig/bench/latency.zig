//! Measures a counter app as a user meets it, on an X display (Xvfb in
//! CI): how long it takes from exec to its first frame, how long from a
//! click on "+" to the frame showing the new count, and the CPU time the
//! app spends on each click. It drives the display with XTest and watches
//! it with XGetImage, so it measures MyGo's examples/counter-native and
//! MyZig's examples/counter alike, from outside.
//!
//!     zig build bench -- [-runs 10] [-clicks 200] app...
//!
//! The window is expected 360x260 DIPs, centered on the screen, at scale 1.
const std = @import("std");

const Display = opaque {};
const Window = c_ulong;
const XImage = extern struct {
    width: c_int,
    height: c_int,
    xoffset: c_int,
    format: c_int,
    data: [*]u8,
    byte_order: c_int,
    bitmap_unit: c_int,
    bitmap_bit_order: c_int,
    bitmap_pad: c_int,
    depth: c_int,
    bytes_per_line: c_int,
    bits_per_pixel: c_int,
};

const X = struct {
    pub const soname = "libX11.so.6";
    XOpenDisplay: *const fn (?[*:0]const u8) callconv(.c) ?*Display,
    XDefaultRootWindow: *const fn (*Display) callconv(.c) Window,
    XDisplayWidth: *const fn (*Display, c_int) callconv(.c) c_int,
    XDisplayHeight: *const fn (*Display, c_int) callconv(.c) c_int,
    XGetImage: *const fn (*Display, Window, c_int, c_int, c_uint, c_uint, c_ulong, c_int) callconv(.c) ?*XImage,
    XGetSubImage: *const fn (*Display, Window, c_int, c_int, c_uint, c_uint, c_ulong, c_int, *XImage, c_int, c_int) callconv(.c) ?*XImage,
    XSync: *const fn (*Display, c_int) callconv(.c) c_int,
};
const Xtst = struct {
    pub const soname = "libXtst.so.6";
    XTestFakeMotionEvent: *const fn (*Display, c_int, c_int, c_int, c_ulong) callconv(.c) c_int,
    XTestFakeButtonEvent: *const fn (*Display, c_uint, c_int, c_ulong) callconv(.c) c_int,
};

var x: X = undefined;
var xtst: Xtst = undefined;

fn load(api: anytype) !void {
    const Api = @TypeOf(api.*);
    var lib = try std.DynLib.openZ(Api.soname);
    inline for (@typeInfo(Api).@"struct".fields) |f| {
        @field(api, f.name) = lib.lookup(f.type, f.name) orelse return error.SymbolNotFound;
    }
}

fn now() u64 {
    var ts: std.c.timespec = undefined;
    _ = std.c.clock_gettime(.MONOTONIC, &ts);
    return @as(u64, @intCast(ts.sec)) * std.time.ns_per_s + @as(u64, @intCast(ts.nsec));
}

fn sleepMs(n: u64) void {
    const ts: std.c.timespec = .{ .sec = @intCast(n / 1000), .nsec = @intCast(n % 1000 * std.time.ns_per_ms) };
    _ = std.c.nanosleep(&ts, null);
}

/// The CPU time the process ran, from /proc/<pid>/schedstat, in ns.
fn cpuTime(pid: std.c.pid_t) !u64 {
    var path_buf: [64]u8 = undefined;
    const path = try std.fmt.bufPrintZ(&path_buf, "/proc/{d}/schedstat", .{pid});
    const fd = std.c.open(path, .{});
    if (fd < 0) return error.NoProcess;
    defer _ = std.c.close(fd);
    var buf: [128]u8 = undefined;
    const n = std.c.read(fd, &buf, buf.len);
    if (n <= 0) return error.NoProcess;
    var it = std.mem.tokenizeScalar(u8, buf[0..@intCast(n)], ' ');
    return std.fmt.parseInt(u64, it.next() orelse return error.NoProcess, 10);
}

const Screen = struct {
    dpy: *Display,
    root: Window,
    /// The window's top left corner.
    ox: c_int,
    oy: c_int,
    probe: *XImage,
    region: *XImage,

    /// The pixel at (wx, wy) of the window, as 0xRRGGBB.
    fn pixel(s: *Screen, wx: c_int, wy: c_int) u32 {
        _ = x.XGetSubImage(s.dpy, s.root, s.ox + wx, s.oy + wy, 1, 1, ~@as(c_ulong, 0), 2, s.probe, 0, 0);
        return std.mem.readInt(u32, s.probe.data[0..4], .little) & 0xffffff;
    }

    /// The pixels of the count, at the middle of the window's top half.
    fn count(s: *Screen, out: []u8) void {
        _ = x.XGetSubImage(s.dpy, s.root, s.ox + 140, s.oy + 40, 80, 100, ~@as(c_ulong, 0), 2, s.region, 0, 0);
        @memcpy(out, s.region.data[0..out.len]);
    }
};

const plus = .{ 228, 150 }; // the "+" button, off its glyph, in the window
const accent = 0x2563eb;

const Result = struct { startup: []u64, latency: []u64, cpu_per_click: []u64 };

fn measure(gpa: std.mem.Allocator, s: *Screen, argv: [*:null]const ?[*:0]const u8, runs: usize, clicks: usize) !Result {
    var res: Result = .{
        .startup = try gpa.alloc(u64, runs),
        .latency = try gpa.alloc(u64, runs * clicks),
        .cpu_per_click = try gpa.alloc(u64, runs),
    };
    const size: usize = @intCast(s.region.bytes_per_line * 100);
    const before = try gpa.alloc(u8, size);
    defer gpa.free(before);
    const after = try gpa.alloc(u8, size);
    defer gpa.free(after);
    // Away from the window, so that the first frame is without hover.
    _ = xtst.XTestFakeMotionEvent(s.dpy, -1, 0, 0, 0);
    _ = x.XSync(s.dpy, 0);
    for (0..runs) |run| {
        const t0 = now();
        const pid = std.c.fork();
        if (pid == 0) {
            _ = std.c.execve(argv[0].?, argv, @ptrCast(std.c.environ));
            std.c._exit(127);
        }
        while (s.pixel(plus[0], plus[1]) != accent) {
            if (now() - t0 > 20 * std.time.ns_per_s) return error.NoFirstFrame;
        }
        res.startup[run] = now() - t0;
        sleepMs(500);
        _ = xtst.XTestFakeMotionEvent(s.dpy, -1, s.ox + plus[0], s.oy + plus[1], 0);
        _ = x.XSync(s.dpy, 0);
        sleepMs(200);
        const cpu0 = try cpuTime(pid);
        for (0..clicks) |i| {
            s.count(before);
            _ = xtst.XTestFakeButtonEvent(s.dpy, 1, 1, 0);
            _ = xtst.XTestFakeButtonEvent(s.dpy, 1, 0, 0);
            _ = x.XSync(s.dpy, 0);
            const t = now();
            while (true) {
                s.count(after);
                if (!std.mem.eql(u8, before, after)) break;
                if (now() - t > 5 * std.time.ns_per_s) return error.NoFrame;
            }
            res.latency[run * clicks + i] = now() - t;
        }
        sleepMs(100);
        res.cpu_per_click[run] = (try cpuTime(pid) - cpu0) / clicks;
        _ = std.c.kill(pid, .TERM);
        _ = std.c.waitpid(pid, null, 0);
        _ = xtst.XTestFakeMotionEvent(s.dpy, -1, 0, 0, 0);
        _ = x.XSync(s.dpy, 0);
        sleepMs(300);
    }
    return res;
}

fn ms(ns: u64) f64 {
    return @as(f64, @floatFromInt(ns)) / std.time.ns_per_ms;
}

fn percentile(v: []u64, p: f64) u64 {
    std.mem.sort(u64, v, {}, std.sort.asc(u64));
    const i: usize = @intFromFloat(@round(p * @as(f64, @floatFromInt(v.len - 1))));
    return v[i];
}

pub fn main(init: std.process.Init) !void {
    const gpa = init.gpa;
    const args = try init.minimal.args.toSlice(init.arena.allocator());
    var runs: usize = 10;
    var clicks: usize = 200;
    var apps: std.ArrayList([:0]const u8) = .empty;
    var i: usize = 1;
    while (i < args.len) : (i += 1) {
        if (std.mem.eql(u8, args[i], "-runs")) {
            i += 1;
            runs = try std.fmt.parseInt(usize, args[i], 10);
        } else if (std.mem.eql(u8, args[i], "-clicks")) {
            i += 1;
            clicks = try std.fmt.parseInt(usize, args[i], 10);
        } else try apps.append(init.arena.allocator(), args[i]);
    }
    try load(&x);
    try load(&xtst);
    const dpy = x.XOpenDisplay(null) orelse return error.NoDisplay;
    const root = x.XDefaultRootWindow(dpy);
    var s: Screen = .{
        .dpy = dpy,
        .root = root,
        .ox = @divTrunc(x.XDisplayWidth(dpy, 0) - 360, 2),
        .oy = @divTrunc(x.XDisplayHeight(dpy, 0) - 260, 2),
        .probe = x.XGetImage(dpy, root, 0, 0, 1, 1, ~@as(c_ulong, 0), 2) orelse return error.NoImage,
        .region = x.XGetImage(dpy, root, 0, 0, 80, 100, ~@as(c_ulong, 0), 2) orelse return error.NoImage,
    };
    for (apps.items) |app| {
        const argv = [_:null]?[*:0]const u8{app.ptr};
        const r = try measure(gpa, &s, &argv, runs, clicks);
        std.debug.print("{s}: first frame {d:.1} ms (median of {d}), click to frame {d:.2} ms median, {d:.2} ms p90 ({d} clicks), CPU {d:.3} ms a click\n", .{
            std.fs.path.basename(app),            ms(percentile(r.startup, 0.5)), runs,
            ms(percentile(r.latency, 0.5)),       ms(percentile(r.latency, 0.9)), r.latency.len,
            ms(percentile(r.cpu_per_click, 0.5)),
        });
    }
}
