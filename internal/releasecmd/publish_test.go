package main

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"
)

func TestPublishVerifiedRelease(t *testing.T) {
	const commit = "1234567890123456789012345678901234567890"
	const tag = "v0.1.0-rc.1"
	const repo = "belevtsev/kitsu"
	expected := map[string]string{"install.sh": "sha256:installer", manifestName: "sha256:manifest"}
	for _, example := range []struct {
		name        string
		block       string
		expectedErr string
	}{
		{name: "fresh draft"},
		{name: "recover uploaded installer", block: "partial draft"},
		{name: "require immutable setting", block: "mutable", expectedErr: "enable immutable releases"},
		{name: "never replace published release", block: "published", expectedErr: "not a compatible draft"},
		{name: "require exact tag SHA", block: "wrong ref", expectedErr: "does not point to"},
		{name: "require expected existing assets", block: "unexpected asset", expectedErr: "unexpected asset or digest"},
		{name: "require uploaded digest", block: "wrong digest", expectedErr: "unexpected asset or digest"},
		{name: "require complete upload", block: "missing asset", expectedErr: "still needs assets"},
	} {
		t.Run(example.name, func(t *testing.T) {
			installer := releaseAsset{Name: "install.sh", State: "uploaded", Digest: expected["install.sh"]}
			manifest := releaseAsset{Name: manifestName, State: "uploaded", Digest: expected[manifestName]}
			draft := release{ID: 1, Tag: tag, Draft: true, Prerelease: true}
			created, uploaded, published := false, false, false
			gh := func(args ...string) ([]byte, error) {
				t.Helper()
				request := strings.Join(args, " ")
				switch {
				case request == "api repos/"+repo+"/immutable-releases":
					return json.Marshal(map[string]bool{"enabled": example.block != "mutable"})
				case request == "api --paginate --slurp repos/"+repo+"/releases?per_page=100":
					switch example.block {
					case "published":
						existing := draft
						existing.Draft = false
						return json.Marshal([][]release{{existing}})
					case "partial draft", "unexpected asset":
						draft.Assets = []releaseAsset{installer}
						if example.block == "unexpected asset" {
							draft.Assets[0].Name = "extra.zip"
						}
						return json.Marshal([][]release{{draft}})
					default:
						return []byte("[[]]"), nil
					}
				case request == "api repos/"+repo+"/git/matching-refs/tags/"+tag:
					switch example.block {
					case "wrong ref":
						return []byte(`[{"ref":"refs/tags/` + tag + `","object":{"type":"commit","sha":"other"}}]`), nil
					case "partial draft":
						return []byte(`[{"ref":"refs/tags/` + tag + `","object":{"type":"commit","sha":"` + commit + `"}}]`), nil
					default:
						return []byte(`[{"ref":"refs/tags/` + tag + `.2","object":{"type":"commit","sha":"other"}}]`), nil
					}
				case request == "api --method POST repos/"+repo+"/git/refs -f ref=refs/tags/"+tag+" -f sha="+commit:
					created = true
					return []byte(`{"ref":"refs/tags/` + tag + `","object":{"type":"commit","sha":"` + commit + `"}}`), nil
				case strings.HasPrefix(request, "api --method POST repos/"+repo+"/releases "):
					if !created || !slices.Contains(args, "target_commitish="+commit) || !slices.Contains(args, "draft=true") ||
						!slices.Contains(args, "prerelease=true") {
						t.Fatalf("unexpected draft creation: %v", args)
					}
					return json.Marshal(draft)
				case strings.HasPrefix(request, "release upload "):
					want := []string{manifestName, "install.sh"}
					if example.block == "partial draft" {
						want = want[:1]
					}
					if args[2] != tag || !slices.Equal(args[3:], want) {
						t.Fatalf("uploaded %v, want %v", args, want)
					}
					uploaded = true
					draft.Assets = []releaseAsset{installer, manifest}
					return nil, nil
				case request == "api repos/"+repo+"/releases/1":
					if !uploaded {
						t.Fatal("read the final draft before uploading the assets")
					}
					switch example.block {
					case "wrong digest":
						draft.Assets[0].Digest = "sha256:other"
					case "missing asset":
						draft.Assets = draft.Assets[:1]
					}
					return json.Marshal(draft)
				case request == "api --method PATCH repos/"+repo+"/releases/1 -F draft=false -f make_latest=false":
					published = true
					draft.Draft, draft.Immutable = false, true
					return json.Marshal(draft)
				default:
					t.Fatalf("unexpected gh invocation: %v", args)
					return nil, nil
				}
			}
			err := publishRelease(gh, repo, tag, commit, expected)
			if example.expectedErr == "" {
				if err != nil || !published {
					t.Fatalf("publication success=%v: %v", published, err)
				}
			} else if err == nil || published || !strings.Contains(err.Error(), example.expectedErr) {
				t.Fatalf("publication success=%v: %v, expected %q", published, err, example.expectedErr)
			}
		})
	}
}
