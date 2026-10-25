package catalogfs

import (
	"archive/zip"
	"bytes"
	"fmt"
	"hash/crc32"
	"io"
	"os"
	"path/filepath"
	"rommeta/internal/catalogchanges"
)

// File is an ordinary file or a bounded seekable ZIP member. Stored videos seek
// in place. A manually deflated member materializes only that member to a
// temporary file, which is removed on Close; memory use stays bounded.
type File struct {
	*os.File
	reader    io.ReadSeeker
	info      os.FileInfo
	temporary string
	publish   func() error
	release   func()
	closed    bool
}

func (f *File) Read(b []byte) (int, error) {
	if f.reader != nil {
		return f.reader.Read(b)
	}
	return f.File.Read(b)
}

// WriteTo must use the member reader, not the embedded archive file.
func (f *File) WriteTo(w io.Writer) (int64, error) { return io.Copy(w, struct{ io.Reader }{f}) }
func (f *File) ReadAt(b []byte, off int64) (int, error) {
	if f.reader != nil {
		if r, ok := f.reader.(io.ReaderAt); ok {
			return r.ReadAt(b, off)
		}
		return 0, fmt.Errorf("member has no random reader")
	}
	return f.File.ReadAt(b, off)
}
func (f *File) Seek(off int64, whence int) (int64, error) {
	if f.reader != nil {
		return f.reader.Seek(off, whence)
	}
	return f.File.Seek(off, whence)
}
func (f *File) Stat() (os.FileInfo, error) {
	if f.info != nil {
		return f.info, nil
	}
	return f.File.Stat()
}
func (f *File) Close() error {
	if f.closed {
		return os.ErrClosed
	}
	f.closed = true
	var err error
	if f.File != nil {
		err = f.File.Close()
	}
	if f.release != nil {
		f.release()
	}
	if err == nil && f.publish != nil {
		err = f.publish()
	}
	if f.temporary != "" {
		e := os.Remove(f.temporary)
		if err == nil && !os.IsNotExist(e) {
			err = e
		}
	}
	return err
}
func Open(name string) (*File, error) {
	l, err := locate(name)
	if err != nil {
		return nil, err
	}
	if !l.packed || l.member == "" || isPack(l.member) {
		f, e := os.Open(name)
		if e != nil {
			return nil, e
		}
		return &File{File: f}, nil
	}
	info, err := Stat(name)
	if err != nil {
		return nil, err
	}
	v, ok := info.(*fileInfo)
	if !ok {
		f, e := os.Open(name)
		if e != nil {
			return nil, e
		}
		return &File{File: f}, nil
	}
	if v.m == nil {
		return nil, fmt.Errorf("cannot open virtual directory: %s", name)
	}
	m := v.m
	f, err := os.Open(filepath.Join(l.game, m.pack))
	if err != nil {
		return nil, err
	}
	actual, err := f.Stat()
	if err != nil || !samePhysical(actual, m.physical) {
		f.Close()
		return nil, fmt.Errorf("ZIP changed while opening %s", name)
	}
	if m.header.Method == zip.Store && m.offset >= 0 {
		if m.header.CompressedSize64 != m.header.UncompressedSize64 {
			f.Close()
			return nil, fmt.Errorf("invalid stored size: %s", name)
		}
		return &File{File: f, reader: io.NewSectionReader(f, m.offset, info.Size()), info: info}, nil
	}
	z, err := zip.NewReader(f, actual.Size())
	if err != nil {
		f.Close()
		return nil, err
	}
	var selected *zip.File
	for _, entry := range z.File {
		if zipName(entry) == m.name {
			selected = entry
			break
		}
	}
	if selected == nil {
		f.Close()
		return nil, fmt.Errorf("ZIP member changed: %s", name)
	}
	if m.header.Method == zip.Store {
		if selected.CompressedSize64 != selected.UncompressedSize64 {
			f.Close()
			return nil, fmt.Errorf("invalid stored size: %s", name)
		}
		offset, err := selected.DataOffset()
		if err != nil {
			f.Close()
			return nil, err
		}
		return &File{File: f, reader: io.NewSectionReader(f, offset, info.Size()), info: info}, nil
	}
	r, err := selected.Open()
	if err != nil {
		f.Close()
		return nil, err
	}
	if info.Size() <= 1<<20 {
		data, e := io.ReadAll(io.LimitReader(r, (1<<20)+1))
		ce := r.Close()
		f.Close()
		if e == nil {
			e = ce
		}
		if e != nil {
			return nil, e
		}
		if int64(len(data)) != info.Size() {
			return nil, fmt.Errorf("ZIP member size changed")
		}
		return &File{reader: bytes.NewReader(data), info: info}, nil
	}
	return cachedMember(name, info, r, f)

}
func ReadFile(name string) ([]byte, error) {
	f, err := Open(name)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if _, ok := info.(*fileInfo); !ok {
		return io.ReadAll(f)
	}
	if info.Size() > MaxMemberSize {
		return nil, fmt.Errorf("oversized ZIP member: %s", name)
	}
	b, err := io.ReadAll(io.LimitReader(f, MaxMemberSize+1))
	if err != nil {
		return nil, err
	}
	v := info.(*fileInfo)
	if int64(len(b)) != info.Size() || crc32.ChecksumIEEE(b) != v.m.header.CRC32 {
		return nil, fmt.Errorf("ZIP CRC/size mismatch: %s", name)
	}
	return b, nil
}
func OpenFile(name string, flags int, mode os.FileMode) (*File, error) {
	if flags&(os.O_WRONLY|os.O_RDWR) == 0 {
		return Open(name)
	}
	if err := catalogchanges.Touch(name); err != nil {
		return nil, err
	}
	l, err := locate(name)
	if err != nil {
		return nil, err
	}
	if !l.packed || l.member == "" || isPack(l.member) {
		f, e := os.OpenFile(name, flags, mode)
		if e != nil {
			return nil, e
		}
		return &File{File: f}, nil
	}
	before, err := Stat(name)
	exists := err == nil
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	if exists && flags&os.O_EXCL != 0 {
		return nil, &os.PathError{Op: "open", Path: name, Err: os.ErrExist}
	}
	if !exists && flags&os.O_CREATE == 0 {
		return nil, &os.PathError{Op: "open", Path: name, Err: os.ErrNotExist}
	}
	if err = os.MkdirAll(l.game, 0755); err != nil {
		return nil, err
	}
	tmp, err := os.CreateTemp(l.game, "rommeta-write-*.tmp")
	if err != nil {
		return nil, err
	}
	cleanup := func(e error) (*File, error) { tmp.Close(); os.Remove(tmp.Name()); return nil, e }
	if exists && flags&os.O_TRUNC == 0 {
		in, e := Open(name)
		if e != nil {
			return cleanup(e)
		}
		_, e = io.Copy(tmp, in)
		in.Close()
		if e != nil {
			return cleanup(e)
		}
	}
	if flags&os.O_APPEND == 0 {
		if _, err = tmp.Seek(0, io.SeekStart); err != nil {
			return cleanup(err)
		}
	}
	result := &File{File: tmp, temporary: tmp.Name()}
	result.publish = func() error {
		now, e := Stat(name)
		if exists {
			if e != nil || !SameFile(before, now) {
				return fmt.Errorf("catalog changed before close: %s", name)
			}
		} else if e == nil || !os.IsNotExist(e) {
			return fmt.Errorf("catalog destination appeared: %s", name)
		}
		return PutFiles(l.game, map[string]string{l.member: tmp.Name()}, nil)
	}
	return result, nil
}
