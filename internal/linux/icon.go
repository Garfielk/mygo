//go:build linux && (amd64 || arm64)

package linux

import (
	"fmt"
	"os"
	"strings"
	"sync"
)

var (
	iconOnce sync.Once

	gFileQueryInfo             func(file ptr, attributes *byte, flags uint32, cancellable ptr, gerr *ptr) ptr
	gFileInfoGetIcon           func(info ptr) ptr
	gDesktopAppInfoNewFromFile func(path *byte) ptr
	gAppInfoGetIcon            func(info ptr) ptr
	gtkIconThemeGetDefault     func() ptr
	gtkIconThemeLookupByGicon  func(theme, icon ptr, size int32, flags uint32) ptr
	gtkIconInfoLoadIcon        func(info ptr, gerr *ptr) ptr
)

func loadIcons() {
	iconOnce.Do(func() {
		mustBind(libGIO, &gFileQueryInfo, "g_file_query_info")
		mustBind(libGIO, &gFileInfoGetIcon, "g_file_info_get_icon")
		mustBind(libGIO, &gDesktopAppInfoNewFromFile, "g_desktop_app_info_new_from_filename")
		mustBind(libGIO, &gAppInfoGetIcon, "g_app_info_get_icon")
		mustBind(libGTK, &gtkIconThemeGetDefault, "gtk_icon_theme_get_default")
		mustBind(libGTK, &gtkIconThemeLookupByGicon, "gtk_icon_theme_lookup_by_gicon")
		mustBind(libGTK, &gtkIconInfoLoadIcon, "gtk_icon_info_load_icon")
	})
}

// FileIcon returns the icon the file manager shows for a file or a folder,
// that of its content type (GIO's standard::icon), or the icon of the
// application of a desktop entry, from the icon theme at size by size
// pixels.
func (shell) FileIcon(path string, size int) ([]byte, error) {
	if _, err := os.Stat(path); err != nil {
		return nil, err
	}
	loadIcons()
	var icon, owner ptr // owner holds icon
	if strings.HasSuffix(path, ".desktop") {
		if owner = gDesktopAppInfoNewFromFile(cs(path)); owner != 0 {
			icon = gAppInfoGetIcon(owner)
		}
	}
	if icon == 0 {
		if owner != 0 {
			gObjectUnref(owner)
		}
		f := gFileNewForPath(cs(path))
		var gerr ptr
		owner = gFileQueryInfo(f, cs("standard::icon"), 0, 0, &gerr)
		gObjectUnref(f)
		if owner == 0 {
			return nil, gErr(gerr)
		}
		icon = gFileInfoGetIcon(owner)
	}
	defer gObjectUnref(owner)
	if icon == 0 {
		return nil, fmt.Errorf("mygo: no icon for %q", path)
	}
	// GIO's themed icons list their generic names themselves, and GTK takes
	// GTK_ICON_LOOKUP_GENERIC_FALLBACK only with names of its own.
	const flags = 1 << 4 // GTK_ICON_LOOKUP_FORCE_SIZE
	info := gtkIconThemeLookupByGicon(gtkIconThemeGetDefault(), icon, int32(size), flags)
	if info == 0 {
		return nil, fmt.Errorf("mygo: the icon theme has no icon for %q", path)
	}
	defer gObjectUnref(info)
	var gerr ptr
	pix := gtkIconInfoLoadIcon(info, &gerr)
	if pix == 0 {
		return nil, gErr(gerr)
	}
	defer gObjectUnref(pix)
	png := pngFromPixbuf(pix)
	if png == nil {
		return nil, fmt.Errorf("mygo: cannot encode the icon of %q", path)
	}
	return png, nil
}
