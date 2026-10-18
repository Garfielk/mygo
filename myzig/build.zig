const std = @import("std");

// zig build                  the examples, in zig-out/bin
// zig build counter          run the counter example
// zig build test             unit tests: the UI runs headless, without GTK
// zig build -Doptimize=ReleaseSmall -Dstrip   an app's release size
pub fn build(b: *std.Build) void {
    const target = b.standardTargetOptions(.{});
    const optimize = b.standardOptimizeOption(.{});
    const strip = b.option(bool, "strip", "strip debug information from the examples");

    // The framework. Native libraries are loaded at run time with dlopen, as
    // MyGo does with purego: nothing links against GTK, so any machine
    // builds for any other without its headers or libraries.
    const myzig = b.addModule("myzig", .{
        .root_source_file = b.path("src/myzig.zig"),
        .target = target,
        .optimize = optimize,
        .link_libc = true,
    });

    const examples = [_][]const u8{"counter"};
    for (examples) |name| {
        const exe = b.addExecutable(.{
            .name = name,
            .root_module = b.createModule(.{
                .root_source_file = b.path(b.fmt("examples/{s}.zig", .{name})),
                .target = target,
                .optimize = optimize,
                .strip = strip,
                .imports = &.{.{ .name = "myzig", .module = myzig }},
            }),
        });
        b.installArtifact(exe);
        const run = b.addRunArtifact(exe);
        if (b.args) |args| run.addArgs(args);
        b.step(name, b.fmt("Run the {s} example", .{name})).dependOn(&run.step);
    }

    // Startup, click latency and CPU of counter apps, MyGo's too.
    const bench = b.addExecutable(.{
        .name = "latency",
        .root_module = b.createModule(.{
            .root_source_file = b.path("bench/latency.zig"),
            .target = target,
            .optimize = .ReleaseFast,
            .link_libc = true,
        }),
    });
    const run_bench = b.addRunArtifact(bench);
    if (b.args) |args| run_bench.addArgs(args);
    b.step("bench", "Measure counter apps on an X display").dependOn(&run_bench.step);

    const tests = b.step("test", "Run the unit tests");
    const lib_tests = b.addTest(.{ .root_module = myzig });
    tests.dependOn(&b.addRunArtifact(lib_tests).step);
    for (examples) |name| {
        const t = b.addTest(.{ .root_module = b.createModule(.{
            .root_source_file = b.path(b.fmt("examples/{s}.zig", .{name})),
            .target = target,
            .optimize = optimize,
            .imports = &.{.{ .name = "myzig", .module = myzig }},
        }) });
        tests.dependOn(&b.addRunArtifact(t).step);
    }
}
