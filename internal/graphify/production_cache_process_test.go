//go:build darwin || linux

package graphify

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"
)

type productionArtifact struct {
	Mode fs.FileMode
	Data []byte
}

func productionSnapshot(test *testing.T, root string) map[string]productionArtifact {
	test.Helper()
	artifacts := make(map[string]productionArtifact)
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		artifact := productionArtifact{Mode: info.Mode()}
		switch {
		case info.Mode().IsRegular():
			artifact.Data, err = os.ReadFile(path)
		case info.Mode()&os.ModeSymlink != 0:
			var target string
			target, err = os.Readlink(path)
			artifact.Data = []byte(target)
		}
		if err != nil {
			return err
		}
		artifacts[relative] = artifact
		return nil
	})
	if err != nil {
		test.Fatal(err)
	}
	return artifacts
}

func productionWantSnapshot(test *testing.T, root string, expected map[string]productionArtifact) {
	test.Helper()
	if actual := productionSnapshot(test, root); !reflect.DeepEqual(actual, expected) {
		test.Fatalf("artifact bytes/modes changed at %s: got %#v, want %#v", root, actual, expected)
	}
}

func productionCopyTree(test *testing.T, source, destination string) {
	test.Helper()
	for relative, artifact := range productionSnapshot(test, source) {
		if artifact.Mode.IsDir() {
			if err := os.MkdirAll(filepath.Join(destination, relative), artifact.Mode.Perm()); err != nil {
				test.Fatal(err)
			}
		}
	}
	for relative, artifact := range productionSnapshot(test, source) {
		if !artifact.Mode.IsRegular() {
			continue
		}
		if err := os.WriteFile(filepath.Join(destination, relative), artifact.Data, artifact.Mode.Perm()); err != nil {
			test.Fatal(err)
		}
	}
}

func productionRead(test *testing.T, path string) []byte {
	test.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		test.Fatal(err)
	}
	return data
}

func productionWrite(test *testing.T, path string, data []byte) {
	test.Helper()
	if err := os.WriteFile(path, data, 0o600); err != nil {
		test.Fatal(err)
	}
}

func productionJSONField(test *testing.T, data []byte, name string, value any) []byte {
	test.Helper()
	var fields map[string]jsontext.Value
	if err := json.Unmarshal(data, &fields); err != nil {
		test.Fatal(err)
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		test.Fatal(err)
	}
	fields[name] = encoded
	result, err := json.Marshal(fields)
	if err != nil {
		test.Fatal(err)
	}
	return result
}

func productionDistinctUndirectedGraph(test *testing.T, data []byte) []byte {
	test.Helper()
	var graph struct {
		Nodes    []jsontext.Value `json:"nodes"`
		Directed bool             `json:"directed"`
	}
	if err := json.Unmarshal(data, &graph); err != nil {
		test.Fatal(err)
	}
	if graph.Directed || len(graph.Nodes) == 0 {
		test.Fatal("winner fixture requires an existing undirected graph with source nodes")
	}
	node := productionJSONField(test, graph.Nodes[0], "id", "production-winner-node")
	node = productionJSONField(test, node, "label", "production winner node")
	return productionJSONField(test, data, "nodes", append(graph.Nodes, node))
}

func productionEntry(result BuildResult) string {
	return filepath.Dir(filepath.Dir(result.Graph))
}

func productionHarness(test *testing.T) *harness {
	test.Helper()
	test.Setenv("HOME", test.TempDir())
	test.Setenv("XDG_CONFIG_HOME", test.TempDir())
	test.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	test.Setenv("GIT_CONFIG_SYSTEM", os.DevNull)
	test.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	test.Setenv("GIT_CONFIG_COUNT", "0")
	test.Setenv("GIT_CONFIG_PARAMETERS", "")
	test.Setenv("GIT_TEMPLATE_DIR", test.TempDir())
	test.Setenv("GIT_TERMINAL_PROMPT", "0")
	return newHarness(test)
}

func productionBuild(test *testing.T, fixture *harness) BuildResult {
	test.Helper()
	result, err := Build(test.Context(), fixture.options())
	if err != nil {
		test.Fatal(err)
	}
	return result
}

func productionRequireReady(test *testing.T, entry string) {
	test.Helper()
	graph := productionRead(test, filepath.Join(entry, "graphify-out", "graph.json"))
	var ready readyEntry
	if err := json.Unmarshal(productionRead(test, filepath.Join(entry, "ready.json")), &ready); err != nil {
		test.Fatal(err)
	}
	digest := sha256.Sum256(graph)
	if ready.GraphSHA256 != hex.EncodeToString(digest[:]) {
		test.Fatalf("published graph digest = %q, want %x", ready.GraphSHA256, digest)
	}
}

func productionRequireCandidate(test *testing.T, entry string) {
	test.Helper()
	var graph map[string]jsontext.Value
	if err := json.Unmarshal(productionRead(test, filepath.Join(entry, "graphify-out", "graph.json")), &graph); err != nil {
		test.Fatalf("candidate publication attempted before graph validation: %v", err)
	}
	if _, err := os.Stat(filepath.Join(entry, "ready.json")); err == nil {
		productionRequireReady(test, entry)
	} else if !errors.Is(err, os.ErrNotExist) {
		test.Fatal(err)
	}
}

func TestProductionCacheRepairsPreserveFailedEntry(test *testing.T) {
	for _, corruption := range []string{"graph-digest", "graph-invalid", "graph-directed", "digest", "missing-ready", "malformed-ready"} {
		test.Run(corruption, func(test *testing.T) {
			fixture := productionHarness(test)
			initial := productionBuild(test, fixture)
			entry := productionEntry(initial)
			readyPath := filepath.Join(entry, "ready.json")
			validGraph := productionRead(test, initial.Graph)
			extractions := fixture.extractions(test)
			if hit := productionBuild(test, fixture); !hit.Reused || fixture.extractions(test) != extractions {
				test.Fatal("valid ready entry was not reused without extraction")
			}
			switch corruption {
			case "graph-digest":
				productionWrite(test, initial.Graph, append(bytes.Clone(validGraph), '\n'))
			case "graph-invalid":
				invalidGraph := []byte(`{"nodes":`)
				digest := sha256.Sum256(invalidGraph)
				productionWrite(test, initial.Graph, invalidGraph)
				productionWrite(test, readyPath, productionJSONField(test, productionRead(test, readyPath), "graph_sha256", hex.EncodeToString(digest[:])))
			case "graph-directed":
				invalidGraph := productionJSONField(test, validGraph, "directed", true)
				digest := sha256.Sum256(invalidGraph)
				invalidSummary := initial.Summary
				invalidSummary.Directed = true
				invalidReady := productionJSONField(test, productionRead(test, readyPath), "graph_sha256", hex.EncodeToString(digest[:]))
				invalidReady = productionJSONField(test, invalidReady, "summary", invalidSummary)
				productionWrite(test, initial.Graph, invalidGraph)
				productionWrite(test, readyPath, invalidReady)
			case "digest":
				productionWrite(test, readyPath, productionJSONField(test, productionRead(test, readyPath), "graph_sha256", strings.Repeat("0", 64)))
			case "missing-ready":
				if err := os.Remove(readyPath); err != nil {
					test.Fatal(err)
				}
			case "malformed-ready":
				productionWrite(test, readyPath, []byte(`{"source":`))
			}
			corrupt := productionSnapshot(test, entry)
			if _, err := Query(test.Context(), fixture.options()); err == nil {
				test.Fatal("query accepted stale cache before repair")
			}
			productionWantSnapshot(test, entry, corrupt)
			fixture.config.Mode = "fail"
			fixture.install(test)
			if _, err := Build(test.Context(), fixture.options()); err == nil {
				test.Fatal("failed extraction unexpectedly repaired entry")
			}
			productionWantSnapshot(test, entry, corrupt)
			if _, err := Query(test.Context(), fixture.options()); err == nil {
				test.Fatal("query accepted entry after unsuccessful repair")
			}
			fixture.config.Mode = ""
			fixture.install(test)
			repaired := productionBuild(test, fixture)
			if repaired.Reused || !reflect.DeepEqual(repaired.Summary, initial.Summary) {
				test.Fatalf("repair result = %#v, initial summary = %#v", repaired, initial.Summary)
			}
			productionRequireReady(test, entry)
			if _, err := Query(test.Context(), fixture.options()); err != nil {
				test.Fatalf("query rejected repaired entry: %v", err)
			}
			if !productionBuild(test, fixture).Reused {
				test.Fatal("repaired entry was not reusable")
			}
		})
	}
}

func TestProductionCandidateValidationPrecedesQuarantine(test *testing.T) {
	for _, mode := range []string{"fail", "invalid-json"} {
		test.Run(mode, func(test *testing.T) {
			fixture := productionHarness(test)
			entry := productionEntry(productionBuild(test, fixture))
			productionWrite(test, filepath.Join(entry, "ready.json"), []byte("invalid ready"))
			before := productionSnapshot(test, entry)
			if mode == "fail" {
				fixture.config.Mode = mode
			}
			fixture.install(test)
			options := fixture.options()
			var moved atomic.Bool
			options.renameCache = func(source, destination string) error {
				if source == entry && destination == entry+".repair" {
					moved.Store(true)
				}
				return os.Rename(source, destination)
			}
			var buildErr error
			if mode == "fail" {
				_, buildErr = Build(test.Context(), options)
			} else {
				peer, done := productionGatedBuild(test, fixture, func(configured *Options) {
					configured.renameCache = options.renameCache
				})
				output := ""
				for _, call := range fixture.calls(test) {
					if len(call.Args) > 0 && call.Args[0] == "extract" {
						output = call.Env["GRAPHIFY_OUT"]
					}
				}
				if output == "" {
					test.Fatal("gated synthetic extractor did not declare its output directory")
				}
				productionWrite(test, filepath.Join(output, "graph.json"), []byte(`{"nodes":`))
				peer.send(test, 'x')
				peer.requireExit(test)
				buildErr = productionAwaitBuild(test, done).Err
			}
			if buildErr == nil {
				test.Fatal("invalid candidate accepted")
			}
			if moved.Load() {
				test.Fatal("invalid old entry moved before candidate validation succeeded")
			}
			productionWantSnapshot(test, entry, before)
		})
	}
}

func TestProductionPublishRollbackAndRecovery(test *testing.T) {
	for _, failRestore := range []bool{false, true} {
		test.Run(fmt.Sprintf("restore-fails-%t", failRestore), func(test *testing.T) {
			fixture := productionHarness(test)
			entry := productionEntry(productionBuild(test, fixture))
			repair := entry + ".repair"
			productionWrite(test, filepath.Join(entry, "ready.json"), []byte("old invalid ready"))
			before := productionSnapshot(test, entry)
			publishFailure := errors.New("production injected candidate publication failure")
			restoreFailure := errors.New("production injected quarantine restoration failure")
			quarantined, published, restored := false, false, false
			options := fixture.options()
			options.renameCache = func(source, destination string) error {
				switch {
				case source == entry && destination == repair:
					productionWantSnapshot(test, source, before)
					quarantined = true
					return os.Rename(source, destination)
				case source == repair && destination == entry:
					restored = true
					if failRestore {
						return restoreFailure
					}
					return os.Rename(source, destination)
				case destination == entry:
					published = true
					if !quarantined {
						test.Error("candidate publication bypassed stable quarantine")
					}
					productionRequireCandidate(test, source)
					if _, err := os.Lstat(entry); !errors.Is(err, os.ErrNotExist) {
						test.Errorf("destination exposed before atomic publication: %v", err)
					}
					queryContext, cancel := context.WithTimeout(test.Context(), 3*time.Second)
					defer cancel()
					if _, err := Query(queryContext, fixture.options()); err == nil {
						test.Error("query observed success before publication")
					}
					return publishFailure
				default:
					return os.Rename(source, destination)
				}
			}
			_, err := Build(test.Context(), options)
			if err == nil || !strings.Contains(err.Error(), publishFailure.Error()) || !quarantined || !published || !restored {
				test.Fatalf("missing failed-publish/rollback events: err=%v quarantine=%t publish=%t restore=%t", err, quarantined, published, restored)
			}
			if failRestore {
				if !strings.Contains(err.Error(), restoreFailure.Error()) || !strings.Contains(err.Error(), repair) {
					test.Fatalf("rollback error omits cause or recoverable path: %v", err)
				}
				productionWantSnapshot(test, repair, before)
				if _, err := os.Lstat(entry); !errors.Is(err, os.ErrNotExist) {
					test.Fatalf("failed rollback left partial destination: %v", err)
				}
			} else {
				productionWantSnapshot(test, entry, before)
				if _, err := os.Lstat(repair); !errors.Is(err, os.ErrNotExist) {
					test.Fatalf("successful rollback retained quarantine: %v", err)
				}
			}
			recoveredOld := !failRestore
			retry := fixture.options()
			retry.renameCache = func(source, destination string) error {
				if source == repair && destination == entry {
					productionWantSnapshot(test, source, before)
					recoveredOld = true
				}
				if destination == entry && source != repair && !recoveredOld {
					test.Error("replacement bypassed abandoned-quarantine restoration")
				}
				return os.Rename(source, destination)
			}
			result, err := Build(test.Context(), retry)
			if err != nil || result.Reused || !recoveredOld {
				test.Fatalf("reattempt recovery = %#v, %v", result, err)
			}
			productionRequireReady(test, entry)
			if _, err := Query(test.Context(), fixture.options()); err != nil {
				test.Fatal(err)
			}
		})
	}
}

func TestProductionAbandonedAuxiliaryRepair(test *testing.T) {
	for _, validActive := range []bool{false, true} {
		test.Run(fmt.Sprintf("valid-active-%t", validActive), func(test *testing.T) {
			fixture := productionHarness(test)
			initial := productionBuild(test, fixture)
			entry, repair := productionEntry(initial), productionEntry(initial)+".repair"
			productionCopyTree(test, entry, repair)
			productionWrite(test, filepath.Join(repair, "ready.json"), []byte("abandoned invalid ready"))
			if !validActive {
				productionWrite(test, filepath.Join(entry, "ready.json"), []byte("active invalid ready"))
				activeBefore, repairBefore := productionSnapshot(test, entry), productionSnapshot(test, repair)
				fixture.config.Mode = "fail"
				fixture.install(test)
				if _, err := Build(test.Context(), fixture.options()); err == nil {
					test.Fatal("failed candidate accepted")
				}
				productionWantSnapshot(test, entry, activeBefore)
				productionWantSnapshot(test, repair, repairBefore)
				fixture.config.Mode = ""
				fixture.install(test)
			}
			activeBefore := productionSnapshot(test, entry)
			result := productionBuild(test, fixture)
			if result.Reused != validActive {
				test.Fatalf("reused = %t, valid active = %t", result.Reused, validActive)
			}
			if validActive {
				productionWantSnapshot(test, entry, activeBefore)
			}
			if _, err := os.Lstat(repair); !errors.Is(err, os.ErrNotExist) {
				test.Fatalf("abandoned auxiliary repair was not reconciled: %v", err)
			}
			productionRequireReady(test, entry)
		})
	}
}

func TestProductionFailedPublishPreservesArrivingWinner(test *testing.T) {
	fixture := productionHarness(test)
	initial := productionBuild(test, fixture)
	entry := productionEntry(initial)
	winnerDirectory := filepath.Join(fixture.root, "arriving-winner")
	productionCopyTree(test, entry, winnerDirectory)
	winnerGraphPath := filepath.Join(winnerDirectory, "graphify-out", "graph.json")
	winnerReadyPath := filepath.Join(winnerDirectory, "ready.json")
	winnerGraph := productionDistinctUndirectedGraph(test, productionRead(test, winnerGraphPath))
	winnerDigest := sha256.Sum256(winnerGraph)
	winnerSummary := initial.Summary
	winnerSummary.Nodes++
	winnerSummary.Directed = false
	winnerReady := productionJSONField(test, productionRead(test, winnerReadyPath), "graph_sha256", hex.EncodeToString(winnerDigest[:]))
	winnerReady = productionJSONField(test, winnerReady, "summary", winnerSummary)
	productionWrite(test, winnerGraphPath, winnerGraph)
	productionWrite(test, winnerReadyPath, winnerReady)
	winner := productionSnapshot(test, winnerDirectory)
	productionWrite(test, filepath.Join(entry, "ready.json"), []byte("old invalid ready"))
	publicationFailure := errors.New("production publication failed while a winner appeared")
	attempted, restoredOverWinner := false, false
	options := fixture.options()
	options.renameCache = func(source, destination string) error {
		if source == entry+".repair" && destination == entry {
			restoredOverWinner = true
			return errors.New("refuse test rollback over valid winner")
		}
		if destination == entry {
			attempted = true
			productionRequireCandidate(test, source)
			if err := os.Rename(winnerDirectory, entry); err != nil {
				return err
			}
			return publicationFailure
		}
		return os.Rename(source, destination)
	}
	result, err := Build(test.Context(), options)
	if !attempted || restoredOverWinner {
		test.Fatalf("publication/rollback winner guard failed: attempted=%t restore=%t err=%v", attempted, restoredOverWinner, err)
	}
	if err != nil && !strings.Contains(err.Error(), publicationFailure.Error()) {
		test.Fatalf("publication error lost its cause: %v", err)
	}
	if err == nil && (!result.Reused || !reflect.DeepEqual(result.Summary, winnerSummary)) {
		test.Fatalf("successful recovery did not report actual winner: %#v", result)
	}
	productionWantSnapshot(test, entry, winner)
	if _, err := Query(test.Context(), fixture.options()); err != nil {
		test.Fatalf("rollback damaged arriving valid winner: %v", err)
	}
	if hit := productionBuild(test, fixture); !hit.Reused || !reflect.DeepEqual(hit.Summary, winnerSummary) {
		test.Fatalf("arriving winner was not retained on reattempt: %#v", hit)
	}
	productionWantSnapshot(test, entry, winner)
}

func TestProductionCachePathRefusals(test *testing.T) {
	for _, component := range []string{"root", "entry", "output"} {
		for _, kind := range []string{"symlink", "file"} {
			test.Run(component+"/"+kind, func(test *testing.T) {
				fixture := productionHarness(test)
				initial := productionBuild(test, fixture)
				victim := productionEntry(initial)
				if component == "root" {
					victim = fixture.cache
				} else if component == "output" {
					victim = filepath.Dir(initial.Graph)
				}
				if err := os.Rename(victim, filepath.Join(fixture.root, "saved-cache-component")); err != nil {
					test.Fatal(err)
				}
				target := filepath.Join(fixture.root, "outside-target")
				if err := os.Mkdir(target, 0o700); err != nil {
					test.Fatal(err)
				}
				productionWrite(test, filepath.Join(target, "sentinel"), []byte("do not touch target"))
				before := productionSnapshot(test, target)
				if kind == "symlink" {
					if err := os.Symlink(target, victim); err != nil {
						test.Fatal(err)
					}
				} else {
					productionWrite(test, victim, []byte("not a cache directory"))
				}
				if component == "root" && kind == "symlink" {
					result := productionBuild(test, fixture)
					if result.Reused || !reflect.DeepEqual(result.Summary, initial.Summary) {
						test.Fatalf("safe root alias did not build the expected graph: %#v", result)
					}
					productionRequireReady(test, productionEntry(result))
					if _, err := Query(test.Context(), fixture.options()); err != nil {
						test.Fatalf("query rejected a safe explicit cache-root alias: %v", err)
					}
					resolvedGraph, err := filepath.EvalSymlinks(result.Graph)
					if err != nil {
						test.Fatal(err)
					}
					relativeGraph, err := filepath.Rel(target, resolvedGraph)
					if err != nil || filepath.IsAbs(relativeGraph) || relativeGraph == ".." || strings.HasPrefix(relativeGraph, ".."+string(filepath.Separator)) {
						test.Fatalf("safe root alias escaped its selected target: graph=%q relative=%q err=%v", resolvedGraph, relativeGraph, err)
					}
					if actual, err := os.Readlink(victim); err != nil || actual != target {
						test.Fatalf("safe root alias was replaced: target=%q err=%v", actual, err)
					}
					after := productionSnapshot(test, target)
					for relative, artifact := range before {
						if !reflect.DeepEqual(after[relative], artifact) {
							test.Fatalf("safe root alias changed preexisting target artifact %q", relative)
						}
					}
					return
				}
				if component == "output" {
					corrupted := productionSnapshot(test, productionEntry(initial))
					if _, err := Query(test.Context(), fixture.options()); err == nil {
						test.Fatal("query accepted corrupt interior output before repair")
					}
					productionWantSnapshot(test, productionEntry(initial), corrupted)
					productionWantSnapshot(test, target, before)
					repaired := productionBuild(test, fixture)
					if repaired.Reused || !reflect.DeepEqual(repaired.Summary, initial.Summary) {
						test.Fatalf("interior corruption was not safely rebuilt: %#v", repaired)
					}
					productionRequireReady(test, productionEntry(repaired))
					if info, err := os.Lstat(victim); err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
						test.Fatalf("repaired output is not an owned real directory: %v", err)
					}
					if _, err := Query(test.Context(), fixture.options()); err != nil {
						test.Fatalf("query rejected repaired interior output: %v", err)
					}
					if !productionBuild(test, fixture).Reused {
						test.Fatal("repaired interior output was not reusable")
					}
					productionWantSnapshot(test, target, before)
					return
				}
				if _, err := Build(test.Context(), fixture.options()); err == nil {
					test.Fatal("build accepted unsafe cache component")
				}
				if _, err := Query(test.Context(), fixture.options()); err == nil {
					test.Fatal("query accepted unsafe cache component")
				}
				productionWantSnapshot(test, target, before)
				if kind == "symlink" {
					if actual, err := os.Readlink(victim); err != nil || actual != target {
						test.Fatalf("refusal replaced symlink: target=%q err=%v", actual, err)
					}
				} else if !bytes.Equal(productionRead(test, victim), []byte("not a cache directory")) {
					test.Fatal("refusal replaced non-directory component")
				}
			})
		}
	}
}

func TestProductionProtectedCacheRootAlias(test *testing.T) {
	fixture := productionHarness(test)
	if err := os.Symlink(fixture.repo, fixture.cache); err != nil {
		test.Fatal(err)
	}
	before := productionSnapshot(test, fixture.repo)
	if _, err := Build(test.Context(), fixture.options()); err == nil {
		test.Fatal("build accepted a cache-root alias into the protected repository")
	}
	if _, err := Query(test.Context(), fixture.options()); err == nil {
		test.Fatal("query accepted a cache-root alias into the protected repository")
	}
	productionWantSnapshot(test, fixture.repo, before)
	if actual, err := os.Readlink(fixture.cache); err != nil || actual != fixture.repo {
		test.Fatalf("protected-root refusal replaced the alias: target=%q err=%v", actual, err)
	}
}

type productionBuildOutcome struct {
	Result BuildResult
	Err    error
}

func productionListen(test *testing.T) *net.TCPListener {
	test.Helper()
	listener, err := net.ListenTCP("tcp", &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		test.Fatal(err)
	}
	test.Cleanup(func() { listener.Close() })
	return listener
}

type productionPeer struct {
	Connection net.Conn
	Reader     *bufio.Reader
	Role       string
	PID        int
	Exited     bool
}

func productionAccept(test *testing.T, listener *net.TCPListener) *productionPeer {
	test.Helper()
	if err := listener.SetDeadline(time.Now().Add(10 * time.Second)); err != nil {
		test.Fatal(err)
	}
	connection, err := listener.Accept()
	if err != nil {
		test.Fatal(err)
	}
	peer := &productionPeer{Connection: connection, Reader: bufio.NewReader(connection)}
	test.Cleanup(func() {
		if peer.PID > 0 && !peer.Exited {
			_ = syscall.Kill(peer.PID, syscall.SIGKILL)
		}
		connection.Close()
	})
	if err := connection.SetReadDeadline(time.Now().Add(10 * time.Second)); err != nil {
		test.Fatal(err)
	}
	line, err := peer.Reader.ReadString('\n')
	if err != nil {
		test.Fatal(err)
	}
	fields := strings.Fields(line)
	if len(fields) < 1 || len(fields) > 2 {
		test.Fatalf("invalid child event: %q", line)
	}
	peer.Role = fields[0]
	if len(fields) == 2 {
		peer.PID, err = strconv.Atoi(fields[1])
		if err != nil || peer.PID <= 0 {
			test.Fatalf("invalid child PID event: %q", line)
		}
	}
	return peer
}

func (peer *productionPeer) send(test *testing.T, command byte) {
	test.Helper()
	if err := peer.Connection.SetWriteDeadline(time.Now().Add(5 * time.Second)); err != nil {
		test.Fatal(err)
	}
	if count, err := peer.Connection.Write([]byte{command}); err != nil || count != 1 {
		test.Fatalf("child gate write = %d, %v", count, err)
	}
}

func (peer *productionPeer) requireExit(test *testing.T) {
	test.Helper()
	if err := peer.Connection.SetReadDeadline(time.Now().Add(5 * time.Second)); err != nil {
		test.Fatal(err)
	}
	if _, err := peer.Reader.ReadByte(); !errors.Is(err, io.EOF) && !errors.Is(err, syscall.ECONNRESET) {
		test.Fatalf("%s PID %d still holds its lifetime gate: %v", peer.Role, peer.PID, err)
	}
	peer.Exited = true
}

func productionGatedBuild(test *testing.T, fixture *harness, configure ...func(*Options)) (*productionPeer, <-chan productionBuildOutcome) {
	test.Helper()
	listener := productionListen(test)
	fixture.config.ProductionGate = listener.Addr().String()
	fixture.config.ProductionDeadline = time.Now().Add(35 * time.Second).Format(time.RFC3339Nano)
	fixture.install(test)
	ctx, cancel := context.WithTimeout(test.Context(), 25*time.Second)
	options := fixture.options()
	options.Timeout = 20 * time.Second
	for _, apply := range configure {
		apply(&options)
	}
	done := make(chan productionBuildOutcome, 1)
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		result, err := Build(ctx, options)
		done <- productionBuildOutcome{Result: result, Err: err}
	}()
	test.Cleanup(func() {
		cancel()
		select {
		case <-finished:
		case <-time.After(10 * time.Second):
			test.Error("gated build did not join after cleanup cancellation")
		}
	})
	peer := productionAccept(test, listener)
	if peer.Role != "extract" {
		test.Fatalf("extraction event = %q", peer.Role)
	}
	return peer, done
}

func productionAwaitBuild(test *testing.T, done <-chan productionBuildOutcome) productionBuildOutcome {
	test.Helper()
	select {
	case outcome := <-done:
		return outcome
	case <-time.After(10 * time.Second):
		test.Fatal("build did not settle within bounded publication deadline")
		return productionBuildOutcome{}
	}
}

func productionTryOSLock(test *testing.T, entry string) (func(), bool) {
	test.Helper()
	ctx, cancel := context.WithTimeout(test.Context(), 2*time.Second)
	defer cancel()
	type lockOutcome struct {
		Release func()
		Err     error
	}
	done := make(chan lockOutcome, 1)
	go func() {
		release, err := acquireCacheLock(ctx, entry+".lock")
		if err == nil && ctx.Err() != nil {
			release()
			release, err = nil, ctx.Err()
		}
		done <- lockOutcome{Release: release, Err: err}
	}()
	var outcome lockOutcome
	select {
	case outcome = <-done:
	case <-time.After(5 * time.Second):
		test.Fatal("publication-lock probe ignored context bound")
	}
	if outcome.Err != nil {
		if ctx.Err() == nil {
			test.Fatalf("safe publication-lock path refused: %v", outcome.Err)
		}
		return func() {}, false
	}
	var once sync.Once
	release := func() {
		once.Do(func() {
			outcome.Release()
		})
	}
	test.Cleanup(release)
	return release, true
}

func TestProductionWinnerAfterPublicationWait(test *testing.T) {
	fixture := productionHarness(test)
	initial := productionBuild(test, fixture)
	entry := productionEntry(initial)
	readyPath := filepath.Join(entry, "ready.json")
	winnerGraph := productionDistinctUndirectedGraph(test, productionRead(test, initial.Graph))
	winnerSummary := initial.Summary
	winnerSummary.Nodes++
	winnerSummary.Directed = false
	winnerDigest := sha256.Sum256(winnerGraph)
	winnerReady := productionJSONField(test, productionRead(test, readyPath), "graph_sha256", hex.EncodeToString(winnerDigest[:]))
	winnerReady = productionJSONField(test, winnerReady, "summary", winnerSummary)
	productionWrite(test, readyPath, []byte("invalid old readiness"))
	peer, done := productionGatedBuild(test, fixture)
	release, acquired := productionTryOSLock(test, entry)
	if !acquired {
		peer.send(test, 'x')
		peer.requireExit(test)
		outcome := productionAwaitBuild(test, done)
		if outcome.Err != nil || outcome.Result.Reused {
			test.Fatalf("candidate already serialized during extraction: %#v, %v", outcome.Result, outcome.Err)
		}
		productionRequireReady(test, entry)
		return
	}
	peer.send(test, 'x')
	peer.requireExit(test)
	productionWrite(test, initial.Graph, winnerGraph)
	productionWrite(test, readyPath, winnerReady)
	winner := productionSnapshot(test, entry)
	release()
	outcome := productionAwaitBuild(test, done)
	if outcome.Err != nil || !outcome.Result.Reused || !reflect.DeepEqual(outcome.Result.Summary, winnerSummary) {
		test.Fatalf("waiter did not return actual winning summary/reused: %#v, %v", outcome.Result, outcome.Err)
	}
	productionWantSnapshot(test, entry, winner)
	fixture.config.ProductionGate = ""
	fixture.install(test)
	if _, err := Query(test.Context(), fixture.options()); err != nil {
		test.Fatalf("installed winning entry is not valid: %v", err)
	}
}

func TestProductionRecheckSourceAndPathAfterCandidate(test *testing.T) {
	for _, change := range []string{"source", "path"} {
		test.Run(change, func(test *testing.T) {
			fixture := productionHarness(test)
			initial := productionBuild(test, fixture)
			entry := productionEntry(initial)
			productionWrite(test, filepath.Join(entry, "ready.json"), []byte("old invalid readiness"))
			before := productionSnapshot(test, entry)
			peer, done := productionGatedBuild(test, fixture)
			release, acquired := productionTryOSLock(test, entry)
			if acquired {
				peer.send(test, 'x')
				peer.requireExit(test)
			}
			target := filepath.Join(fixture.root, "after-wait-target")
			if change == "source" {
				fixture.write(test, "main.go", "package main\nfunc changedDuringPublication() {}\n")
			} else {
				if err := os.Mkdir(target, 0o700); err != nil {
					test.Fatal(err)
				}
				productionWrite(test, filepath.Join(target, "sentinel"), []byte("untouched target"))
				if err := os.Rename(entry, filepath.Join(fixture.root, "saved-before-path-swap")); err != nil {
					test.Fatal(err)
				}
				if err := os.Symlink(target, entry); err != nil {
					test.Fatal(err)
				}
			}
			if !acquired {
				peer.send(test, 'x')
				peer.requireExit(test)
			}
			release()
			outcome := productionAwaitBuild(test, done)
			if change == "path" {
				if outcome.Err == nil {
					test.Fatal("publication followed a swapped cache symlink")
				}
				if actual, err := os.Readlink(entry); err != nil || actual != target {
					test.Fatalf("unsafe entry changed: %q %v", actual, err)
				}
				if !bytes.Equal(productionRead(test, filepath.Join(target, "sentinel")), []byte("untouched target")) {
					test.Fatal("unsafe target was modified")
				}
				if entries, err := os.ReadDir(target); err != nil || len(entries) != 1 {
					test.Fatalf("publication wrote into unsafe target: %v %v", entries, err)
				}
			} else if outcome.Err != nil {
				productionWantSnapshot(test, entry, before)
			} else {
				fixture.config.ProductionGate = ""
				fixture.install(test)
				current := productionBuild(test, fixture)
				if outcome.Result.Provenance.SourceSHA256 != current.Provenance.SourceSHA256 {
					test.Fatal("successful waiter returned stale source provenance")
				}
			}
		})
	}
}

func TestProductionBuildWaitBoundAndStableLock(test *testing.T) {
	fixture := productionHarness(test)
	entry := productionEntry(productionBuild(test, fixture))
	productionWrite(test, filepath.Join(entry, "ready.json"), []byte("invalid readiness"))
	before := productionSnapshot(test, entry)
	release, acquired := productionTryOSLock(test, entry)
	if !acquired {
		test.Fatal("fixture lock unexpectedly busy")
	}
	lockBefore, err := os.Stat(entry + ".lock")
	if err != nil {
		test.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(test.Context(), 5*time.Second)
	defer cancel()
	options := fixture.options()
	options.Timeout = 20 * time.Second
	done := make(chan productionBuildOutcome, 1)
	go func() {
		result, err := Build(ctx, options)
		done <- productionBuildOutcome{Result: result, Err: err}
	}()
	if outcome := productionAwaitBuild(test, done); outcome.Err == nil {
		test.Fatal("build bypassed held publication lock")
	}
	productionWantSnapshot(test, entry, before)
	release()
	productionBuild(test, fixture)
	lockAfter, err := os.Lstat(entry + ".lock")
	if err != nil || !lockAfter.Mode().IsRegular() || !os.SameFile(lockBefore, lockAfter) {
		test.Fatalf("publication unlinked/replaced stable lock: %v %v", lockAfter, err)
	}
}

func TestProductionConcurrentCachePublication(test *testing.T) {
	fixture := productionHarness(test)
	entry := productionEntry(productionBuild(test, fixture))
	productionWrite(test, filepath.Join(entry, "ready.json"), []byte("invalid readiness"))
	start := make(chan struct{})
	results := make(chan productionBuildOutcome, 4)
	var renaming atomic.Int32
	var overlap atomic.Bool
	ctx, cancel := context.WithTimeout(test.Context(), 30*time.Second)
	var workers sync.WaitGroup
	workers.Add(4)
	finished := make(chan struct{})
	for worker := 0; worker < 4; worker++ {
		go func() {
			defer workers.Done()
			options := fixture.options()
			options.renameCache = func(source, destination string) error {
				if source == entry || destination == entry || source == entry+".repair" || destination == entry+".repair" {
					if renaming.Add(1) != 1 {
						overlap.Store(true)
					}
					defer renaming.Add(-1)
				}
				return os.Rename(source, destination)
			}
			<-start
			result, err := Build(ctx, options)
			results <- productionBuildOutcome{Result: result, Err: err}
		}()
	}
	go func() {
		workers.Wait()
		close(finished)
	}()
	test.Cleanup(func() {
		cancel()
		select {
		case <-finished:
		case <-time.After(10 * time.Second):
			test.Error("concurrent cache builders did not join")
		}
	})
	close(start)
	created := 0
	for worker := 0; worker < 4; worker++ {
		outcome := productionAwaitBuild(test, results)
		if outcome.Err != nil {
			test.Fatal(outcome.Err)
		}
		if !outcome.Result.Reused {
			created++
		}
		if productionEntry(outcome.Result) != entry {
			test.Fatal("concurrent builders disagreed on stable entry")
		}
	}
	if created != 1 || overlap.Load() {
		test.Fatalf("publication winners = %d, overlapping cache renames = %t", created, overlap.Load())
	}
	productionRequireReady(test, entry)
}

func TestProductionProcessHelper(test *testing.T) {
	separator := slices.Index(os.Args, "--")
	if separator < 0 || len(os.Args) <= separator+1 || os.Args[separator+1] != "production-process" {
		return
	}
	os.Exit(productionProcessMain(os.Args[separator+2:]))
}

func productionProcessMain(arguments []string) int {
	if len(arguments) == 0 {
		return 60
	}
	switch arguments[0] {
	case "refuse-lock":
		if len(arguments) != 3 {
			return 79
		}
		deadline, err := time.Parse(time.RFC3339Nano, arguments[2])
		if err != nil {
			return 80
		}
		ctx, cancel := context.WithDeadline(context.Background(), deadline)
		defer cancel()
		release, err := acquireCacheLock(ctx, arguments[1])
		if err != nil {
			return 0
		}
		release()
		return 81
	case "output":
		if len(arguments) != 4 {
			return 61
		}
		stdout, stdoutErr := hex.DecodeString(arguments[1])
		stderr, stderrErr := hex.DecodeString(arguments[2])
		exitCode, exitErr := strconv.Atoi(arguments[3])
		if stdoutErr != nil || stderrErr != nil || exitErr != nil {
			return 62
		}
		if _, err := os.Stdout.Write(stdout); err != nil {
			return 63
		}
		if _, err := os.Stderr.Write(stderr); err != nil {
			return 64
		}
		return exitCode
	case "leader", "descendant", "lock", "completion-leader", "completion-descendant":
		argumentCount := 4
		if arguments[0] == "completion-leader" || arguments[0] == "completion-descendant" {
			argumentCount = 5
		}
		if len(arguments) != argumentCount {
			return 65
		}
		deadline, err := time.Parse(time.RFC3339Nano, arguments[2])
		if err != nil || !deadline.After(time.Now()) {
			return 66
		}
		if arguments[0] == "lock" {
			ctx, cancel := context.WithDeadline(context.Background(), deadline)
			defer cancel()
			release, err := acquireCacheLock(ctx, arguments[3])
			if err != nil {
				return 67
			}
			defer release()
		}
		connection, err := net.DialTimeout("tcp", arguments[1], 5*time.Second)
		if err != nil {
			return 68
		}
		defer connection.Close()
		if err := connection.SetDeadline(deadline); err != nil {
			return 69
		}
		if _, err := fmt.Fprintf(connection, "%s %d\n", arguments[0], os.Getpid()); err != nil {
			return 70
		}
		var command [1]byte
		if _, err := io.ReadFull(connection, command[:]); err != nil {
			return 71
		}
		if arguments[0] == "completion-leader" {
			return productionCompletionLeader(connection, arguments, command[0])
		}
		if arguments[0] == "completion-descendant" {
			return productionCompletionDescendant(connection, arguments[4], command[0])
		}
		if arguments[0] == "lock" {
			return 0
		}
		if arguments[0] == "descendant" {
			if command[0] == 'o' {
				if _, err := io.WriteString(os.Stdout, strings.Repeat("x", 17)); err != nil {
					return 72
				}
				if _, err := io.WriteString(os.Stderr, strings.Repeat("y", 17)); err != nil {
					return 73
				}
			} else if command[0] == 'w' {
				if _, err := io.WriteString(os.Stdout, "before cancel"); err != nil {
					return 82
				}
				if _, err := io.WriteString(os.Stderr, "diagnostic"); err != nil {
					return 83
				}
				if _, err := io.WriteString(connection, "partial-written\n"); err != nil {
					return 84
				}
			} else {
				return 74
			}
			_, _ = io.ReadFull(connection, command[:])
			return 0
		}
		if command[0] != 's' {
			return 75
		}
		child := exec.Command(os.Args[0], "-test.run=^TestProductionProcessHelper$", "--", "production-process", "descendant", arguments[1], arguments[2], "hold")
		child.Env = []string{}
		child.Stdout, child.Stderr = os.Stdout, os.Stderr
		if err := child.Start(); err != nil {
			return 76
		}
		if arguments[3] == "exit" {
			if err := child.Process.Release(); err != nil {
				return 77
			}
			return 0
		}
		defer func() {
			_ = child.Process.Kill()
			_ = child.Wait()
		}()
		_, _ = io.ReadFull(connection, command[:])
		return 0
	default:
		return 78
	}
}

func productionCompletionLeader(connection net.Conn, arguments []string, command byte) int {
	if command != 's' || (arguments[3] != "inherited-nonzero" && arguments[3] != "redirected-success") {
		return 85
	}
	child := exec.Command(os.Args[0], "-test.run=^TestProductionProcessHelper$", "--", "production-process", "completion-descendant", arguments[1], arguments[2], arguments[3], arguments[4])
	child.Env = []string{}
	child.Stdout, child.Stderr = os.Stdout, os.Stderr
	if arguments[3] == "redirected-success" {
		null, err := os.OpenFile(os.DevNull, os.O_RDWR, 0)
		if err != nil {
			return 86
		}
		defer null.Close()
		child.Stdout, child.Stderr = null, null
	}
	if err := child.Start(); err != nil {
		return 87
	}
	released := false
	defer func() {
		if !released {
			_ = child.Process.Kill()
			_ = child.Wait()
		}
	}()
	var finish [1]byte
	if _, err := io.ReadFull(connection, finish[:]); err != nil || finish[0] != 'e' {
		return 88
	}
	if _, err := io.WriteString(os.Stdout, "leader stdout\n"); err != nil {
		return 89
	}
	if _, err := io.WriteString(os.Stderr, "leader stderr\n"); err != nil {
		return 90
	}
	if err := child.Process.Release(); err != nil {
		return 91
	}
	released = true
	if arguments[3] == "inherited-nonzero" {
		return 1
	}
	return 0
}

func productionCompletionDescendant(connection net.Conn, marker string, command byte) int {
	if command != 'a' {
		return 92
	}
	if _, err := io.WriteString(connection, "armed\n"); err != nil {
		return 93
	}
	var mutation [1]byte
	if _, err := io.ReadFull(connection, mutation[:]); err != nil || mutation[0] != 'm' {
		return 94
	}
	if err := os.WriteFile(marker, []byte("escaped owned descendant\n"), 0o600); err != nil {
		return 95
	}
	if _, err := io.WriteString(connection, "mutated\n"); err != nil {
		return 96
	}
	return 0
}

func productionChildArgs(test *testing.T, arguments ...string) []string {
	test.Helper()
	executable, err := os.Executable()
	if err != nil {
		test.Fatal(err)
	}
	return append([]string{executable, "-test.run=^TestProductionProcessHelper$", "--", "production-process"}, arguments...)
}

type productionProcessOutcome struct {
	Code   int
	Stdout string
	Stderr string
	Err    error
}

// productionHelperEnvironment is the whole environment of a helper started
// through productionRun. The race runtime option stops a successful helper
// exit from sleeping about a second; the helpers read no other variable.
var productionHelperEnvironment = []string{"GORACE=atexit_sleep_ms=0"}

func productionRun(ctx context.Context, arguments []string, timeout time.Duration, limit int) productionProcessOutcome {
	code, stdout, stderr, err := runWithOutputLimit(ctx, arguments, "", slices.Clone(productionHelperEnvironment), timeout, limit)
	return productionProcessOutcome{Code: code, Stdout: stdout, Stderr: stderr, Err: err}
}

func productionAwaitProcess(test *testing.T, done <-chan productionProcessOutcome) productionProcessOutcome {
	test.Helper()
	select {
	case outcome := <-done:
		return outcome
	case <-time.After(5 * time.Second):
		test.Fatal("runner did not join leader/pipe copiers promptly")
		return productionProcessOutcome{}
	}
}

func productionWantEmptyError(test *testing.T, outcome productionProcessOutcome) {
	test.Helper()
	if outcome.Err == nil || outcome.Code != 0 || outcome.Stdout != "" || outcome.Stderr != "" {
		test.Fatalf("error result must be code0 with empty outputs: %#v", outcome)
	}
}

func TestProductionOutputLimitBoundariesAndEncoding(test *testing.T) {
	cases := []struct {
		Name       string
		Stdout     []byte
		Stderr     []byte
		Limit      int
		Exit       int
		WantStdout string
		WantStderr string
		Overflow   bool
	}{
		{Name: "zero-empty", Limit: 0},
		{Name: "zero-byte", Stdout: []byte("x"), Limit: 0, Overflow: true},
		{Name: "combined-equality", Stdout: []byte("abc"), Stderr: []byte("de"), Limit: 5, WantStdout: "abc", WantStderr: "de"},
		{Name: "combined-next-byte", Stdout: []byte("abc"), Stderr: []byte("def"), Limit: 5, Overflow: true},
		{Name: "stderr-alone", Stderr: []byte("abcdef"), Limit: 5, Overflow: true},
		{Name: "utf8-raw-equality", Stdout: []byte("é"), Stderr: []byte("界"), Limit: 5, WantStdout: "é", WantStderr: "界"},
		{Name: "utf8-raw-overflow", Stdout: []byte("é"), Stderr: []byte("界!"), Limit: 5, Overflow: true},
		{Name: "invalid-bytes-each-replaced", Stdout: []byte{0xff, 0xfe, 0xc3, '('}, Stderr: []byte{0x80, 0xf0, 0x9f}, Limit: 7, WantStdout: "\ufffd\ufffd\ufffd(", WantStderr: "\ufffd\ufffd\ufffd"},
		{Name: "nonzero-keeps-output", Stdout: []byte("literal output"), Stderr: []byte("failure detail"), Limit: 64, Exit: 7, WantStdout: "literal output", WantStderr: "failure detail"},
		{Name: "nonzero-invalid-output", Stdout: []byte{0xff, 0xff}, Stderr: []byte{0xfe}, Limit: 3, Exit: 9, WantStdout: "\ufffd\ufffd", WantStderr: "\ufffd"},
	}
	for _, scenario := range cases {
		test.Run(scenario.Name, func(test *testing.T) {
			arguments := productionChildArgs(test, "output", hex.EncodeToString(scenario.Stdout), hex.EncodeToString(scenario.Stderr), strconv.Itoa(scenario.Exit))
			outcome := productionRun(test.Context(), arguments, 10*time.Second, scenario.Limit)
			if scenario.Overflow {
				productionWantEmptyError(test, outcome)
				wantCode(test, outcome.Err, "output_limit")
			} else if outcome.Err != nil || outcome.Code != scenario.Exit || outcome.Stdout != scenario.WantStdout || outcome.Stderr != scenario.WantStderr {
				test.Fatalf("output result = %#v", outcome)
			}
		})
	}
}

func TestProductionOutputCancellationPreservesContextError(test *testing.T) {
	arguments := productionChildArgs(test, "output", "", "", "0")
	cancelled, cancel := context.WithCancel(test.Context())
	cancel()
	outcome := productionRun(cancelled, arguments, 10*time.Second, 8)
	productionWantEmptyError(test, outcome)
	if !errors.Is(outcome.Err, cancelled.Err()) {
		test.Fatalf("cancellation error = %v, want original %v", outcome.Err, cancelled.Err())
	}
	expired, stop := context.WithDeadline(test.Context(), time.Now().Add(-time.Second))
	defer stop()
	outcome = productionRun(expired, arguments, 10*time.Second, 8)
	productionWantEmptyError(test, outcome)
	if !errors.Is(outcome.Err, context.DeadlineExceeded) {
		test.Fatalf("expired context error = %v", outcome.Err)
	}
}

func TestProductionProcessGroupLifetimeAndPipeJoin(test *testing.T) {
	test.Parallel()
	for _, scenario := range []string{"overflow-leader-live", "overflow-leader-exited", "cancel", "timeout", "context-deadline"} {
		test.Run(scenario, func(test *testing.T) {
			listener := productionListen(test)
			lifetime := time.Now().Add(45 * time.Second).Format(time.RFC3339Nano)
			leaderMode := "hold"
			if scenario == "overflow-leader-exited" {
				leaderMode = "exit"
			}
			ctx, cancel := context.WithCancel(test.Context())
			if scenario == "context-deadline" {
				cancel()
				ctx, cancel = context.WithTimeout(test.Context(), 12*time.Second)
			}
			timeout := 30 * time.Second
			if scenario == "timeout" {
				timeout = 12 * time.Second
			}
			arguments := productionChildArgs(test, "leader", listener.Addr().String(), lifetime, leaderMode)
			done := make(chan productionProcessOutcome, 1)
			finished := make(chan struct{})
			go func() {
				defer close(finished)
				done <- productionRun(ctx, arguments, timeout, 32)
			}()
			test.Cleanup(func() {
				cancel()
				select {
				case <-finished:
				case <-time.After(5 * time.Second):
					test.Error("runner leaked after cleanup cancellation")
				}
			})
			leader := productionAccept(test, listener)
			if leader.Role != "leader" {
				test.Fatalf("first event = %q", leader.Role)
			}
			leader.send(test, 's')
			descendant := productionAccept(test, listener)
			if descendant.Role != "descendant" || descendant.PID == leader.PID {
				test.Fatalf("descendant event = %#v", descendant)
			}
			if leaderMode == "exit" {
				leader.requireExit(test)
			}
			if strings.HasPrefix(scenario, "overflow") {
				descendant.send(test, 'o')
			} else {
				descendant.send(test, 'w')
				if err := descendant.Connection.SetReadDeadline(time.Now().Add(5 * time.Second)); err != nil {
					test.Fatal(err)
				}
				if event, err := descendant.Reader.ReadString('\n'); err != nil || event != "partial-written\n" {
					test.Fatalf("partial-output gate = %q, %v", event, err)
				}
			}
			if scenario == "cancel" {
				cancel()
			}
			var outcome productionProcessOutcome
			if scenario == "timeout" || scenario == "context-deadline" {
				select {
				case outcome = <-done:
				case <-time.After(18 * time.Second):
					test.Fatal("timeout/deadline failed to bound live process group")
				}
			} else {
				outcome = productionAwaitProcess(test, done)
			}
			productionWantEmptyError(test, outcome)
			switch scenario {
			case "cancel":
				if !errors.Is(outcome.Err, context.Canceled) {
					test.Fatalf("cancellation error = %v", outcome.Err)
				}
			case "context-deadline":
				if !errors.Is(outcome.Err, context.DeadlineExceeded) {
					test.Fatalf("deadline error = %v", outcome.Err)
				}
			case "timeout":
				wantCode(test, outcome.Err, "timeout")
			default:
				wantCode(test, outcome.Err, "output_limit")
			}
			descendant.requireExit(test)
			if leaderMode != "exit" {
				leader.requireExit(test)
			}
		})
	}
}

func TestProductionProcessCompletionOwnsDescendants(test *testing.T) {
	test.Parallel()
	for _, scenario := range []string{"inherited-nonzero", "redirected-success"} {
		test.Run(scenario, func(test *testing.T) {
			listener := productionListen(test)
			marker := filepath.Join(test.TempDir(), "late-mutation")
			lifetime := time.Now().Add(90 * time.Second).Format(time.RFC3339Nano)
			ctx, cancel := context.WithCancel(test.Context())
			arguments := productionChildArgs(test, "completion-leader", listener.Addr().String(), lifetime, scenario, marker)
			done := make(chan productionProcessOutcome, 1)
			finished := make(chan struct{})
			go func() {
				defer close(finished)
				done <- productionRun(ctx, arguments, 60*time.Second, 64)
			}()
			test.Cleanup(func() {
				cancel()
				select {
				case <-finished:
				case <-time.After(10 * time.Second):
					test.Error("completion runner did not join after cleanup cancellation")
				}
			})
			leader := productionAccept(test, listener)
			if leader.Role != "completion-leader" {
				test.Fatalf("first completion event = %q", leader.Role)
			}
			leaderGroup, err := syscall.Getpgid(leader.PID)
			if err != nil || leaderGroup != leader.PID {
				test.Fatalf("leader does not own an isolated process group: group=%d PID=%d err=%v", leaderGroup, leader.PID, err)
			}
			cleanCompletion := false
			test.Cleanup(func() {
				if !cleanCompletion {
					_ = syscall.Kill(-leaderGroup, syscall.SIGKILL)
				}
			})
			leader.send(test, 's')
			descendant := productionAccept(test, listener)
			if descendant.Role != "completion-descendant" || descendant.PID == leader.PID {
				test.Fatalf("completion descendant event = %#v", descendant)
			}
			descendantGroup, err := syscall.Getpgid(descendant.PID)
			if err != nil || descendantGroup != leaderGroup {
				test.Fatalf("descendant escaped the owned group: group=%d want=%d err=%v", descendantGroup, leaderGroup, err)
			}
			descendant.send(test, 'a')
			if err := descendant.Connection.SetReadDeadline(time.Now().Add(10 * time.Second)); err != nil {
				test.Fatal(err)
			}
			if event, err := descendant.Reader.ReadString('\n'); err != nil || event != "armed\n" {
				test.Fatalf("descendant mutation gate = %q, %v", event, err)
			}
			if _, err := os.Lstat(marker); !errors.Is(err, os.ErrNotExist) {
				test.Fatalf("descendant mutated before its gate: %v", err)
			}
			select {
			case outcome := <-done:
				test.Fatalf("runner returned before the leader exit gate: %#v", outcome)
			default:
			}
			leader.send(test, 'e')
			var outcome productionProcessOutcome
			select {
			case outcome = <-done:
			case <-time.After(25 * time.Second):
				test.Fatal("leader completion exceeded the outer lifetime allowance, including the five-second pipe-copy grace")
			}
			if err := descendant.Connection.SetWriteDeadline(time.Now().Add(5 * time.Second)); err != nil {
				test.Fatal(err)
			}
			_, _ = descendant.Connection.Write([]byte{'m'})
			descendant.requireExit(test)
			if _, err := os.Lstat(marker); !errors.Is(err, os.ErrNotExist) {
				test.Fatalf("returned operation transferred a live mutation-capable descendant: %v", err)
			}
			leader.requireExit(test)
			cleanCompletion = true
			wantExit := 0
			if scenario == "inherited-nonzero" {
				wantExit = 1
			}
			if outcome.Err != nil || outcome.Code != wantExit || outcome.Stdout != "leader stdout\n" || outcome.Stderr != "leader stderr\n" {
				test.Fatalf("owned leader completion changed normal exit/output conventions: %#v", outcome)
			}
		})
	}
}

func TestProductionCacheLockProcessDeathAndRefusals(test *testing.T) {
	test.Parallel()
	root := test.TempDir()
	lockPath := filepath.Join(root, "entry.lock")
	listener := productionListen(test)
	arguments := productionChildArgs(test, "lock", listener.Addr().String(), time.Now().Add(35*time.Second).Format(time.RFC3339Nano), lockPath)
	command := exec.Command(arguments[0], arguments[1:]...)
	command.Env = []string{}
	command.Stdout, command.Stderr = io.Discard, io.Discard
	if err := command.Start(); err != nil {
		test.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- command.Wait() }()
	var waited bool
	test.Cleanup(func() {
		_ = command.Process.Kill()
		if !waited {
			select {
			case <-done: //nolint:errcheck // the holder was killed; only reaping matters
			case <-time.After(5 * time.Second):
				test.Error("lock holder was not reaped")
			}
		}
	})
	holder := productionAccept(test, listener)
	if holder.Role != "lock" || holder.PID != command.Process.Pid {
		test.Fatal("lock ownership event does not identify child")
	}
	before, err := os.Lstat(lockPath)
	if err != nil || !before.Mode().IsRegular() {
		test.Fatalf("lock is not a regular stable file: %v %v", before, err)
	}
	ctx, cancel := context.WithTimeout(test.Context(), 2*time.Second)
	defer cancel()
	bounded := make(chan error, 1)
	go func() {
		release, err := acquireCacheLock(ctx, lockPath)
		if err == nil {
			release()
		}
		bounded <- err
	}()
	select {
	case err := <-bounded:
		if err == nil {
			test.Fatal("second process bypassed held OS lock")
		}
	case <-time.After(5 * time.Second):
		test.Fatal("OS lock wait ignored context bound")
	}
	if err := command.Process.Kill(); err != nil {
		test.Fatal(err)
	}
	select {
	case <-done: //nolint:errcheck // the owner was killed; only its exit matters
		waited = true
	case <-time.After(5 * time.Second):
		test.Fatal("killed lock owner did not exit")
	}
	holder.requireExit(test)
	acquireContext, stop := context.WithTimeout(test.Context(), 5*time.Second)
	defer stop()
	release, err := acquireCacheLock(acquireContext, lockPath)
	if err != nil {
		test.Fatalf("OS did not release killed owner's lock: %v", err)
	}
	release()
	after, err := os.Lstat(lockPath)
	if err != nil || !os.SameFile(before, after) {
		test.Fatalf("lock path was unlinked/replaced: %v %v", after, err)
	}
	for _, kind := range []string{"symlink", "directory", "fifo"} {
		test.Run(kind, func(test *testing.T) {
			path := filepath.Join(root, kind+".lock")
			target := filepath.Join(root, kind+"-target")
			productionWrite(test, target, []byte("target stays exact"))
			switch kind {
			case "symlink":
				if err := os.Symlink(target, path); err != nil {
					test.Fatal(err)
				}
			case "directory":
				if err := os.Mkdir(path, 0o700); err != nil {
					test.Fatal(err)
				}
			case "fifo":
				if err := syscall.Mkfifo(path, 0o600); err != nil {
					test.Fatal(err)
				}
			}
			before, err := os.Lstat(path)
			if err != nil {
				test.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(test.Context(), 7*time.Second)
			defer cancel()
			arguments := productionChildArgs(test, "refuse-lock", path, time.Now().Add(3*time.Second).Format(time.RFC3339Nano))
			probe := exec.CommandContext(ctx, arguments[0], arguments[1:]...)
			probe.Env = []string{}
			if err := probe.Run(); err != nil || ctx.Err() != nil {
				test.Fatalf("unsafe lock was accepted or refusal blocked: %v %v", err, ctx.Err())
			}
			after, err := os.Lstat(path)
			if err != nil || !os.SameFile(before, after) || before.Mode().Type() != after.Mode().Type() {
				test.Fatalf("unsafe lock path was unlinked/replaced: %v %v", after, err)
			}
			if !bytes.Equal(productionRead(test, target), []byte("target stays exact")) {
				test.Fatal("unsafe lock target was modified")
			}
		})
	}
}
