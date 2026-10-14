// Package appimage reads the files an AppImage bundles: an ELF runtime with the application's
// squashfs image appended right after its section headers.
package appimage

import (
	"debug/elf"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"

	"github.com/CalebQ42/squashfs"
)

// Image is an open AppImage's bundled files.
type Image struct {
	fs.FS
	file *os.File
}

// Open opens the AppImage at path.
func Open(path string) (*Image, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	offset, err := squashfsOffset(f)
	if err != nil {
		_ = f.Close()
		return nil, err
	}
	r, err := squashfs.NewReaderAtOffset(f, offset)
	if err != nil {
		_ = f.Close()
		return nil, fmt.Errorf("open squashfs: %w", err)
	}
	return &Image{FS: r, file: f}, nil
}

func (i *Image) Close() error { return i.file.Close() }

// squashfsOffset is where an AppImage's squashfs image starts: the end of its runtime's section
// header table.
func squashfsOffset(r io.ReaderAt) (int64, error) {
	ident := make([]byte, elf.EI_NIDENT)
	if _, err := r.ReadAt(ident, 0); err != nil || string(ident[:4]) != elf.ELFMAG {
		return 0, errors.New("not an AppImage: no ELF header")
	}
	var order binary.ByteOrder = binary.LittleEndian
	if elf.Data(ident[elf.EI_DATA]) == elf.ELFDATA2MSB {
		order = binary.BigEndian
	}
	sr := io.NewSectionReader(r, 0, 1<<16)
	switch elf.Class(ident[elf.EI_CLASS]) {
	case elf.ELFCLASS64:
		var hdr elf.Header64
		if err := binary.Read(sr, order, &hdr); err != nil {
			return 0, err
		}
		return int64(hdr.Shoff) + int64(hdr.Shentsize)*int64(hdr.Shnum), nil
	case elf.ELFCLASS32:
		var hdr elf.Header32
		if err := binary.Read(sr, order, &hdr); err != nil {
			return 0, err
		}
		return int64(hdr.Shoff) + int64(hdr.Shentsize)*int64(hdr.Shnum), nil
	}
	return 0, errors.New("not an AppImage: unknown ELF class")
}
