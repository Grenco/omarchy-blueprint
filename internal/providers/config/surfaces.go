package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Grenco/omarchy-blueprint/internal/profile"
)

type SurfaceKind string

const (
	SurfaceRecursiveConfig SurfaceKind = "recursive-config"
	SurfaceExactHome       SurfaceKind = "exact-home"
)

type HomeConfigSpec struct{ Path string }

func DefaultHomeConfigSpecs() []HomeConfigSpec {
	paths := strings.Fields(`.bashrc .bash_profile .bash_login .bash_logout .bash_aliases .profile .zshrc .zprofile .zlogin .zlogout .zshenv .p10k.zsh .zsh_plugins.txt .inputrc .hushlogin .aliases .functions .exports .tmux.conf .screenrc .wezterm.lua .alacritty.yml .alacritty.toml .vimrc .gvimrc .exrc .nanorc .ideavimrc .emacs .emacs.el .spacemacs .gitconfig .gitignore .gitignore_global .gitmessage .gitmessage.txt .gitattributes_global .tigrc .editorconfig .dircolors .ctags .ackrc .agignore .ripgreprc .fdignore .ignore .lesskey .XCompose .Xresources .Xdefaults .xprofile .xinitrc .pam_environment .gtkrc-2.0 .pythonrc .pythonrc.py .irbrc .pryrc .Rprofile .ghci .haskeline .iex.exs .ocamlinit .sbclrc .luacheckrc .stylua.toml .rustfmt.toml .clippy.toml .sqliterc .psqlrc .mongorc.js .taskrc .condarc .gemrc .cargo/config .cargo/config.toml .julia/config/startup.jl`)
	result := make([]HomeConfigSpec, len(paths))
	for i, path := range paths {
		result[i] = HomeConfigSpec{Path: path}
	}
	return result
}

func LogicalHomeConfigPath(home, absolute string) (string, error) {
	rel, err := filepath.Rel(home, absolute)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("config path outside home: %s", absolute)
	}
	return profile.NormalizeConfigPath(rel)
}
func ExpandHomeConfigPath(home, logical string) (string, error) {
	logical, err := profile.NormalizeConfigPath(logical)
	if err != nil {
		return "", err
	}
	return filepath.Join(home, filepath.FromSlash(logical)), nil
}

// ResolveBaselineFor returns only explicit Omarchy package templates.
func (p Provider) ResolveBaselineFor(path string) (string, bool, error) {
	path, err := profile.NormalizeConfigPath(path)
	if err != nil {
		return "", false, err
	}
	candidates := map[string][]string{".bashrc": {"/usr/share/omarchy-zsh/templates/bashrc", "/usr/share/omarchy-fish/templates/bashrc"}, ".zshrc": {"/usr/share/omarchy-zsh/templates/zshrc"}, ".inputrc": {"/usr/share/omarchy-zsh/shell/inputrc"}}[path]
	var found []string
	for _, candidate := range candidates {
		if info, err := os.Lstat(candidate); err == nil && info.Mode().IsRegular() {
			found = append(found, candidate)
		} else if err != nil && !os.IsNotExist(err) {
			return "", false, err
		}
	}
	if len(found) > 1 {
		return "", false, fmt.Errorf("ambiguous baseline for %s", path)
	}
	if len(found) == 1 {
		return found[0], true, nil
	}
	return "", false, nil
}
