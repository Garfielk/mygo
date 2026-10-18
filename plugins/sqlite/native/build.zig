const std = @import("std");

// SQLite's amalgamation, which build.zig.zon pins, and the plugin's C shim,
// as a dynamic library: mygo native build builds it for every platform.
// Nothing here, including Zig, is needed by the application at run time.
pub fn build(b: *std.Build) void {
    const target = b.standardTargetOptions(.{});
    const optimize = b.standardOptimizeOption(.{});
    const source: std.Build.LazyPath = if (b.option([]const u8, "sqlite", "Directory of another sqlite3.c and sqlite3.h")) |dir|
        .{ .cwd_relative = dir }
    else
        b.dependency("sqlite", .{}).path("");
    const module = b.createModule(.{
        .target = target,
        .optimize = optimize,
        .link_libc = true,
        .strip = true,
        .pic = true,
    });
    module.addIncludePath(source);
    module.addCSourceFiles(.{ .root = source, .files = &.{"sqlite3.c"}, .flags = &.{"-std=c11"} });
    module.addCSourceFile(.{ .file = b.path("sqlite.c"), .flags = &.{"-std=c11"} });
    module.addCMacro("SQLITE_THREADSAFE", "1");
    module.addCMacro("SQLITE_DQS", "0");
    module.addCMacro("SQLITE_DEFAULT_FOREIGN_KEYS", "1");
    module.addCMacro("SQLITE_ENABLE_FTS5", "1");
    module.addCMacro("SQLITE_ENABLE_RTREE", "1");
    module.addCMacro("SQLITE_OMIT_LOAD_EXTENSION", "1");
    module.addCMacro("SQLITE_OMIT_DEPRECATED", "1");
    module.addCMacro("SQLITE_OMIT_SHARED_CACHE", "1");
    if (target.result.os.tag == .windows) {
        module.addCMacro("SQLITE_API", "__declspec(dllexport)");
    } else {
        module.linkSystemLibrary("m", .{});
        module.linkSystemLibrary("pthread", .{});
    }
    const library = b.addLibrary(.{ .name = "mygo-sqlite3", .linkage = .dynamic, .root_module = module });
    b.installArtifact(library);
}
