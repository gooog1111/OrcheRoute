// Command orcheroute-build is the single local entry point for checking the
// supported OrcheRoute targets: Android and Linux Server.
package main

import (
	"archive/zip"
	"bytes"
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
	target := flag.String("target", "all", "all, linux-server, android, web, or common")
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
	case "linux-server":
		if runtime.GOOS == "windows" {
			if err := b.web(); err != nil {
				return err
			}
			return b.linuxViaWSL()
		}
		return b.linuxServer()
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
		if err := b.android(); err != nil {
			return err
		}
		return b.linuxViaWSL()
	}
	if runtime.GOOS == "linux" {
		if err := b.linuxServer(); err != nil {
			return err
		}
		return b.android()
	}
	return fmt.Errorf("target=all supports Windows+WSL or Linux, got %s", runtime.GOOS)
}

func (b *builder) common() error {
	return b.once("common-"+runtime.GOOS, func() error {
		patterns := []string{"./cmd/...", "./internal/...", "./mobilecore/..."}
		if runtime.GOOS != "windows" {
			return b.command(b.root, nil, "go", append([]string{"test"}, patterns...)...)
		}
		listed, err := outputIn(b.root, "go", append([]string{"list"}, patterns...)...)
		if err != nil {
			return err
		}
		packages := make([]string, 0, len(strings.Fields(listed)))
		for _, packagePath := range strings.Fields(listed) {
			if strings.HasSuffix(packagePath, "/internal/serverruntime") {
				continue
			}
			packages = append(packages, packagePath)
		}
		return b.command(b.root, nil, "go", append([]string{"test"}, packages...)...)
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

func (b *builder) linuxServer() error {
	if runtime.GOOS != "linux" {
		return fmt.Errorf("Linux target must run on Linux, got %s", runtime.GOOS)
	}
	if err := b.common(); err != nil {
		return err
	}
	return b.buildServer()
}

func (b *builder) buildServer() error {
	if err := b.web(); err != nil {
		return err
	}
	return b.once("linux-server", func() error {
		out := filepath.Join(b.dist, "linux-server")
		if err := os.MkdirAll(out, 0o755); err != nil {
			return err
		}
		commands := []string{"orcheroute-server", "orcheroute-components-go", "orcheroute-network-go", "orcheroute-update-go", "orcheroute-self-update"}
		for _, name := range commands {
			if err := b.command(b.root, nil, "go", "build", "-trimpath", "-o", filepath.Join(out, name), "./cmd/"+name); err != nil {
				return err
			}
		}
		if err := replaceDirectory(filepath.Join(b.root, "webui", "out"), filepath.Join(out, "webui")); err != nil {
			return fmt.Errorf("include shared Server WebUI: %w", err)
		}
		return nil
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
		gomobile, err := goToolExecutable("gomobile")
		if err != nil {
			return err
		}
		if err := b.command(b.root, androidEnv, gomobile, "bind", "-tags=with_gvisor,cmfa", "-target=android/arm64", "-androidapi", "26", "-o", aar, "./mobilecore"); err != nil {
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
		return verifyArchiveFileMatches(
			filepath.Join(androidDir, "app", "build", "outputs", "apk", "debug", "app-debug.apk"),
			"assets/web/index.html",
			filepath.Join(webOut, "index.html"),
		)
	})
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
	command := "export PATH=\"$HOME/go/bin:$PATH\"; cd " + quotedRoot + " && go run ./cmd/orcheroute-build -target linux-server -skip-web"
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
	return outputIn("", name, args...)
}

func outputIn(dir, name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	data, err := cmd.Output()
	if err != nil {
		detail := ""
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			detail = strings.TrimSpace(string(exitErr.Stderr))
		}
		if detail != "" {
			return "", fmt.Errorf("%s: %w: %s", name, err, detail)
		}
		return "", fmt.Errorf("%s: %w", name, err)
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
	pathValue := os.Getenv("PATH")
	if goPath, err := output("go", "env", "GOPATH"); err == nil {
		for _, root := range filepath.SplitList(strings.TrimSpace(goPath)) {
			if root = strings.TrimSpace(root); root != "" {
				pathValue = filepath.Join(root, "bin") + string(os.PathListSeparator) + pathValue
			}
		}
	}
	return []string{
		"ANDROID_HOME=" + sdk,
		"ANDROID_SDK_ROOT=" + sdk,
		"PATH=" + pathValue,
	}, nil
}

func goToolExecutable(name string) (string, error) {
	if resolved, err := exec.LookPath(name); err == nil {
		return resolved, nil
	}
	goPath, err := output("go", "env", "GOPATH")
	if err != nil {
		return "", fmt.Errorf("locate %s: %w", name, err)
	}
	if resolved, ok := goToolInGOPATH(name, strings.TrimSpace(goPath)); ok {
		return resolved, nil
	}
	return "", fmt.Errorf("%s is unavailable; install the module-pinned golang.org/x/mobile tool", name)
}

func goToolInGOPATH(name, goPath string) (string, bool) {
	executable := name
	if runtime.GOOS == "windows" {
		executable += ".exe"
	}
	for _, root := range filepath.SplitList(goPath) {
		if root = strings.TrimSpace(root); root == "" {
			continue
		}
		candidate := filepath.Join(root, "bin", executable)
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			return candidate, true
		}
	}
	return "", false
}

func replaceDirectory(source, destination string) error {
	source = filepath.Clean(source)
	destination = filepath.Clean(destination)
	if source == destination || !safeGeneratedFrontendDestination(destination) {
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

func safeGeneratedFrontendDestination(destination string) bool {
	path := filepath.ToSlash(filepath.Clean(destination))
	return strings.Contains(path, "/dist/verify/") && strings.HasSuffix(path, "/linux-server/webui")
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

func verifyArchiveFileMatches(path, required, source string) error {
	expected, err := os.ReadFile(source)
	if err != nil {
		return fmt.Errorf("read canonical WebUI asset %s: %w", source, err)
	}
	archive, err := zip.OpenReader(path)
	if err != nil {
		return fmt.Errorf("open Android artifact %s: %w", path, err)
	}
	defer archive.Close()
	for _, entry := range archive.File {
		if entry.Name != required {
			continue
		}
		reader, err := entry.Open()
		if err != nil {
			return fmt.Errorf("open Android WebUI asset %s: %w", required, err)
		}
		actual, readErr := io.ReadAll(reader)
		closeErr := reader.Close()
		if readErr != nil {
			return fmt.Errorf("read Android WebUI asset %s: %w", required, readErr)
		}
		if closeErr != nil {
			return closeErr
		}
		if !bytes.Equal(actual, expected) {
			return fmt.Errorf("Android WebUI asset %s differs from canonical WebUI", required)
		}
		return nil
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
