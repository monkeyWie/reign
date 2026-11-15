package system

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func windowsAppFixture(t *testing.T) (string, string) {
	t.Helper()
	root := filepath.Join(t.TempDir(), "Codex 安装目录")
	exe := filepath.Join(root, "app", "ChatGPT.exe")
	if err := os.MkdirAll(filepath.Dir(exe), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(exe, nil, 0600); err != nil {
		t.Fatal(err)
	}
	manifest := `<Package xmlns="http://schemas.microsoft.com/appx/manifest/foundation/windows10"><Identity Name="OpenAI.Codex"/><Applications><Application Id="Helper" Executable="helper.exe"/><Application Id="App" Executable="app/ChatGPT.exe"/></Applications></Package>`
	if err := os.WriteFile(filepath.Join(root, "AppxManifest.xml"), []byte(manifest), 0600); err != nil {
		t.Fatal(err)
	}
	return root, exe
}

func TestWindowsManifestAndExplicitOverrides(t *testing.T) {
	root, exe := windowsAppFixture(t)
	for _, configured := range []string{root, filepath.Dir(exe), exe} {
		app, err := FindAppContext(context.Background(), configured)
		if err != nil || app != configured || AppExecutable(app) != exe {
			t.Fatalf("configured %q: app=%q executable=%q err=%v", configured, app, AppExecutable(app), err)
		}
	}
	if AppExecutable("") != "" {
		t.Fatal("empty app must not search the working directory")
	}
	for _, executable := range []string{`../outside.exe`, `C:\outside.exe`, `app/ChatGPT.txt`} {
		manifest := fmt.Sprintf(`<Package><Identity Name="OpenAI.Codex"/><Applications><Application Id="App" Executable="%s"/></Applications></Package>`, executable)
		if got := manifestAppExecutable(root, []byte(manifest)); got != "" {
			t.Fatalf("invalid executable accepted: %q", got)
		}
	}
	if err := os.Remove(exe); err != nil {
		t.Fatal(err)
	}
	if AppExecutable(root) != "" {
		t.Fatal("uninstalled executable still detected")
	}
}

func TestWindowsBundledCLIUsesManifestExecutableDirectory(t *testing.T) {
	root, exe := windowsAppFixture(t)
	cli := filepath.Join(filepath.Dir(exe), "resources", "codex.exe")
	if err := os.MkdirAll(filepath.Dir(cli), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cli, nil, 0600); err != nil {
		t.Fatal(err)
	}
	if got := FindCLI("", root); got != cli {
		t.Fatalf("bundled CLI: %q want %q", got, cli)
	}
	override := filepath.Join(root, "override.exe")
	if err := os.WriteFile(override, nil, 0600); err != nil {
		t.Fatal(err)
	}
	if got := FindCLI(override, root); got != override {
		t.Fatalf("explicit CLI overridden: %q", got)
	}
}

func TestInstalledCodexReadOnly(t *testing.T) {
	if os.Getenv("REIGNHUB_TEST_INSTALLED_CODEX") != "1" {
		t.Skip("opt-in read-only installed App probe")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	app, err := FindAppContext(ctx, "")
	if err != nil || app == "" {
		t.Fatalf("installed App: %q %v", app, err)
	}
	instances, err := Discover(ctx, app)
	if err != nil {
		t.Fatal(err)
	}
	if len(instances) != 1 {
		t.Fatalf("expected one running App, got %+v", instances)
	}
	cli := FindCLI("", app)
	if cli == "" {
		t.Fatal("bundled CLI not found")
	}
	t.Logf("App: %s; executable: %s; CLI: %s; instances: %+v", app, AppExecutable(app), cli, instances)
}
