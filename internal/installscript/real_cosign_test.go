package installscript

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// TestRealCosignOfflinePositivePath exercises genuine Cosign bundle creation
// and verification without network access. It intentionally uses a local key:
// a genuine keyless positive fixture additionally requires Fulcio identity
// issuance, Rekor inclusion, and an SCT, which cannot be minted hermetically.
func TestRealCosignOfflinePositivePath(t *testing.T) {
	cosign, err := exec.LookPath("cosign")
	if err != nil {
		if os.Getenv("OKT_REQUIRE_REAL_COSIGN") == "1" {
			t.Fatal("OKT_REQUIRE_REAL_COSIGN=1 but cosign is not installed")
		}
		t.Skip("cosign not installed; skipping genuine offline positive-path test")
	}
	dir := t.TempDir()
	signingConfig := filepath.Join(repoRoot(t), "scripts", "testdata", "offline-signing-config.json")
	prefix := filepath.Join(dir, "fixture")
	blob := filepath.Join(dir, "artifact")
	bundle := filepath.Join(dir, "artifact.sigstore.json")
	statementPath := filepath.Join(dir, "provenance.intoto.json")
	attestationBundle := filepath.Join(dir, "provenance.sigstore.json")
	if err := os.WriteFile(blob, []byte("authenticated release fixture\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	env := append(os.Environ(), "COSIGN_PASSWORD=hermetic-test-password",
		"HTTP_PROXY=http://127.0.0.1:1", "HTTPS_PROXY=http://127.0.0.1:1", "NO_PROXY=")
	runCosign := func(args ...string) {
		t.Helper()
		cmd := exec.Command(cosign, args...)
		cmd.Env = env
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("cosign %v: %v\n%s", args, err, out)
		}
	}
	runCosign("generate-key-pair", "--output-key-prefix", prefix)
	runCosign("sign-blob", "--key", prefix+".key", "--bundle", bundle, "--signing-config", signingConfig, "--yes", blob)
	runCosign("verify-blob", "--key", prefix+".pub", "--bundle", bundle, "--insecure-ignore-tlog", blob)

	digest := sha256.Sum256([]byte("authenticated release fixture\n"))
	statement := map[string]any{
		"_type":         "https://in-toto.io/Statement/v1",
		"subject":       []any{map[string]any{"name": "artifact", "digest": map[string]string{"sha256": fmt.Sprintf("%x", digest)}}},
		"predicateType": "https://slsa.dev/provenance/v1",
		"predicate":     map[string]any{},
	}
	statementJSON, err := json.Marshal(statement)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(statementPath, statementJSON, 0o600); err != nil {
		t.Fatal(err)
	}
	runCosign("attest-blob", "--key", prefix+".key", "--statement", statementPath, "--bundle", attestationBundle, "--signing-config", signingConfig, "--yes")
	runCosign("verify-blob-attestation", "--key", prefix+".pub", "--bundle", attestationBundle, "--insecure-ignore-tlog", "--type", "slsaprovenance1", blob)
	assertForgedAttestationFails(t, cosign, env, attestationBundle, prefix+".pub", blob, dir)
}

func assertForgedAttestationFails(t *testing.T, cosign string, env []string, attestationBundle, publicKey, blob, dir string) {
	data, err := os.ReadFile(attestationBundle)
	if err != nil {
		t.Fatal(err)
	}
	var forged map[string]any
	if err := json.Unmarshal(data, &forged); err != nil {
		t.Fatal(err)
	}
	envelope := forged["dsseEnvelope"].(map[string]any)
	payload, err := base64.StdEncoding.DecodeString(envelope["payload"].(string))
	if err != nil {
		t.Fatal(err)
	}
	envelope["payload"] = base64.StdEncoding.EncodeToString(append(payload, ' '))
	forgedJSON, err := json.Marshal(forged)
	if err != nil {
		t.Fatal(err)
	}
	forgedPath := filepath.Join(dir, "forged.sigstore.json")
	if err := os.WriteFile(forgedPath, forgedJSON, 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(cosign, "verify-blob-attestation", "--key", publicKey, "--bundle", forgedPath, "--insecure-ignore-tlog", "--type", "slsaprovenance1", blob)
	cmd.Env = env
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("forged DSSE bundle verified successfully:\n%s", out)
	}
	if !bytes.Contains(bytes.ToLower(out), []byte("accepted signatures do not match")) {
		t.Fatalf("forged bundle failure did not identify signature verification: %v\n%s", err, out)
	}
}
