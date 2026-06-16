package main

import (
	"bytes"
	"fmt"
	"image"
	_ "image/png"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
)

func iconThemeDirs() []string {
	dirs := []string{}

	if home := os.Getenv("HOME"); home != "" {
		dirs = append(dirs, filepath.Join(home, ".local/share/icons"))
	}

	xdgDirs := os.Getenv("XDG_DATA_DIRS")
	if xdgDirs == "" {
		xdgDirs = "/usr/local/share:/usr/share"
	}
	for _, d := range filepath.SplitList(xdgDirs) {
		dirs = append(dirs, filepath.Join(d, "icons"))
	}

	return uniquePaths(dirs)
}

func uniquePaths(paths []string) []string {
	seen := make(map[string]bool)
	res := make([]string, 0, len(paths))
	for _, p := range paths {
		if !seen[p] {
			seen[p] = true
			res = append(res, p)
		}
	}
	return res
}

func preferredThemes() []string {
	theme := detectGtkTheme()
	if theme != "" {
		return []string{theme, "hicolor", "Adwaita", "Papirus", "Humanity", "breeze", "gnome"}
	}
	return []string{"hicolor", "Adwaita", "Papirus", "Humanity", "breeze", "gnome"}
}

func detectGtkTheme() string {
	data, err := os.ReadFile(filepath.Join(os.Getenv("HOME"), ".config/gtk-4.0/settings.ini"))
	if err != nil {
		data, err = os.ReadFile(filepath.Join(os.Getenv("HOME"), ".config/gtk-3.0/settings.ini"))
		if err != nil {
			return ""
		}
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "gtk-icon-theme-name=") {
			return strings.TrimSpace(line[len("gtk-icon-theme-name="):])
		}
	}
	return ""
}

func sizeDirs(target int) []string {
	sizes := []int{}

	for _, base := range []int{16, 22, 24, 32, 48, 64, 72, 96, 128, 192, 256} {
		sizes = append(sizes, base)
	}

	sort.Slice(sizes, func(i, j int) bool {
		di := abs(sizes[i] - target)
		dj := abs(sizes[j] - target)
		if di != dj {
			return di < dj
		}
		return sizes[i] > sizes[j]
	})

	seen := make(map[int]bool)
	res := make([]string, 0, len(sizes))
	for _, s := range sizes {
		if seen[s] {
			continue
		}
		seen[s] = true
		res = append(res, fmt.Sprintf("%dx%d", s, s))
	}
	res = append(res, "scalable")
	return res
}

func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

func iconCategories() []string {
	return []string{"actions", "apps", "categories", "devices", "emblems", "mimetypes", "places", "status"}
}

func findSystemIcon(name string, targetSize int) (string, error) {
	themes := preferredThemes()
	dirs := iconThemeDirs()
	sDirs := sizeDirs(targetSize)
	cats := iconCategories()

	for _, base := range dirs {
		for _, theme := range themes {
			themeDir := filepath.Join(base, theme)
			if _, err := os.Stat(themeDir); os.IsNotExist(err) {
				continue
			}

			for _, sd := range sDirs {
				for _, cat := range cats {
					for _, ext := range []string{"png", "xpm"} {
						p := filepath.Join(themeDir, sd, cat, name+"."+ext)
						if fi, err := os.Stat(p); err == nil && !fi.IsDir() {
							return p, nil
						}
					}
				}
			}

			for _, sd := range sDirs {
				for _, cat := range cats {
					p := filepath.Join(themeDir, sd, cat, name+".svg")
					if fi, err := os.Stat(p); err == nil && !fi.IsDir() {
						return p, nil
					}
				}
			}
		}
	}

	return "", fmt.Errorf("system icon %q not found", name)
}

func svgToPNG(svgPath string, targetSize int) (image.Image, error) {
	cmd := exec.Command("rsvg-convert",
		"-w", strconv.Itoa(targetSize),
		"-h", strconv.Itoa(targetSize),
		"-f", "png",
		svgPath,
	)
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("rsvg-convert: %w", err)
	}
	img, _, err := image.Decode(bytes.NewReader(out))
	if err != nil {
		return nil, fmt.Errorf("decode converted svg: %w", err)
	}
	return img, nil
}

var iconSizeCache sync.Map

func loadSystemIcon(name string, targetSize int) (string, error) {
	type cacheKey struct {
		name string
		size int
	}
	key := cacheKey{name, targetSize}
	if cached, ok := iconSizeCache.Load(key); ok {
		return cached.(string), nil
	}
	path, err := findSystemIcon(name, targetSize)
	if err != nil {
		return "", err
	}
	iconSizeCache.Store(key, path)
	return path, nil
}

func isSystemIconRef(path string) bool {
	return strings.HasPrefix(path, "@")
}

func systemIconName(path string) string {
	return strings.TrimPrefix(path, "@")
}

func parseSizeDir(dirName string) (int, bool) {
	parts := strings.SplitN(dirName, "x", 2)
	if len(parts) != 2 {
		return 0, false
	}
	s, err := strconv.Atoi(parts[0])
	if err != nil {
		return 0, false
	}
	return s, true
}
