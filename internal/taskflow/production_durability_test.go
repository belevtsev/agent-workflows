//go:build darwin || linux

package taskflow

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func productionCanonicalTemp(test *testing.T) string {
	test.Helper()
	root, err := filepath.EvalSymlinks(test.TempDir())
	if err != nil {
		test.Fatal(err)
	}
	return root
}

func productionOpenRoot(test *testing.T, directory string) *os.Root {
	test.Helper()
	root, err := os.OpenRoot(directory)
	if err != nil {
		test.Fatal(err)
	}
	test.Cleanup(func() { root.Close() })
	return root
}

func productionReadNamed(test *testing.T, root *os.Root, name string) []byte {
	test.Helper()
	file, err := root.Open(name)
	if err != nil {
		test.Fatal(err)
	}
	defer file.Close()
	data, err := io.ReadAll(file)
	if err != nil {
		test.Fatal(err)
	}
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 {
		test.Fatalf("named artifact is not complete/private0600: %v %v", info, err)
	}
	return data
}

func productionOnlyNamed(test *testing.T, directory, name string) {
	test.Helper()
	entries, err := os.ReadDir(directory)
	if err != nil || len(entries) != 1 || entries[0].Name() != name {
		test.Fatalf("publication left temporary or unexpected files: %v %v", entries, err)
	}
}

func TestProductionWriteNewUncertainPublication(test *testing.T) {
	directory := productionCanonicalTemp(test)
	root := productionOpenRoot(test, directory)
	name := "record-000001.json"
	data := []byte("private complete bytes\x00\xff\n")
	syncFailure := errors.New("production injected directory sync failure")
	syncCalls := 0
	err := writeNewWithSync(root, name, data, func(publishedRoot *os.Root) error {
		syncCalls++
		if !bytes.Equal(productionReadNamed(test, publishedRoot, name), data) {
			test.Fatal("directory sync ran before exact named publication")
		}
		return syncFailure
	})
	if err == nil || !errors.Is(err, syncFailure) || errors.Is(err, ErrTaskLocked) || syncCalls != 1 {
		test.Fatalf("uncertain publication err=%v, sync calls=%d", err, syncCalls)
	}
	message := strings.ToLower(err.Error())
	if !strings.Contains(err.Error(), filepath.Join(directory, name)) || !strings.Contains(message, "reload") || !strings.Contains(message, "retry") {
		test.Fatalf("uncertain outcome omits absolute artifact/reload-before-retry guidance: %v", err)
	}
	if !strings.Contains(message, "uncertain") && !strings.Contains(message, "durab") && !strings.Contains(message, "may") {
		test.Fatalf("publication uncertainty is not reported: %v", err)
	}
	if !bytes.Equal(productionReadNamed(test, root, name), data) {
		test.Fatal("postpublication error removed or altered published bytes")
	}
	productionOnlyNamed(test, directory, name)
	retrySyncs := 0
	err = writeNewWithSync(root, name, []byte("must not replace uncertain winner"), func(publishedRoot *os.Root) error {
		retrySyncs++
		return nil
	})
	if err == nil || errors.Is(err, ErrTaskLocked) || retrySyncs != 0 || !bytes.Equal(productionReadNamed(test, root, name), data) {
		test.Fatalf("blind retry overwrote/replayed publication: err=%v syncs=%d", err, retrySyncs)
	}
	productionOnlyNamed(test, directory, name)
}

func TestProductionWriteNewSuccessAndExistingDestination(test *testing.T) {
	for _, existing := range []bool{false, true} {
		test.Run(fmt.Sprintf("existing-%t", existing), func(test *testing.T) {
			directory := productionCanonicalTemp(test)
			root := productionOpenRoot(test, directory)
			name := "record-000001.json"
			original := []byte("existing immutable bytes")
			candidate := []byte("new complete bytes\x00\xff")
			if existing {
				productionTaskWrite(test, filepath.Join(directory, name), original)
			}
			syncCalls := 0
			err := writeNewWithSync(root, name, candidate, func(publishedRoot *os.Root) error {
				syncCalls++
				if !bytes.Equal(productionReadNamed(test, publishedRoot, name), candidate) {
					test.Fatal("sync did not observe complete candidate")
				}
				file, err := publishedRoot.Open(".")
				if err != nil {
					return err
				}
				defer file.Close()
				return file.Sync()
			})
			if existing {
				if err == nil || syncCalls != 0 || !bytes.Equal(productionReadNamed(test, root, name), original) {
					test.Fatalf("existing destination overwritten/synced: %v, %d", err, syncCalls)
				}
			} else if err != nil || syncCalls != 1 || !bytes.Equal(productionReadNamed(test, root, name), candidate) {
				test.Fatalf("successful no-replace publication = %v, syncs=%d", err, syncCalls)
			}
			if errors.Is(err, ErrTaskLocked) {
				test.Fatal("artifact refusal was misclassified as task lock contention")
			}
			productionOnlyNamed(test, directory, name)
		})
	}
}

func TestProductionWriteNewConcurrentNoReplace(test *testing.T) {
	directory := productionCanonicalTemp(test)
	root := productionOpenRoot(test, directory)
	name := "record-000001.json"
	start := make(chan struct{})
	results := make(chan error, 4)
	var syncCalls atomic.Int32
	for writer := 0; writer < 4; writer++ {
		candidate := []byte(fmt.Sprintf("complete candidate %d", writer))
		go func() {
			<-start
			results <- writeNewWithSync(root, name, candidate, func(publishedRoot *os.Root) error {
				syncCalls.Add(1)
				file, err := publishedRoot.Open(name)
				if err != nil {
					return err
				}
				defer file.Close()
				actual, err := io.ReadAll(file)
				if err != nil {
					return err
				}
				if !bytes.Equal(actual, candidate) {
					return errors.New("winner's named bytes do not match its candidate")
				}
				return nil
			})
		}()
	}
	close(start)
	winners := 0
	for writer := 0; writer < 4; writer++ {
		var err error
		select {
		case err = <-results:
		case <-time.After(10 * time.Second):
			test.Fatal("concurrent no-replace writer did not settle")
		}
		if err == nil {
			winners++
		} else if errors.Is(err, ErrTaskLocked) {
			test.Fatal("no-replace collision was reported as task lock contention")
		}
	}
	if winners != 1 || syncCalls.Load() != 1 {
		test.Fatalf("winners=%d syncs=%d", winners, syncCalls.Load())
	}
	productionOnlyNamed(test, directory, name)
}

func TestProductionNestedStoreStartReadRemove(test *testing.T) {
	for _, prepareStore := range []bool{false, true} {
		test.Run(fmt.Sprintf("private-store-first-%t", prepareStore), func(test *testing.T) {
			root := test.TempDir()
			canonical, err := filepath.EvalSymlinks(root)
			if err != nil {
				test.Fatal(err)
			}
			repository := filepath.Join(canonical, "repository")
			ancestor := filepath.Join(root, "existing-ancestor")
			if err := os.Mkdir(repository, 0o700); err != nil {
				test.Fatal(err)
			}
			if err := os.Mkdir(ancestor, 0o751); err != nil {
				test.Fatal(err)
			}
			if err := os.Chmod(ancestor, 0o751); err != nil {
				test.Fatal(err)
			}
			source := []byte("source remains unchanged\n")
			productionTaskWrite(test, filepath.Join(repository, "source.txt"), source)
			store := filepath.Join(ancestor, "private one", "private two", "tasks")
			if prepareStore {
				resolved, err := privateStore(store, repository)
				if err != nil || resolved != filepath.Join(canonical, "existing-ancestor", "private one", "private two", "tasks") {
					test.Fatalf("nested privateStore = %q, %v", resolved, err)
				}
			}
			record, err := StartWithPacks(test.Context(), repository, store, productionBasePlan(), fixtureBinding(), SelectionExplicit, nil)
			if err != nil {
				test.Fatal(err)
			}
			for _, directory := range []string{filepath.Join(ancestor, "private one"), filepath.Join(ancestor, "private one", "private two"), store, record.Directory} {
				info, err := os.Stat(directory)
				if err != nil || !info.IsDir() || info.Mode().Perm() != 0o700 {
					test.Fatalf("new directory is not private0700: %s %v %v", directory, info, err)
				}
			}
			loaded, err := Load(record.Directory)
			if err != nil || loaded.ID != record.ID || !reflect.DeepEqual(loaded.Plan, record.Plan) || !reflect.DeepEqual(loaded.Advisers, fixtureBinding()) {
				test.Fatalf("nested task unreadable: %v", err)
			}
			if err := os.RemoveAll(record.Directory); err != nil {
				test.Fatal(err)
			}
			if _, err := Load(record.Directory); err == nil {
				test.Fatal("removed synthetic task remained readable")
			}
			if info, err := os.Stat(ancestor); err != nil || info.Mode().Perm() != 0o751 {
				test.Fatalf("existing parent was chmodded: %v %v", info, err)
			}
			if !bytes.Equal(productionTaskRead(test, filepath.Join(repository, "source.txt")), source) {
				test.Fatal("store lifecycle changed repository fixture")
			}
		})
	}
}

func TestProductionStorePathGuards(test *testing.T) {
	for _, kind := range []string{"symlink-final", "symlink-intermediate", "public-final", "file-final", "inside-repository"} {
		test.Run(kind, func(test *testing.T) {
			root := productionCanonicalTemp(test)
			repository, target := filepath.Join(root, "repository"), filepath.Join(root, "target")
			for _, directory := range []string{repository, target} {
				if err := os.Mkdir(directory, 0o700); err != nil {
					test.Fatal(err)
				}
			}
			productionTaskWrite(test, filepath.Join(repository, "source.txt"), []byte("source\n"))
			productionTaskWrite(test, filepath.Join(target, "sentinel"), []byte("untouched\n"))
			store := filepath.Join(root, "tasks")
			switch kind {
			case "symlink-final", "symlink-intermediate":
				if err := os.Symlink(target, store); err != nil {
					test.Fatal(err)
				}
				if kind == "symlink-intermediate" {
					store = filepath.Join(store, "nested", "tasks")
				}
			case "public-final":
				if err := os.Mkdir(store, 0o755); err != nil {
					test.Fatal(err)
				}
				if err := os.Chmod(store, 0o755); err != nil {
					test.Fatal(err)
				}
			case "file-final":
				productionTaskWrite(test, store, []byte("not a directory"))
			case "inside-repository":
				store = filepath.Join(repository, "tasks")
			}
			targetBefore := productionTaskBytes(test, target)
			repoBefore := productionTaskBytes(test, repository)
			if _, err := privateStore(store, repository); err == nil {
				test.Fatal("privateStore accepted unsafe path")
			}
			if _, err := StartWithPacks(test.Context(), repository, store, productionBasePlan(), fixtureBinding(), SelectionExplicit, nil); err == nil {
				test.Fatal("Start accepted unsafe store path")
			}
			if !reflect.DeepEqual(productionTaskBytes(test, target), targetBefore) || !reflect.DeepEqual(productionTaskBytes(test, repository), repoBefore) {
				test.Fatal("store refusal changed repository or symlink target")
			}
			if kind == "public-final" {
				if info, err := os.Stat(store); err != nil || info.Mode().Perm() != 0o755 {
					test.Fatalf("existing final-store mode changed: %v %v", info, err)
				}
			}
			if kind == "file-final" && !bytes.Equal(productionTaskRead(test, store), []byte("not a directory")) {
				test.Fatal("non-directory store was altered")
			}
		})
	}
}
