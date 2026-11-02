package installscript

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// mirrorEnv is the explicit mirrored-checksum opt-in runInstall uses.
func mirrorEnv(url string, extra ...string) []string {
	return append([]string{"OKT_ALLOW_MIRROR_CHECKSUM=1", "OKT_CHECKSUM_BASE=" + url}, extra...)
}

func TestInstallSh_InstallServerInstallsBothBinaries(t *testing.T) {
	requireBash(t)
	archive := buildTarArchive(t, fakeTag)
	srv := installServer(t, scriptAsset(t), archive, hexDigest(archive))
	defer srv.Close()

	out, installDir, err := runInstallEnv(t, srv, mirrorEnv(srv.URL, "OKT_INSTALL_SERVER=1"), "")
	if err != nil {
		t.Fatalf("install with OKT_INSTALL_SERVER=1 failed: %v\noutput:\n%s", err, out)
	}
	for _, name := range []string{"okt", "okt-server"} {
		if _, err := os.Stat(filepath.Join(installDir, name)); err != nil {
			t.Fatalf("%s was not installed: %v\noutput:\n%s", name, err, out)
		}
	}
	if !strings.Contains(out, "Installed okt-server "+fakeTag) {
		t.Errorf("output does not report the installed okt-server:\n%s", out)
	}
}

func TestInstallSh_InstallsOnlyOktByDefault(t *testing.T) {
	requireBash(t)
	archive := buildTarArchive(t, fakeTag)
	srv := installServer(t, scriptAsset(t), archive, hexDigest(archive))
	defer srv.Close()

	out, installDir, err := runInstall(t, srv)
	if err != nil {
		t.Fatalf("default install failed: %v\noutput:\n%s", err, out)
	}
	if _, err := os.Stat(filepath.Join(installDir, "okt-server")); !os.IsNotExist(err) {
		t.Fatalf("default install placed okt-server (stat err = %v)", err)
	}
}

func TestInstallSh_InstallServerRefusesArchiveWithoutServer(t *testing.T) {
	requireBash(t)
	archive := buildTarArchiveOf(t, fakeTag, false)
	srv := installServer(t, scriptAsset(t), archive, hexDigest(archive))
	defer srv.Close()

	out, installDir, err := runInstallEnv(t, srv, mirrorEnv(srv.URL, "OKT_INSTALL_SERVER=1"), "")
	if err == nil {
		t.Fatalf("install from an archive without okt-server succeeded:\n%s", out)
	}
	assertNoBinary(t, installDir, out)
	assertReason(t, out, "does not ship okt-server")
}

func TestInstallSh_InstallServerRejectsUnknownValue(t *testing.T) {
	requireBash(t)
	archive := buildTarArchive(t, fakeTag)
	srv := installServer(t, scriptAsset(t), archive, hexDigest(archive))
	defer srv.Close()

	out, installDir, err := runInstallEnv(t, srv, mirrorEnv(srv.URL, "OKT_INSTALL_SERVER=yes"), "")
	if err == nil {
		t.Fatalf("OKT_INSTALL_SERVER=yes was accepted:\n%s", out)
	}
	assertNoBinary(t, installDir, out)
	assertReason(t, out, "OKT_INSTALL_SERVER must be '0' or '1'")
}

func TestInstallPs1_InstallServerInstallsBothBinaries(t *testing.T) {
	requireBash(t) // the cosign double is a POSIX sh script
	srv := strictServer(t, windowsFixture(t, fakeTag))
	cosignDir, cosignEnv, logPath := writeFakeCosign(t, fakeCosignOptions{})

	run := runStrictPs1(t, srv, cosignDir, cosignEnv, logPath, "OKT_INSTALL_SERVER=1")
	if run.Err != nil {
		t.Fatalf("install with OKT_INSTALL_SERVER=1 failed: %v\noutput:\n%s", run.Err, run.Output)
	}
	for _, name := range []string{"okt.exe", "okt-server.exe"} {
		if _, err := os.Stat(filepath.Join(run.InstallDir, name)); err != nil {
			t.Fatalf("%s was not installed: %v\noutput:\n%s", name, err, run.Output)
		}
	}
	if !strings.Contains(run.Output, "Installed okt-server "+fakeTag) {
		t.Errorf("output does not report the installed okt-server:\n%s", run.Output)
	}
}

func TestInstallPs1_InstallServerRefusesArchiveWithoutServer(t *testing.T) {
	requireBash(t)
	fixture := buildStrictFixture(t, fakeTag, "okt_Windows_x86_64.zip", buildZipArchiveOf(t, fakeTag, false))
	srv := strictServer(t, fixture)
	cosignDir, cosignEnv, logPath := writeFakeCosign(t, fakeCosignOptions{})

	run := runStrictPs1(t, srv, cosignDir, cosignEnv, logPath, "OKT_INSTALL_SERVER=1")
	if run.Err == nil {
		t.Fatalf("install from an archive without okt-server succeeded:\n%s", run.Output)
	}
	assertNoBinary(t, run.InstallDir, run.Output)
	assertReason(t, run.Output, "does not ship okt-server")
}
