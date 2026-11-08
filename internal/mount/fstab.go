package mount

import (
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
)

var uuidPattern = regexp.MustCompile("(?i)^[a-f0-9]{8}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{12}$")

const FstabOptions = "rw,nobrowse,owners"
const DiskutilOptions = "owners"

type Entry struct {
	UUID string `json:"uuid"`
	Path string `json:"path"`
}

func (e Entry) Validate() error {
	if !uuidPattern.MatchString(e.UUID) {
		return errors.New("expected APFS volume UUID")
	}
	if !filepath.IsAbs(e.Path) || filepath.Clean(e.Path) != e.Path || e.Path == "/" || strings.ContainsAny(e.Path, "\r\n\x00") {
		return errors.New("invalid absolute mount path")
	}
	return nil
}

func escape(s string) string {
	return strings.NewReplacer("\\", "\\134", " ", "\\040", "\t", "\\011", "#", "\\043").Replace(s)
}

func Merge(content []byte, e Entry) ([]byte, bool, error) {
	if err := e.Validate(); err != nil {
		return nil, false, err
	}
	spec := "UUID=" + strings.ToUpper(e.UUID)
	mp := escape(e.Path)
	found := false
	for _, line := range strings.Split(string(content), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 4 {
			return nil, false, fmt.Errorf("malformed existing fstab record %q", line)
		}
		sameUUID := strings.EqualFold(fields[0], spec)
		samePath := fields[1] == mp
		if !sameUUID && !samePath {
			continue
		}
		if !sameUUID || !samePath || len(fields) != 6 || fields[2] != "apfs" || fields[3] != FstabOptions || fields[4] != "0" || fields[5] != "0" {
			return nil, false, fmt.Errorf("conflicting fstab entry %q", line)
		}
		if found {
			return nil, false, errors.New("duplicate fstab entries")
		}
		found = true
	}
	if found {
		return content, false, nil
	}
	next := append([]byte(nil), content...)
	if len(next) > 0 && next[len(next)-1] != '\n' {
		next = append(next, '\n')
	}
	next = append(next, []byte(spec+" "+mp+" apfs "+FstabOptions+" 0 0\n")...)
	return next, true, nil
}
