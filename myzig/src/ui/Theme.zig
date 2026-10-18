//! The colors and metrics widgets use, as MyGo's ui.Theme. Change a copy
//! of `light` or `dark` and give it to the window's runtime.
const builtin = @import("builtin");
const Color = @import("../gfx.zig").Color;

const Theme = @This();

is_dark: bool = false,
/// Fills the window.
background: Color,
/// The face of buttons and other controls, and while hovered or pressed.
surface: Color,
surface_hover: Color,
surface_pressed: Color,
/// Outlines controls.
border: Color,
text: Color,
/// Secondary text and placeholders.
text_muted: Color,
/// Primary buttons, checked controls and focus rings, and text on them.
accent: Color,
accent_hover: Color,
accent_pressed: Color,
accent_text: Color,
danger: Color,
warning: Color,
success: Color,
selection: Color,
/// The ring around the control with the keyboard focus.
focus: Color,
/// Rounds the corners of controls.
radius: f32 = 6,
/// The unit of the room widgets leave inside and between their parts.
spacing: f32 = 4,
/// The size of text.
font_size: f32 = default_font_size,

pub const default_font_size: f32 = if (builtin.os.tag.isDarwin()) 13 else 14;

/// n units of spacing.
pub fn space(t: *const Theme, n: f32) f32 {
    return n * t.spacing;
}

pub const light: Theme = .{
    .background = .hex("#ffffff"),
    .surface = .hex("#f4f4f5"),
    .surface_hover = .hex("#e9e9ec"),
    .surface_pressed = .hex("#dddde1"),
    .border = .hex("#d9d9de"),
    .text = .hex("#18181b"),
    .text_muted = .hex("#71717a"),
    .accent = .hex("#2563eb"),
    .accent_hover = .hex("#1d4ed8"),
    .accent_pressed = .hex("#1e40af"),
    .accent_text = .hex("#ffffff"),
    .danger = .hex("#dc2626"),
    .warning = .hex("#d97706"),
    .success = .hex("#16a34a"),
    .selection = .rgba(37, 99, 235, 0.25),
    .focus = .rgba(37, 99, 235, 0.55),
};

pub const dark: Theme = .{
    .is_dark = true,
    .background = .hex("#18181b"),
    .surface = .hex("#27272a"),
    .surface_hover = .hex("#323236"),
    .surface_pressed = .hex("#3c3c41"),
    .border = .hex("#3f3f46"),
    .text = .hex("#f4f4f5"),
    .text_muted = .hex("#a1a1aa"),
    .accent = .hex("#3b82f6"),
    .accent_hover = .hex("#60a5fa"),
    .accent_pressed = .hex("#2563eb"),
    .accent_text = .hex("#ffffff"),
    .danger = .hex("#ef4444"),
    .warning = .hex("#f59e0b"),
    .success = .hex("#22c55e"),
    .selection = .rgba(59, 130, 246, 0.4),
    .focus = .rgba(96, 165, 250, 0.6),
};
