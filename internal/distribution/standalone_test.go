package distribution_test

import (
	"context"
	"crypto/sha256"
	"encoding/json/v2"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/belevtsev/agent-workflows/internal/bundled"
	"github.com/belevtsev/agent-workflows/internal/distribution"
)

type distributionJSON map[string]any
type distributionReply struct {
	body   []byte
	status int
}
type distributionHTTP struct {
	client distribution.Client
	mutex  sync.Mutex
	hits   map[string]int
}
type distributionMetadata struct {
	release, tag distributionJSON
	sums         string
}

func distributionEncode(test *testing.T, value any) []byte {
	test.Helper()
	encoded, err := json.Marshal(value)
	if err != nil {
		test.Fatalf("encode synthetic response: %v", err)
	}
	return encoded
}
func distributionIdentity(version string) bundled.Identity {
	return bundled.Identity{Version: version, Revision: strings.Repeat("a", 40), OS: "linux", Arch: "amd64", Digest: strings.Repeat("b", 64)}
}
func distributionInvalidVersions() []string {
	return []string{"", "1.2.3", "V1.2.3", "v1.2", "v1.2.3.4", "v01.2.3", "v1.02.3", "v1.2.03", "v1.2.3-rc.1", "v1.2.3+build", "dev", "v1.2.3-dev", "v-1.2.3", "v1.2.+3"}
}
func distributionWorkspace(test *testing.T) {
	test.Helper()
	root, err := filepath.EvalSymlinks(test.TempDir())
	if err != nil {
		test.Fatal(err)
	}
	filename := filepath.Join(root, "keep.txt")
	if err := os.WriteFile(filename, []byte("unrelated sentinel\n"), 0600); err != nil {
		test.Fatal(err)
	}
	before, err := os.Lstat(filename)
	if err != nil {
		test.Fatal(err)
	}
	test.Chdir(root)
	test.Cleanup(func() {
		entries, readErr := os.ReadDir(root)
		after, statErr := os.Lstat(filename)
		if readErr != nil || statErr != nil {
			test.Errorf("workspace changed: %v, %v", readErr, statErr)
			return
		}
		if !after.Mode().IsRegular() {
			test.Error("workspace sentinel replaced with nonregular file")
			return
		}
		data, dataErr := os.ReadFile(filename)
		if dataErr != nil || len(entries) != 1 || string(data) != "unrelated sentinel\n" || after.Mode() != before.Mode() || !after.ModTime().Equal(before.ModTime()) {
			test.Error("HTTP operation wrote to the protected workspace")
		}
	})
}
func distributionServe(test *testing.T, replies map[string]distributionReply) *distributionHTTP {
	test.Helper()
	distributionWorkspace(test)
	fixture := &distributionHTTP{hits: make(map[string]int)}
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		fixture.mutex.Lock()
		fixture.hits[request.Method+" "+request.URL.RequestURI()]++
		fixture.mutex.Unlock()
		reply, exists := replies[request.URL.Path]
		if !exists {
			http.NotFound(writer, request)
			return
		}
		writer.Header().Set("Retry-After", "0")
		writer.WriteHeader(reply.status)
		_, _ = writer.Write(reply.body)
	}))
	test.Cleanup(server.Close)
	httpClient := server.Client()
	httpClient.Timeout = 30 * time.Second
	fixture.client = distribution.Client{HTTP: httpClient, APIBase: server.URL + "/api", DownloadBase: server.URL + "/download"}
	return fixture
}
func (fixture *distributionHTTP) check(test *testing.T, paths []string, required ...string) {
	test.Helper()
	fixture.mutex.Lock()
	defer fixture.mutex.Unlock()
	allowed := make(map[string]bool)
	for _, path := range paths {
		allowed["GET "+path] = true
	}
	for request, count := range fixture.hits {
		if !allowed[request] || count != 1 {
			test.Errorf("unexpected request or retry: %q count %d", request, count)
		}
	}
	for _, path := range required {
		if fixture.hits["GET "+path] != 1 {
			test.Errorf("GET %s count = %d, want 1", path, fixture.hits["GET "+path])
		}
	}
}
func distributionPaths(replies map[string]distributionReply) []string {
	paths := make([]string, 0, len(replies))
	for path := range replies {
		paths = append(paths, path)
	}
	return paths
}
func distributionArchiveNames() []string {
	return []string{"cw_darwin_arm64.tar.gz", "cw_darwin_amd64.tar.gz", "cw_linux_arm64.tar.gz", "cw_linux_amd64.tar.gz"}
}
func distributionReleaseMetadata() distributionMetadata {
	assets := make([]distributionJSON, 0, 5)
	var sums strings.Builder
	for index, name := range distributionArchiveNames() {
		assets = append(assets, distributionJSON{"id": index + 1, "name": name, "size": 128, "state": "uploaded"})
		fmt.Fprintf(&sums, "%064x  %s\n", index+10, name)
	}
	assets = append(assets, distributionJSON{"id": 5, "name": "SHA256SUMS", "size": sums.Len(), "state": "uploaded"})
	return distributionMetadata{
		release: distributionJSON{"id": 17, "tag_name": "v2.0.10", "target_commitish": strings.Repeat("a", 40), "draft": false, "prerelease": false, "assets": assets},
		tag:     distributionJSON{"ref": "refs/tags/v2.0.10", "object": distributionJSON{"type": "commit", "sha": strings.Repeat("a", 40)}}, sums: sums.String(),
	}
}
func (metadata distributionMetadata) replies(test *testing.T) map[string]distributionReply {
	version := metadata.release["tag_name"].(string)
	return map[string]distributionReply{
		"/api/releases/latest":                 {distributionEncode(test, metadata.release), 200},
		"/api/git/ref/tags/" + version:         {distributionEncode(test, metadata.tag), 200},
		"/download/" + version + "/SHA256SUMS": {[]byte(metadata.sums), 200},
	}
}
func TestR_D1_CheckOrderNumericAuthority(test *testing.T) {
	cases := []struct {
		name, target, water string
		accept              bool
	}{
		{"fresh_zero", "v0.0.0", "", true}, {"fresh", "v1.2.3", "", true}, {"equal", "v2.0.9", "v2.0.9", true},
		{"patch_numeric", "v2.0.10", "v2.0.9", true}, {"minor_numeric", "v2.10.0", "v2.9.999", true}, {"major_numeric", "v10.0.0", "v9.999.999", true},
		{"patch_rollback", "v2.0.9", "v2.0.10", false}, {"minor_rollback", "v2.9.999", "v2.10.0", false}, {"major_rollback", "v9.999.999", "v10.0.0", false},
		{"uint64_overflow", "v18446744073709551616.0.0", "v18446744073709551615.0.0", true},
		{"long_minor", "v1.1" + strings.Repeat("0", 80) + ".0", "v1." + strings.Repeat("9", 80) + ".9", true},
		{"long_patch_rollback", "v1.0." + strings.Repeat("9", 80), "v1.0.1" + strings.Repeat("0", 80), false},
	}
	for _, scenario := range cases {
		test.Run(scenario.name, func(test *testing.T) {
			target, water := distributionIdentity(scenario.target), bundled.Identity{}
			if scenario.water != "" {
				water = distributionIdentity(scenario.water)
			}
			if err := distribution.CheckOrder(target, water); (err == nil) != scenario.accept {
				test.Fatalf("CheckOrder(%q, %q) = %v, accept=%v", scenario.target, scenario.water, err, scenario.accept)
			}
		})
	}
	for _, field := range []string{"revision", "digest", "os", "arch"} {
		for _, version := range []string{"v2.0.9", "v2.0.10"} {
			test.Run(field+"_"+version, func(test *testing.T) {
				water, target := distributionIdentity("v2.0.9"), distributionIdentity(version)
				switch field {
				case "revision":
					target.Revision = strings.Repeat("c", 40)
				case "digest":
					target.Digest = strings.Repeat("d", 64)
				case "os":
					target.OS = "darwin"
				case "arch":
					target.Arch = "arm64"
				}
				accept := version == "v2.0.10" && (field == "revision" || field == "digest")
				if err := distribution.CheckOrder(target, water); (err == nil) != accept {
					test.Fatalf("identity change %s at %s: %v, accept=%v", field, version, err, accept)
				}
			})
		}
	}
}
func TestR_D1_CheckOrderRejectsInvalidAndPartialIdentities(test *testing.T) {
	if err := distribution.CheckOrder(bundled.Identity{}, bundled.Identity{}); err == nil {
		test.Error("accepted zero target identity")
	}
	invalid := make([]bundled.Identity, 0)
	for _, version := range distributionInvalidVersions() {
		invalid = append(invalid, distributionIdentity(version))
	}
	for _, field := range []string{"revision", "digest", "os", "arch"} {
		for _, value := range []string{"", "invalid", "UPPERCASE"} {
			identity := distributionIdentity("v1.2.3")
			switch field {
			case "revision":
				identity.Revision = value
			case "digest":
				identity.Digest = value
			case "os":
				identity.OS = value
			case "arch":
				identity.Arch = value
			}
			invalid = append(invalid, identity)
		}
	}
	uppercase := distributionIdentity("v1.2.3")
	uppercase.Revision = strings.Repeat("A", 40)
	invalid = append(invalid, uppercase)
	uppercase = distributionIdentity("v1.2.3")
	uppercase.Digest = strings.Repeat("B", 64)
	invalid = append(invalid, uppercase)
	for index, identity := range invalid {
		test.Run(fmt.Sprintf("invalid_%d", index), func(test *testing.T) {
			if err := distribution.CheckOrder(identity, bundled.Identity{}); err == nil {
				test.Error("accepted invalid target")
			}
			if err := distribution.CheckOrder(distributionIdentity("v2.0.10"), identity); err == nil {
				test.Error("accepted invalid nonzero high-water identity")
			}
		})
	}
	for mask := 1; mask < 31; mask++ {
		test.Run(fmt.Sprintf("partial_%02d", mask), func(test *testing.T) {
			full, partial := distributionIdentity("v1.2.3"), bundled.Identity{}
			if mask&1 != 0 {
				partial.Version = full.Version
			}
			if mask&2 != 0 {
				partial.Revision = full.Revision
			}
			if mask&4 != 0 {
				partial.OS = full.OS
			}
			if mask&8 != 0 {
				partial.Arch = full.Arch
			}
			if mask&16 != 0 {
				partial.Digest = full.Digest
			}
			if err := distribution.CheckOrder(distributionIdentity("v2.0.10"), partial); err == nil {
				test.Fatal("accepted partial high-water identity")
			}
		})
	}
}
func TestR_D2_LatestPinsCurrentPlatformAndManifest(test *testing.T) {
	for _, scenario := range []string{"commit", "branch", "reordered_checksums"} {
		test.Run(scenario, func(test *testing.T) {
			metadata := distributionReleaseMetadata()
			if scenario == "branch" {
				metadata.release["target_commitish"] = "release/stable"
			}
			if scenario == "reordered_checksums" {
				records := strings.Split(strings.TrimSuffix(metadata.sums, "\n"), "\n")
				for left, right := 0, len(records)-1; left < right; left, right = left+1, right-1 {
					records[left], records[right] = records[right], records[left]
				}
				metadata.sums = strings.Join(records, "\n") + "\n"
			}
			replies := metadata.replies(test)
			fixture := distributionServe(test, replies)
			release, err := fixture.client.Latest(test.Context())
			if err != nil {
				test.Fatalf("Latest: %v", err)
			}
			name, archiveDigest := "cw_"+runtime.GOOS+"_"+runtime.GOARCH+".tar.gz", ""
			for index, candidate := range distributionArchiveNames() {
				if candidate == name {
					archiveDigest = fmt.Sprintf("%064x", index+10)
				}
			}
			if archiveDigest == "" {
				test.Fatalf("unsupported contract test platform %s/%s", runtime.GOOS, runtime.GOARCH)
			}
			manifestDigest := fmt.Sprintf("%x", sha256.Sum256([]byte(metadata.sums)))
			if release.ID != 17 || release.Version != "v2.0.10" || release.Revision != strings.Repeat("a", 40) || release.OS != runtime.GOOS || release.Arch != runtime.GOARCH {
				test.Errorf("incorrect pinned release identity: %#v", release)
			}
			if release.ArchiveName != name || release.ArchiveURL != fixture.client.DownloadBase+"/v2.0.10/"+name || release.ArchiveSHA256 != archiveDigest || release.ChecksumsSHA256 != manifestDigest {
				test.Errorf("incorrect pinned archive or manifest: %#v", release)
			}
			paths := distributionPaths(replies)
			fixture.check(test, paths, paths...)
		})
	}
}
func TestR_D2_LatestRejectsInvalidMetadataAndChecksums(test *testing.T) {
	cases := []struct {
		name   string
		change func(*distributionMetadata)
	}{
		{"draft", func(metadata *distributionMetadata) { metadata.release["draft"] = true }},
		{"prerelease", func(metadata *distributionMetadata) { metadata.release["prerelease"] = true }},
		{"nonpositive_release_id", func(metadata *distributionMetadata) { metadata.release["id"] = 0 }},
		{"commit_mismatch", func(metadata *distributionMetadata) { metadata.release["target_commitish"] = strings.Repeat("b", 40) }},
		{"missing_archive", func(metadata *distributionMetadata) {
			metadata.release["assets"] = metadata.release["assets"].([]distributionJSON)[1:]
		}},
		{"missing_checksums_asset", func(metadata *distributionMetadata) {
			metadata.release["assets"] = metadata.release["assets"].([]distributionJSON)[:4]
		}},
		{"duplicate_asset", func(metadata *distributionMetadata) {
			assets := metadata.release["assets"].([]distributionJSON)
			metadata.release["assets"] = append(assets, assets[0])
		}},
		{"duplicate_replaces_missing", func(metadata *distributionMetadata) {
			assets := metadata.release["assets"].([]distributionJSON)
			assets[1] = assets[0]
		}},
		{"extra_asset", func(metadata *distributionMetadata) {
			metadata.release["assets"] = append(metadata.release["assets"].([]distributionJSON), distributionJSON{"id": 6, "name": "extra.txt", "size": 1, "state": "uploaded"})
		}},
		{"asset_id", func(metadata *distributionMetadata) { metadata.release["assets"].([]distributionJSON)[0]["id"] = 0 }},
		{"asset_size", func(metadata *distributionMetadata) { metadata.release["assets"].([]distributionJSON)[4]["size"] = 0 }},
		{"asset_state", func(metadata *distributionMetadata) {
			metadata.release["assets"].([]distributionJSON)[0]["state"] = "new"
		}},
		{"tag_ref", func(metadata *distributionMetadata) { metadata.tag["ref"] = "refs/tags/v2.0.9" }},
		{"annotated_tag", func(metadata *distributionMetadata) { metadata.tag["object"].(distributionJSON)["type"] = "tag" }},
		{"tag_sha_uppercase", func(metadata *distributionMetadata) {
			metadata.tag["object"].(distributionJSON)["sha"] = strings.Repeat("A", 40)
		}},
		{"tag_sha_short", func(metadata *distributionMetadata) {
			metadata.tag["object"].(distributionJSON)["sha"] = strings.Repeat("a", 39)
		}},
		{"checksum_uppercase", func(metadata *distributionMetadata) {
			metadata.sums = strings.ToUpper(metadata.sums[:64]) + metadata.sums[64:]
		}},
		{"checksum_short", func(metadata *distributionMetadata) { metadata.sums = metadata.sums[1:] }},
		{"checksum_nonhex", func(metadata *distributionMetadata) { metadata.sums = "g" + metadata.sums[1:] }},
		{"checksum_single_space", func(metadata *distributionMetadata) { metadata.sums = strings.Replace(metadata.sums, "  ", " ", 1) }},
		{"checksum_no_final_newline", func(metadata *distributionMetadata) { metadata.sums = strings.TrimSuffix(metadata.sums, "\n") }},
		{"checksum_crlf", func(metadata *distributionMetadata) { metadata.sums = strings.ReplaceAll(metadata.sums, "\n", "\r\n") }},
		{"checksum_missing", func(metadata *distributionMetadata) { metadata.sums = strings.SplitN(metadata.sums, "\n", 2)[1] }},
		{"checksum_duplicate", func(metadata *distributionMetadata) { metadata.sums += strings.Split(metadata.sums, "\n")[0] + "\n" }},
		{"checksum_duplicate_replaces_missing", func(metadata *distributionMetadata) {
			records := strings.Split(strings.TrimSuffix(metadata.sums, "\n"), "\n")
			records[1] = records[0]
			metadata.sums = strings.Join(records, "\n") + "\n"
		}},
		{"checksum_extra", func(metadata *distributionMetadata) { metadata.sums += strings.Repeat("f", 64) + "  extra.tar.gz\n" }},
		{"checksum_path", func(metadata *distributionMetadata) {
			metadata.sums = strings.Replace(metadata.sums, "  cw_", "  ../cw_", 1)
		}},
		{"checksum_garbage", func(metadata *distributionMetadata) { metadata.sums += "garbage\n" }},
	}
	for _, version := range distributionInvalidVersions() {
		cases = append(cases, struct {
			name   string
			change func(*distributionMetadata)
		}{"tag_" + version, func(metadata *distributionMetadata) {
			metadata.release["tag_name"] = version
			metadata.tag["ref"] = "refs/tags/" + version
		}})
	}
	for _, scenario := range cases {
		test.Run(scenario.name, func(test *testing.T) {
			metadata := distributionReleaseMetadata()
			scenario.change(&metadata)
			replies := metadata.replies(test)
			fixture := distributionServe(test, replies)
			if _, err := fixture.client.Latest(test.Context()); err == nil {
				test.Fatal("Latest accepted invalid release metadata or checksum inventory")
			}
			fixture.check(test, distributionPaths(replies), "/api/releases/latest")
		})
	}
}
func TestR_D2_LatestHTTPJSONAndCancellationFailures(test *testing.T) {
	for _, path := range []string{"/api/releases/latest", "/api/git/ref/tags/v2.0.10", "/download/v2.0.10/SHA256SUMS"} {
		for _, status := range []int{404, 429, 503, 200} {
			test.Run(fmt.Sprintf("%s_%d", path, status), func(test *testing.T) {
				replies := distributionReleaseMetadata().replies(test)
				replies[path] = distributionReply{[]byte("{"), status}
				fixture := distributionServe(test, replies)
				if _, err := fixture.client.Latest(test.Context()); err == nil {
					test.Fatal("Latest accepted HTTP failure or malformed response")
				}
				fixture.check(test, distributionPaths(replies), "/api/releases/latest", path)
			})
		}
	}
	test.Run("cancelled", func(test *testing.T) {
		replies := distributionReleaseMetadata().replies(test)
		fixture := distributionServe(test, replies)
		ctx, cancel := context.WithCancel(test.Context())
		cancel()
		if _, err := fixture.client.Latest(ctx); err == nil {
			test.Fatal("Latest ignored cancelled context")
		}
		fixture.check(test, distributionPaths(replies))
	})
}
