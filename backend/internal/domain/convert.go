package domain

import (
	"errors"
	"fmt"
	"path"
	"strings"
)

// Conversion is a stored format a System's emulator can't use as placed, and the format to ask
// the Server to convert it to.
type Conversion struct {
	// From and To are extensions, lowercase and without the dot.
	From, To string
	// Note says what the user has to do with a file left in From, with %s for the emulator.
	Note string
}

// ServedFile is a game file as the Server serves it in a requested format.
type ServedFile struct {
	Name string
	Size int64
}

// ErrNoConversion means the Server won't convert a file: it predates conversion, has it turned
// off, or can't convert that file.
var ErrNoConversion = errors.New("the server can't convert this file")

// ConversionFor returns the conversion a file named name needs in s, if any.
func (s System) ConversionFor(name string) (Conversion, bool) {
	ext := strings.ToLower(strings.TrimPrefix(path.Ext(name), "."))
	for _, c := range s.Conversions {
		if c.From == ext {
			return c, true
		}
	}
	return Conversion{}, false
}

// InstallNote says what the user has to do before a placed file plays in s's emulator on a CPU of
// arch, empty when it plays as it is.
func (s System) InstallNote(fileName, arch string) string {
	c, ok := s.ConversionFor(fileName)
	if !ok {
		return ""
	}
	name := s.DefaultEmulator(arch)
	if e, ok := EmulatorByID(name); ok {
		name = e.Name
	}
	return fmt.Sprintf(c.Note, name)
}
