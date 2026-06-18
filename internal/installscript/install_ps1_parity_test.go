package installscript

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInstallPs1PreservesVerifyBeforeExtractParity(t *testing.T) {
	assertOrderedMarkers(t, "install.sh", readRootFile(t, "install.sh"), []string{
		`curl -fsSL "${url}" -o "${tmpdir}/${asset}"`,
		`verify_checksum "${tmpdir}/${asset}" "${asset}" "${tag}" "${tmpdir}"`,
		`tar -xzf "${tmpdir}/${asset}" -C "${tmpdir}"`,
		`install -m 755 "${tmpdir}/okt" "${INSTALL_DIR}/okt"`,
		"\n  ensure_path\n",
		`"${INSTALL_DIR}/okt" --version`,
		`"${INSTALL_DIR}/okt" setup`,
	})

	assertOrderedMarkers(t, "install.ps1", readRootFile(t, "install.ps1"), []string{
		`Invoke-WebRequest -Uri $url -OutFile "$tmpdir\$asset"`,
		`Verify-Checksum -Archive "$tmpdir\$asset" -Asset $asset -Tag $tag -TmpDir $tmpdir`,
		`Expand-Archive -Path "$tmpdir\$asset" -DestinationPath $tmpdir -Force`,
		`New-Item -ItemType Directory -Path $InstallDir -Force | Out-Null`,
		`Copy-Item -Path "$tmpdir\okt.exe" -Destination "$InstallDir\okt.exe" -Force`,
		`Add-ToPath -Dir $InstallDir`,
		`& "$InstallDir\okt.exe" --version`,
		`& "$InstallDir\okt.exe" setup`,
	})
}

func TestInstallPs1PreservesChecksumTrustRootParity(t *testing.T) {
	sh := readRootFile(t, "install.sh")
	ps1 := readRootFile(t, "install.ps1")

	assertContainsAll(t, "install.sh", sh, []string{
		`CHECKSUM_BASE="https://github.com"`,
		`OKT_ALLOW_MIRROR_CHECKSUM`,
		`OKT_CHECKSUM_BASE to name the checksum mirror`,
		`CHECKSUM_BASE="${OKT_CHECKSUM_BASE%/}"`,
		`sums_url="${CHECKSUM_BASE}/${REPO}/releases/download/v${tag}/checksums.txt"`,
		`expected="$(awk -v want="${asset}" '$2 == want {print $1; exit}' "${sums}")"`,
		`actual="$(sha256_of "${archive}")"`,
		`refusing to install a tampered or corrupt archive`,
	})

	assertContainsAll(t, "install.ps1", ps1, []string{
		`$ChecksumBase = "https://github.com"`,
		`OKT_ALLOW_MIRROR_CHECKSUM`,
		`OKT_CHECKSUM_BASE to name the checksum mirror`,
		`$ChecksumBase = $env:OKT_CHECKSUM_BASE.TrimEnd("/")`,
		`$sumsUrl = "$ChecksumBase/$Repo/releases/download/v$Tag/checksums.txt"`,
		`if ($fields.Count -eq 2 -and $fields[1].Trim() -eq $Asset)`,
		`$actual = (Get-FileHash -Algorithm SHA256 -Path $Archive).Hash`,
		`refusing to install a tampered or corrupt archive`,
	})
}

func readRootFile(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(repoRoot(t), name))
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	return strings.ReplaceAll(string(b), "\r\n", "\n")
}

func assertOrderedMarkers(t *testing.T, label, body string, markers []string) {
	t.Helper()
	last := -1
	for _, marker := range markers {
		idx := strings.Index(body, marker)
		if idx < 0 {
			t.Fatalf("%s missing marker:\n%s", label, marker)
		}
		if idx <= last {
			t.Fatalf("%s marker appears out of order:\n%s", label, marker)
		}
		last = idx
	}
}

func assertContainsAll(t *testing.T, label, body string, needles []string) {
	t.Helper()
	for _, needle := range needles {
		if !strings.Contains(body, needle) {
			t.Fatalf("%s missing checksum trust-root marker:\n%s", label, needle)
		}
	}
}
