// Copyright (c) 2020 Microsoft Corporation, Sean Hinchee.
// Licensed under the MIT License.

// Implements an abstract file system
// TODO - rewrite with the io/fs package if the proposal completes
// ^ See: https://go.googlesource.com/proposal/+/master/design/draft-iofs.md
package main

import (
	"errors"
	"io"
	"log"
	"os"
	"path"
	"strings"
	"time"
)

const (
	maxChildren = 32  // Maxmimum number of children a directory can have
	maxProtoBuf = 256 // Maximum size of the buffer for storing directory contents
	infoBuf     = 10  // Buffer size for file info
)

// Represents a file in the file system
type File struct {
	parent   *File            // Parent directory
	srv      *Server          // Server we run under (could be global?)
	name     string           // Name of the file singleton `/f/a` is `a`
	dir      bool             // Are we a directory?
	last     time.Time        // Last modified time
	*Blob                     // Some kind of contents to the file
	Children []*File          // Our child nodes (if a dirrectory)
	info     chan os.FileInfo // Info channel for Readdir()
}

// Creates a VFile out of a File - See: vfile.go
func (f *File) VF() VFile {
	return VFile{f}
}

// Create a new tree with a stub root directory
func NewTree(srv *Server) *File {
	f := &File{
		srv:      srv,
		name:     "/",
		dir:      true,
		Children: make([]*File, 0, maxChildren),
	}

	return f
}

// Synchronize our tree with Azure remote
func (t *File) Sync() error {
	// TODO - sync up as well?
	// TODO - nested directories handling?
	// TODO - download only files that have changed

	remotes, err := ListBlobs(t.srv)
	if err != nil {
		return err
	}

	locals := make([]string, len(t.Children))
	for i, _ := range t.Children {
		locals[i] = t.Children[i].name
	}

	diff := missingLocally(locals, remotes)

	for _, name := range diff {
		// TODO - nested (and) dir handling
		_, err := t.srv.Insert("/"+name, false)
		if err != nil {
			return errors.New("could not insert remote blobs into fs - " + err.Error())
		}
	}

	return nil
}

// Find a full path within the tree
func (t *File) Search(full string) (*File, error) {
	cleaned := path.Clean(full)
	files := strings.Split(cleaned, "/")

	// Hack over split, drops the / entry, assume we're /
	files = files[1:]

	found := t

	// For every file to search for in the set
Path:
	for _, current := range files {
		for _, child := range t.Children {
			if child.name == current {
				found = child
				continue Path
			}
		}

		return nil, errors.New("could not find file")
	}

	return found, nil
}

// Insert a new child somewhere in the tree ;; returns the Tree root
func (t *File) Insert(full string, isDir bool) (*File, error) {
	var parent *File = t
	var err error = nil
	parentName, name := path.Split(full)
	if parentName == "/" {
		// Short circuit root base case - no search
		goto Root
	}

	parent, err = t.Search(parentName)
	if err != nil {
		return t, errors.New(`could not find parent directory: "` + parentName + `" - ` + err.Error())
	}

Root:

	for _, child := range parent.Children {
		if child.name == name {
			return t, errors.New(`file "` + full + `" exists`)
		}
	}

	f := parent.NewChild(name, isDir)

	// TODO - upload here?
	//f.Blob.Upload(t.srv.ctx)

	return f, nil
}

// Delete a file from somewhere in the tree
func (t *File) Delete(full string) error {
	var parent *File = t
	var err error = nil
	parentName, name := path.Split(full)
	if parentName == "/" {
		// Short circuit root base case - no search
		goto Root
	}

	parent, err = t.Search(parentName)
	if err != nil {
		return errors.New(`could not find parent directory: "` + parentName + `" - ` + err.Error())
	}

	// Find the child of the parent
Root:
	for i, child := range parent.Children {
		if child.name == name {
			// Found the child, cut it from the child slice
			left := parent.Children[:i]
			if i < len(parent.Children)-1 {
				right := parent.Children[i+1:]
				parent.Children = append(left, right...)
			} else {
				parent.Children = left
			}

			return nil
		}
	}

	return errors.New(`could not find child "` + name + `"`)
}

// Create a new File as a child of t
func (t *File) NewChild(name string, isDir bool) *File {
	child := &File{
		parent:   t,
		srv:      t.srv,
		name:     name,
		dir:      isDir,
		Children: make([]*File, 0, maxChildren),
	}

	// Hope this isn't nil :)
	child.Blob = NewBlob(&child.name, t.srv.container)

	t.Children = append(t.Children, child)
	return child
}

// Total number of files in the tree
func (t *File) Len() uint64 {
	var descend func(t *File) uint64

	descend = func(t *File) uint64 {
		size := uint64(1)

		for _, child := range t.Children {
			size += descend(child)
		}

		return size
	}

	return descend(t)
}

/* Interface fulfillment for ReaderAt, WriterAt, Closer, etc. */

// Open file
// UNUSED by styx
// This will never be called
func (f *File) Open() error {
	log.Println("!!!! OPEN")
	return nil
}

// Close file
func (f *File) Close() error {
	// TODO - anything? maybe sync up to azure since we know we're done?
	if f.IsDir() {
		f.reloadInfo()
	}
	log.Println("!!!! CLOSE")
	return nil
}

// Write from a certain offset - not called for directories
func (f *File) WriteAt(p []byte, off int64) (n int, err error) {
	// Sync root
	f.srv.File.Sync()

	log.Println("!!!! WRITEAT off= ", off)

	// TODO - Contents() maybe should have to sync - done above anyways for now
	buf := f.Blob.Contents()

	// Might not be necessary or correct
	if off > int64(len(buf)) {
		return 0, io.EOF
	}

	// Truncate file and write from offset
	// TODO - should this casting be guarded?
	if off < int64(len(buf)) {
		// Truncating might not be the answer if this is intended
		// to be insert rather than overwrite
		f.Blob.body.Truncate(int(off))
	}

	n, err = f.Blob.body.Write(p)
	if err != nil {
		return n, err
	}

	// Upload to blob storage
	err = f.Blob.Upload(f.srv.ctx)
	if err != nil {
		// Undo changes if we fail
		f.Blob.body.Reset()
		f.Blob.body.Write(buf)
		return 0, err
	}

	return
}

// Read from a certain offset - not called for directories
func (f *File) ReadAt(p []byte, offset int64) (n int, err error) {
	// Sync root
	f.srv.File.Sync()

	log.Println("!!!! READAT")

	// TODO - don't download the whole file each time
	f.Blob.Download(f.srv.ctx)

	if f.dir {
		// This will not be called
		// See: Readdir()
	}

	if offset >= f.Size() {
		return 0, io.EOF
	}

	buf := f.Blob.Contents()
	n = copy(p, buf[offset:])

	return n, nil
}

// Is this file a directory?
func (f File) IsDir() bool {
	// Sync root
	f.srv.File.Sync()

	return f.dir
}

// Returns the singleton name of the file `/foo/bar` is `bar`
func (f File) Name() string {
	// Sync root
	f.srv.File.Sync()

	return f.name
}

// Returns the size of the file contents
func (f File) Size() int64 {
	// Sync root
	f.srv.File.Sync()

	log.Println("!!!! SIZE")

	if f.IsDir() {
		// Size is number of children
		// Seems to work
		// Previously: 0
		return int64(len(f.Children))
	}

	// TODO - get this info from azure, not the buffer, for lazy loading
	return int64(len(f.Blob.Contents()))
}

// Returns the permission bits (uint32)
func (f File) Mode() os.FileMode {
	// Sync root
	f.srv.File.Sync()

	// TODO - derive from azure storage and XOR sane defaults?
	if f.IsDir() {
		// We are a directory
		return os.ModeDir | 0777
	}

	// We are a regular file
	return 0777
}

// Returns the time of the last modification of the file
func (f File) ModTime() time.Time {
	// Sync root
	f.srv.File.Sync()

	// TODO - ask blob storage?
	return time.Now()
}

// Returns "the underlying data source"
func (f File) Sys() interface{} {
	// Sync root
	f.srv.File.Sync()

	// TODO?
	return nil
}

// Returns the info that styx wants
func (f File) Stat() os.FileInfo {
	// Sync root
	f.srv.File.Sync()

	return f
}

// Reload the channel for Readdir()
func (f *File) reloadInfo() {
	log.Println("« Reloading info for file: ", f.Name())
	f.info = make(chan os.FileInfo, infoBuf)
	go func() {
		for i := 0; i < len(f.Children); i++ {
			f.info <- f.Children[i]
		}
		close(f.info)
	}()
}

// Styx says we must implement Readdir() or marshal directory information ourselves through ReadAt()
// See: https://pkg.go.dev/aqwari.net/net/styx?tab=doc#Directory
func (f *File) Readdir(n int) ([]os.FileInfo, error) {
	// Sync root
	f.srv.File.Sync()

	// Nothing to list
	if len(f.Children) == 0 {
		return nil, io.EOF
	}

	// If channel is nil, push into channel
	// We will close when done

	if f.info == nil {
		f.reloadInfo()
	}

	var err error
	fi := make([]os.FileInfo, 0, infoBuf)
	for i := 0; i < n; i++ {
		s, ok := <-f.info
		if !ok {
			err = io.EOF
			break
		}
		fi = append(fi, s)
	}
	return fi, err
}
