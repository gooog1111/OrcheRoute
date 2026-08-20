// Command orcheroute-build is the single local and CI entry point for checking
// every supported OrcheRoute platform. Platform jobs intentionally call this
// same program so the developer and CI build graphs cannot drift apart.
package main

import (
	"archive/zip"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

type builder struct {
	root      string
	target    string
	skipWeb   bool
	wslDistro string
	completed map[string]bool
	dist      string
}

func main() {
	target := flag.String("target", "all", "all, windows, linux, their server/desktop variants, android, web, or common")
	skipWeb := flag.Bool("skip-web", false, "reuse an already verified webui/out (used by local WSL dispatch)")
	wslDistro := flag.String("wsl-distro", "Ubuntu-24.04", "WSL distribution used by target=all on Windows")
	flag.Parse()

	root, err := repositoryRoot()
	if err != nil {
		fatal(err)
	}
	b := &builder{
		root: root, target: strings.ToLower(strings.TrimSpace(*target)),
		skipWeb: *skipWeb, wslDistro: *wslDistro, completed: map[string]bool{},
		dist: filepath.Join(root, "dist", "verify"),
	}
	started := time.Now()
	fmt.Printf("OrcheRoute unified build: target=%s host=%s/%s\n", b.target, runtime.GOOS, runtime.GOARCH)
	if err := b.run(); err != nil {
		fatal(err)
	}
	fmt.Printf("\nPASS target=%s elapsed=%s\n", b.target, time.Since(started).Round(time.Second))
}

func (b *builder) run() error {
	switch b.target {
	case "common":
		return b.common()
	case "web":
		return b.web()
	case "windows":
		return b.windows()
	case "windows-server":
		return b.windowsServer()
	case "windows-desktop":
		return b.windowsDesktop()
	case "linux":
		return b.linux()
	case "linux-server":
		return b.linuxServer()
	case "linux-desktop":
		return b.linuxDesktop()
	case "android":
		return b.android()
	case "all":
		return b.all()
	default:
		return fmt.Errorf("unknown target %q", b.target)
	}
}

func (b *builder) all() error {
	if runtime.GOOS == "windows" {
		if err := b.windows(); err != nil {
			return err
		}
		if err := b.android(); err != nil {
			return err
		}
		return b.linuxViaWSL()
	}
	if runtime.GOOS == "linux" {
		if err := b.linux(); err != nil {
			return err
		}
		return b.android()
	}
	return fmt.Errorf("target=all supports Windows+WSL or Linux; run platform targets in CI on %s", runtime.GOOS)
}

func (b *builder) common() error {
	return b.once("common-"+runtime.GOOS, func() error {
		return b.command(b.root, nil, "go", "test", "./cmd/...", "./internal/...", "./mobilecore/...")
	})
}

func (b *builder) web() error {
	if b.skipWeb {
		index := filepath.Join(b.root, "webui", "out", "index.html")
		if _, err := os.Stat(index); err != nil {
			return fmt.Errorf("-skip-web requires %s: %w", index, err)
		}
		return nil
	}
	return b.once("web", func() error {
		dir := filepath.Join(b.root, "webui")
		if err := b.command(dir, nil, npmCommand(), "ci"); err != nil {
			return err
		}
		if err := b.command(dir, nil, npmCommand(), "run", "lint"); err != nil {
			return err
		}
		return b.command(dir, nil, npmCommand(), "test")
	})
}

func (b *builder) windows() error {
	if err := b.windowsServer(); err != nil {
		return err
	}
	return b.windowsDesktop()
}

func (b *builder) windowsServer() error {
	if runtime.GOOS != "windows" {
		return fmt.Errorf("Windows target must run on Windows, got %s", runtime.GOOS)
	}
	if err := b.common(); err != nil {
		return err
	}
	return b.buildServer("windows")
}

func (b *builder) windowsDesktop() error {
	if runtime.GOOS != "windows" {
		return fmt.Errorf("Windows target must run on Windows, got %s", runtime.GOOS)
	}
	if err := b.common(); err != nil {
		return err
	}
	if err := b.web(); err != nil {
		return err
	}
	return b.buildDesktop("windows")
}

func (b *builder) linux() error {
	if err := b.linuxServer(); err != nil {
		return err
	}
	return b.linuxDesktop()
}

func (b *builder) linuxServer() error {
	if runtime.GOOS != "linux" {
		return fmt.Errorf("Linux target must run on Linux, got %s", runtime.GOOS)
	}
	if err := b.common(); err != nil {
		return err
	}
	return b.buildServer("linux")
}

func (b *builder) linuxDesktop() error {
	if runtime.GOOS != "linux" {
		return fmt.Errorf("Linux target must run on Linux, got %s", runtime.GOOS)
	}
	if err := b.common(); err != nil {
		return err
	}
	if err := b.web(); err != nil {
		return err
	}
	return b.buildDesktop("linux")
}

func (b *builder) buildServer(goos string) error {
	return b.once("server-"+goos, func() error {
		out := filepath.Join(b.dist, goos+"-server")
		if err := os.MkdirAll(out, 0o755); err != nil {
			return err
		}
		commands := []string{"orcheroute-server", "orcheroute-components-go", "orcheroute-network-go", "orcheroute-update-go"}
		for _, name := range commands {
			ext := ""
			if goos == "windows" {
				ext = ".exe"
			}
			if err := b.command(b.root, nil, "go", "build", "-trimpath", "-o", filepath.Join(out, name+ext), "./cmd/"+name); err != nil {
				return err
			}
		}
		return nil
	})
}

func (b *builder) buildDesktop(goos string) error {
	return b.once("desktop-"+goos, func() error {
		if err := b.syncDesktopWeb(); err != nil {
			return err
		}
		dir := filepath.Join(b.root, "desktop")
		if err := b.command(dir, nil, "go", "test", "./..."); err != nil {
			return err
		}
		output := "OrcheRoute-" + goos + "-amd64"
		if goos == "windows" {
			output += ".exe"
		}
		args := []string{"build", "-s", "-skipbindings", "-nopackage", "-trimpath", "-o", output}
		if goos == "linux" && !pkgConfigExists("webkit2gtk-4.0") && pkgConfigExists("webkit2gtk-4.1") {
			args = append(args, "-tags", "webkit2_41")
		}
		return b.command(dir, nil, "wails", args...)
	})
}

func (b *builder) android() error {
	if runtime.GOOS != "windows" && runtime.GOOS != "linux" {
		return fmt.Errorf("Android target must run on Windows or Linux, got %s", runtime.GOOS)
	}
	if err := b.common(); err != nil {
		return err
	}
	if err := b.web(); err != nil {
		return err
	}
	return b.once("android", func() error {
		androidEnv, err := androidEnvironment()
		if err != nil {
			return err
		}
		out := filepath.Join(b.dist, "android")
		if err := os.MkdirAll(out, 0o755); err != nil {
			return err
		}
		aar := filepath.Join(out, "mobilecore.aar")
		if err := b.command(b.root, androidEnv, "gomobile", "bind", "-tags=with_gvisor,cmfa", "-target=android/arm64", "-androidapi", "26", "-o", aar, "./mobilecore"); err != nil {
			return err
		}
		gradle := "./gradlew"
		if runtime.GOOS == "windows" {
			gradle = filepath.Join(b.root, "android", "gradlew.bat")
		}
		androidDir := filepath.Join(b.root, "android")
		webOut := filepath.Join(b.root, "webui", "out")
		if err := b.command(androidDir, androidEnv, gradle, "--no-daemon", "--no-configuration-cache", "clean", "assembleDebug",
			"-PorcherouteMobileCoreAar="+aar, "-PorcherouteWebAssets="+webOut); err != nil {
			return err
		}
		return verifyArchiveEntry(
			filepath.Join(androidDir, "app", "build", "outputs", "apk", "debug", "app-debug.apk"),
			"assets/web/index.html",
		)
	})
}

func (b *builder) syncDesktopWeb() error {
	if err := b.web(); err != nil {
		return err
	}
	source := filepath.Join(b.root, "webui", "out")
	destination := filepath.Join(b.root, "desktop", "frontend", "dist")
	if err := replaceDirectory(source, destination); err != nil {
		return fmt.Errorf("sync Desktop WebUI: %w", err)
	}
	fmt.Printf("Desktop frontend synced: %s\n", destination)
	return nil
}

func (b *builder) linuxViaWSL() error {
	if runtime.GOOS != "windows" {
		return errors.New("WSL dispatch is only available on Windows")
	}
	linuxRoot, err := output("wsl.exe", "-d", b.wslDistro, "--exec", "wslpath", "-a", b.root)
	if err != nil {
		return fmt.Errorf("WSL %s is unavailable: %w", b.wslDistro, err)
	}
	quotedRoot := shellQuote(strings.TrimSpace(linuxRoot))
	command := "export PATH=\"$HOME/go/bin:$PATH\"; cd " + quotedRoot + " && go run ./cmd/orcheroute-build -target linux -skip-web"
	return b.command(b.root, nil, "wsl.exe", "-d", b.wslDistro, "--exec", "bash", "-lc", command)
}

func (b *builder) once(name string, fn func() error) error {
	if b.completed[name] {
		return nil
	}
	fmt.Printf("\n=== %s ===\n", name)
	if err := fn(); err != nil {
		return fmt.Errorf("%s: %w", name, err)
	}
	b.completed[name] = true
	return nil
}

func (b *builder) command(dir string, env []string, name string, args ...string) error {
	fmt.Printf("+ (%s) %s %s\n", relative(b.root, dir), name, strings.Join(args, " "))
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), env...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s failed: %w", name, err)
	}
	return nil
}

func repositoryRoot() (string, error) {
	root, err := output("git", "rev-parse", "--show-toplevel")
	if err != nil {
		return "", fmt.Errorf("run inside the OrcheRoute Git repository: %w", err)
	}
	return filepath.Clean(strings.TrimSpace(root)), nil
}

func output(name string, args ...string) (string, error) {
	data, err := exec.Command(name, args...).CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("%s: %w: %s", name, err, strings.TrimSpace(string(data)))
	}
	return string(data), nil
}

func npmCommand() string {
	if runtime.GOOS == "windows" {
		return "npm.cmd"
	}
	return "npm"
}

func androidEnvironment() ([]string, error) {
	sdk := strings.TrimSpace(os.Getenv("ANDROID_HOME"))
	if sdk == "" {
		sdk = strings.TrimSpace(os.Getenv("ANDROID_SDK_ROOT"))
	}
	if sdk == "" && runtime.GOOS == "windows" {
		sdk = filepath.Join(os.Getenv("LOCALAPPDATA"), "Android", "Sdk")
	}
	if sdk == "" {
		return nil, errors.New("Android SDK is not configured; set ANDROID_HOME or ANDROID_SDK_ROOT")
	}
	if info, err := os.Stat(sdk); err != nil || !info.IsDir() {
		return nil, fmt.Errorf("Android SDK directory is unavailable: %s", sdk)
	}
	return []string{"ANDROID_HOME=" + sdk, "ANDROID_SDK_ROOT=" + sdk}, nil
}

func pkgConfigExists(name string) bool {
	cmd := exec.Command("pkg-config", "--exists", name)
	return cmd.Run() == nil
}

func replaceDirectory(source, destination string) error {
	source = filepath.Clean(source)
	destination = filepath.Clean(destination)
	if source == destination || !strings.Contains(filepath.ToSlash(destination), "/desktop/frontend/dist") {
		return fmt.Errorf("unsafe generated frontend destination: %s", destination)
	}
	if info, err := os.Stat(source); err != nil || !info.IsDir() {
		return fmt.Errorf("generated frontend is unavailable: %s", source)
	}
	if err := os.RemoveAll(destination); err != nil {
		return err
	}
	if err := os.MkdirAll(destination, 0o755); err != nil {
		return err
	}
	return filepath.WalkDir(source, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		relativePath, err := filepath.Rel(source, path)
		if err != nil || relativePath == "." {
			return err
		}
		target := filepath.Join(destination, relativePath)
		if entry.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("generated frontend contains unsupported symlink: %s", path)
		}
		return copyFile(path, target)
	})
}

func copyFile(source, destination string) error {
	input, err := os.Open(source)
	if err != nil {
		return err
	}
	defer input.Close()
	info, err := input.Stat()
	if err != nil {
		return err
	}
	output, err := os.OpenFile(destination, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, info.Mode().Perm())
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(output, input)
	closeErr := output.Close()
	if copyErr != nil {
		return copyErr
	}
	return closeErr
}

func verifyArchiveEntry(path, required string) error {
	archive, err := zip.OpenReader(path)
	if err != nil {
		return fmt.Errorf("open Android artifact %s: %w", path, err)
	}
	defer archive.Close()
	for _, entry := range archive.File {
		if entry.Name == required {
			return nil
		}
	}
	return fmt.Errorf("Android artifact %s does not contain required %s", path, required)
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}

func relative(root, value string) string {
	result, err := filepath.Rel(root, value)
	if err != nil || result == "." {
		return "."
	}
	return result
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "FAIL:", err)
	os.Exit(1)
}
