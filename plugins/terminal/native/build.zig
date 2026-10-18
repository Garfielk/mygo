const std = @import("std");

// libghostty-vt, the terminal emulator of Ghostty, from Ghostty's sources at
// the commit build.zig.zon pins. mygo native build builds it for every
// platform, with the Zig version Ghostty asks for (minimum_zig_version).
pub fn build(b: *std.Build) void {
    const ghostty = b.dependency("ghostty", .{
        .target = b.standardTargetOptions(.{}),
        .optimize = b.standardOptimizeOption(.{}),
        .@"emit-lib-vt" = true,
        .strip = true,
    });
    b.installArtifact(ghostty.artifact("ghostty-vt"));
}
