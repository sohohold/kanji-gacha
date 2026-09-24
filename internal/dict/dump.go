package dict

import (
	"archive/tar"
	"bufio"
	"bytes"
	"compress/gzip"
	"fmt"
	"io"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"
)

// dump holds the glyphs of a GlyphWiki dump (dump.tar.gz): the newest data of
// every glyph, and the older versions that references pin with "@".
type dump struct {
	archive  []byte
	newest   map[string]string
	versions map[string]string
}

const (
	newestFile   = "dump_newest_only.txt"
	versionsFile = "dump_all_versions.txt"
)

func readDump(archive []byte) (*dump, error) {
	d := &dump{archive: archive, newest: map[string]string{}, versions: map[string]string{}}
	err := d.scan(newestFile, func(name, data string) bool {
		d.newest[name] = data
		return true
	})
	if err == nil && len(d.newest) == 0 {
		err = fmt.Errorf("%s: no glyphs", newestFile)
	}
	return d, err
}

// fetchVersions reads the pinned versions named in want from the file of all
// versions, and returns the names it did not find.
func (d *dump) fetchVersions(want map[string]bool) ([]string, error) {
	left := len(want)
	err := d.scan(versionsFile, func(name, data string) bool {
		if want[name] {
			if _, ok := d.versions[name]; !ok {
				d.versions[name] = data
				left--
			}
		}
		return left > 0
	})
	var missing []string
	for n := range want {
		if _, ok := d.versions[n]; !ok {
			missing = append(missing, n)
		}
	}
	return missing, err
}

// scan calls f for every glyph line of one file in the archive until f
// returns false. Lines are "name | related | data" after two header lines;
// names in the file of all versions escape "@" as "\@".
func (d *dump) scan(file string, f func(name, data string) bool) error {
	z, e := gzip.NewReader(bytes.NewReader(d.archive))
	if e != nil {
		return e
	}
	defer z.Close()
	tr := tar.NewReader(z)
	for {
		h, e := tr.Next()
		if e == io.EOF {
			return fmt.Errorf("%s not in archive", file)
		}
		if e != nil {
			return e
		}
		if h.Name != file && !strings.HasSuffix(h.Name, "/"+file) {
			continue
		}
		sc := bufio.NewScanner(tr)
		sc.Buffer(make([]byte, 1<<20), 1<<24)
		for sc.Scan() {
			line := sc.Text()
			i := strings.IndexByte(line, '|')
			j := strings.LastIndexByte(line, '|')
			if i < 0 || i == j {
				continue
			}
			name := strings.ReplaceAll(strings.TrimSpace(strings.TrimPrefix(line[:i], "\ufeff")), `\@`, "@")
			if name == "name" || name == "" || strings.HasPrefix(name, "-") {
				continue
			}
			if !f(name, strings.TrimSpace(line[j+1:])) {
				return nil
			}
		}
		return sc.Err()
	}
}

// data returns a glyph's KAGE data: the pinned version for "name@n", the
// newest otherwise.
func (d *dump) data(name string) (string, bool) {
	if strings.Contains(name, "@") {
		s, ok := d.versions[name]
		return s, ok
	}
	s, ok := d.newest[name]
	return s, ok
}
func (d *dump) lookup(name string) string {
	s, _ := d.data(name)
	return s
}

// ref is one component reference line (99) of KAGE data.
type ref struct {
	line string
	name string
}

// lines splits KAGE data into its component references and other lines.
func lines(data string) (refs []ref, other []string) {
	if data == "" {
		return nil, nil
	}
	for _, l := range strings.Split(data, "$") {
		cols := strings.Split(l, ":")
		if n, e := strconv.ParseFloat(strings.TrimSpace(cols[0]), 64); e == nil && int(n) == 99 && len(cols) > 7 {
			refs = append(refs, ref{l, cols[7]})
		} else {
			other = append(other, l)
		}
	}
	return refs, other
}

var aliasLine = regexp.MustCompile(`^99:0:0:0:0:200:200:([^:$]+)(?::0)*$`)

// resolve follows aliases (a glyph whose data is one reference to another
// glyph over the whole frame) and returns the glyph that has the drawing.
func (d *dump) resolve(name string) string {
	for range 8 {
		data, ok := d.data(name)
		if !ok {
			return name
		}
		m := aliasLine.FindStringSubmatch(data)
		if m == nil {
			return name
		}
		name = m[1]
	}
	return name
}

// closure adds the glyph and every glyph it references to seen, and returns
// the referenced names that have no data.
func (d *dump) closure(name string, seen map[string]bool) (missing []string) {
	if seen[name] {
		return nil
	}
	seen[name] = true
	data, ok := d.data(name)
	if !ok {
		return []string{name}
	}
	refs, _ := lines(data)
	for _, r := range refs {
		missing = append(missing, d.closure(r.name, seen)...)
	}
	return missing
}

var glyphName = regexp.MustCompile(`^u([0-9a-f]{4,6})((?:-[0-9a-z]+)*)$`)

// charOf is the character a glyph name stands for ("u9752-02" is 青),
// with compatibility ideographs mapped to their unified forms (ufa66 is 辶),
// or "" for names that do not start with a code point.
func charOf(name string) string {
	if i := strings.IndexByte(name, '@'); i >= 0 {
		name = name[:i]
	}
	m := glyphName.FindStringSubmatch(name)
	if m == nil {
		return ""
	}
	cp, _ := strconv.ParseUint(m[1], 16, 32)
	r := rune(cp)
	if !utf8.ValidRune(r) {
		return ""
	}
	return norm.NFC.String(string(r))
}

// nameOf is the base glyph name of a character: "u9752".
func nameOf(c string) string {
	r, _ := utf8.DecodeRuneInString(c)
	return fmt.Sprintf("u%04x", r)
}
