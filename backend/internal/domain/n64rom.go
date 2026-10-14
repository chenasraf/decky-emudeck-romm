package domain

import (
	"bytes"
	"crypto/md5" //nolint:gosec // mupen64plus names saves after the ROM's MD5, not for security
	"encoding/binary"
	"encoding/hex"
	"errors"
	"strings"
)

// N64ROM is what mupen64plus reads from a ROM to name its saves.
type N64ROM struct {
	// MD5 is of the ROM in .z64 byte order, as uppercase hex.
	MD5 string
	// Header is the name in the ROM's header, raw bytes trimmed of spaces at both ends: Japanese
	// headers hold Shift-JIS.
	Header     []byte
	CRC1, CRC2 uint32
}

// ParseN64ROM reads a ROM in any of the three byte orders dumps come in, told apart by its first
// four bytes as mupen64plus does.
func ParseN64ROM(data []byte) (N64ROM, error) {
	if len(data) < 0x40 {
		return N64ROM{}, errors.New("not an N64 ROM")
	}
	z64 := data
	switch {
	case bytes.Equal(data[:4], []byte{0x80, 0x37, 0x12, 0x40}):
	case bytes.Equal(data[:4], []byte{0x37, 0x80, 0x40, 0x12}):
		// .v64: each 16-bit half-word swapped.
		z64 = make([]byte, len(data))
		for i := 0; i+2 <= len(data); i += 2 {
			z64[i], z64[i+1] = data[i+1], data[i]
		}
	case bytes.Equal(data[:4], []byte{0x40, 0x12, 0x37, 0x80}):
		// .n64: each 32-bit word reversed.
		z64 = bytes.Clone(data)
		swap32(z64)
	default:
		return N64ROM{}, errors.New("not an N64 ROM")
	}
	sum := md5.Sum(z64) //nolint:gosec // see import
	header := z64[0x20:0x34]
	if i := bytes.IndexByte(header, 0); i >= 0 {
		header = header[:i]
	}
	return N64ROM{
		MD5:    strings.ToUpper(hex.EncodeToString(sum[:])),
		Header: bytes.TrimSpace(bytes.Clone(header)),
		CRC1:   binary.BigEndian.Uint32(z64[0x10:]),
		CRC2:   binary.BigEndian.Uint32(z64[0x14:]),
	}, nil
}

// N64SaveName is the name, before the extension, mupen64plus gives a ROM's saves with
// SaveFilenameFormat 1, as RMG has it: the first 32 bytes of goodName and the first 8 of the MD5,
// the header name in its place when the ROM database lacks the ROM.
func N64SaveName(rom N64ROM, goodName string, known bool) string {
	var name string
	switch {
	case known:
		name = goodName
		if len(name) > 32 {
			name = name[:32]
		}
		name += "-" + rom.MD5[:8]
	case len(rom.Header) > 0:
		name = string(rom.Header) + "-" + rom.MD5[:8]
	default:
		name = "unknown-" + rom.MD5[:8]
	}
	// Byte by byte: a header name's Shift-JIS bytes aren't UTF-8, and must come through as they are.
	out := []byte(name)
	for i, b := range out {
		if strings.IndexByte(`:<>"/\|?*`, b) >= 0 {
			out[i] = '_'
		}
	}
	return string(out)
}
