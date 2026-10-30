package storagesample

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"testing"
)

var metadataTestFault = errors.New("owned fixture detail must not escape")

type metadataTestEntry struct {
	name string
	mode os.FileMode
}

func (e *metadataTestEntry) Name() string               { return e.name }
func (e *metadataTestEntry) IsDir() bool                { return e.mode.IsDir() }
func (e *metadataTestEntry) Type() os.FileMode          { return e.mode }
func (e *metadataTestEntry) Info() (os.FileInfo, error) { return nil, metadataTestFault }

type metadataTestObject struct {
	info       os.FileInfo
	data       []byte
	linkTarget string
	entries    []os.DirEntry
}
type metadataTestOwner struct {
	fixture        *metadataTestFixture
	path, kind     string
	object         *metadataTestObject
	info           os.FileInfo
	offset, closes int
}

func (h *metadataTestOwner) Fd() uintptr { return 0 }
func (h *metadataTestOwner) Stat() (os.FileInfo, error) {
	if h.fixture.call(h.kind+"-stat", h.path) {
		return nil, metadataTestFault
	}
	if info := h.fixture.statOverride[h.kind+":"+h.path]; info != nil {
		return info, nil
	}
	return h.info, nil
}
func (h *metadataTestOwner) Close() error {
	h.closes++
	h.fixture.closed = append(h.fixture.closed, h.kind+":"+h.path)
	if h.fixture.call("close-"+h.kind, h.path) {
		return metadataTestFault
	}
	return nil
}
func (h *metadataTestOwner) Read(p []byte) (int, error) {
	if h.fixture.call("read", h.path) {
		return 0, metadataTestFault
	}
	if h.offset == len(h.object.data) {
		return 0, io.EOF
	}
	n := copy(p, h.object.data[h.offset:])
	h.offset += n
	return n, nil
}
func (h *metadataTestOwner) ReadDir(n int) ([]os.DirEntry, error) {
	if h.fixture.call("readdir", h.path) {
		return nil, metadataTestFault
	}
	if h.offset == len(h.object.entries) {
		return nil, io.EOF
	}
	end := min(len(h.object.entries), h.offset+n)
	rows := h.object.entries[h.offset:end]
	h.offset = end
	return rows, nil
}

type metadataTestRoot struct {
	fixture *metadataTestFixture
	name    string
	closes  int
}

func (r *metadataTestRoot) Close() error {
	r.closes++
	r.fixture.closed = append(r.fixture.closed, "root:"+r.name)
	if r.fixture.call("close-root", r.name) {
		return metadataTestFault
	}
	return nil
}
func (r *metadataTestRoot) Lstat(path string) (os.FileInfo, error) {
	key := r.name + "/" + path
	if r.fixture.call("lstat", key) {
		return nil, metadataTestFault
	}
	object := r.fixture.objects[key]
	if object == nil {
		return nil, os.ErrNotExist
	}
	return object.info, nil
}
func (r *metadataTestRoot) Readlink(path string) (string, error) {
	key := r.name + "/" + path
	if r.fixture.call("readlink", key) {
		return "", metadataTestFault
	}
	object := r.fixture.objects[key]
	if object == nil {
		return "", os.ErrNotExist
	}
	if object.linkTarget == "" {
		return "", metadataTestFault
	}
	return object.linkTarget, nil
}
func (r *metadataTestRoot) OpenPath(path string) (storageMetadataPath, error) {
	key := r.name + "/" + path
	object := r.fixture.objects[key]
	if object == nil {
		return nil, os.ErrNotExist
	}
	h := &metadataTestOwner{fixture: r.fixture, path: key, kind: "path", object: object, info: object.info}
	r.fixture.owners = append(r.fixture.owners, h)
	if r.fixture.call("open-path", key) {
		return h, metadataTestFault
	}
	return h, nil
}
func (r *metadataTestRoot) OpenReadable(path storageMetadataPath) (storageMetadataFile, error) {
	p := path.(*metadataTestOwner)
	h := &metadataTestOwner{fixture: r.fixture, path: p.path, kind: "file", object: p.object, info: p.info}
	r.fixture.owners = append(r.fixture.owners, h)
	if r.fixture.call("open-readable", p.path) {
		return h, metadataTestFault
	}
	return h, nil
}

type metadataTestFixture struct {
	t            *testing.T
	directory    string
	next         int
	objects      map[string]*metadataTestObject
	fail         map[string]bool
	statOverride map[string]os.FileInfo
	calls        map[string]int
	hook         func(string, string)
	roots        []*metadataTestRoot
	owners       []*metadataTestOwner
	closed       []string
}

func (f *metadataTestFixture) call(stage, path string) bool {
	f.calls[stage+":"+path]++
	if f.hook != nil {
		f.hook(stage, path)
	}
	return f.fail[stage+":"+path]
}
func (f *metadataTestFixture) info(directory bool) os.FileInfo {
	f.t.Helper()
	f.next++
	path := filepath.Join(f.directory, strconv.Itoa(f.next))
	var err error
	if directory {
		err = os.Mkdir(path, 0700)
	} else {
		err = os.WriteFile(path, []byte("fixture\n"), 0600)
	}
	if err != nil {
		f.t.Fatal("owned metadata identity fixture setup failed")
	}
	info, err := os.Stat(path)
	if err != nil {
		f.t.Fatal("owned metadata identity fixture stat failed")
	}
	return info
}
func (f *metadataTestFixture) file(path, data string) {
	f.objects[path] = &metadataTestObject{info: f.info(false), data: []byte(data)}
}
func (f *metadataTestFixture) dir(path string, names ...string) {
	entries := []os.DirEntry{}
	for _, name := range names {
		entries = append(entries, &metadataTestEntry{name, os.ModeSymlink})
	}
	f.objects[path] = &metadataTestObject{info: f.info(true), entries: entries}
}
func (f *metadataTestFixture) node(name, number string) {
	base := "sysfs/class/block/" + name + "/"
	f.dir(base + ".")
	for field, value := range map[string]string{"dev": number + "\n", "size": "16777216\n", "ro": "0\n", "removable": "0\n", "queue/logical_block_size": "512\n"} {
		f.file(base+field, value)
	}
	f.dir(base + "holders")
	f.dir(base + "slaves")
}
func (f *metadataTestFixture) operations() storageMetadataOperations {
	return storageMetadataOperations{openRoot: func(name string) (storageMetadataRoot, error) {
		r := &metadataTestRoot{fixture: f, name: name}
		f.roots = append(f.roots, r)
		if f.call("open-root", name) {
			return r, metadataTestFault
		}
		return r, nil
	}}
}
func (f *metadataTestFixture) allClosed() {
	f.t.Helper()
	for _, root := range f.roots {
		if root.closes != 1 {
			f.t.Fatal("root owner not closed exactly once")
		}
	}
	for _, owner := range f.owners {
		if owner.closes != 1 {
			f.t.Fatal("metadata owner not closed exactly once")
		}
	}
}
func newMetadataTestFixture(t *testing.T) *metadataTestFixture {
	t.Helper()
	f := &metadataTestFixture{t: t, directory: t.TempDir(), objects: map[string]*metadataTestObject{}, fail: map[string]bool{},
		statOverride: map[string]os.FileInfo{}, calls: map[string]int{}}
	f.dir("sysfs/class/block", "disk")
	f.node("disk", "8:0")
	f.file("proc/self/mountinfo", "1 0 0:1 / / rw - tmpfs tmpfs rw\n")
	f.file("proc/swaps", "Filename Type Size Used Priority\n")
	return f
}

type metadataTestContext struct {
	context.Context
	observe func()
}

func (c *metadataTestContext) Err() error {
	if c.observe != nil {
		c.observe()
	}
	return c.Context.Err()
}

type metadataReadFunc func([]byte) (int, error)

func (f metadataReadFunc) Read(p []byte) (int, error) { return f(p) }

type metadataNamesFile struct {
	read func(int) ([]os.DirEntry, error)
}

func (f *metadataNamesFile) ReadDir(n int) ([]os.DirEntry, error) { return f.read(n) }
func (f *metadataNamesFile) Read([]byte) (int, error)             { return 0, metadataTestFault }
func (f *metadataNamesFile) Stat() (os.FileInfo, error)           { return nil, metadataTestFault }
func (f *metadataNamesFile) Close() error                         { return metadataTestFault }
