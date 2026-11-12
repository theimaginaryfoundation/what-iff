package xlsx

import (
	"path"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/theimaginaryfoundation/what-iff/internal/officecomments"
)

const (
	nsThreaded       = "http://schemas.microsoft.com/office/spreadsheetml/2018/threadedcomments"
	nsMain           = "http://schemas.openxmlformats.org/spreadsheetml/2006/main"
	relThreaded      = "http://schemas.microsoft.com/office/2017/10/relationships/threadedComment"
	relPerson        = "http://schemas.microsoft.com/office/2017/10/relationships/person"
	ctThreaded       = "application/vnd.ms-excel.threadedcomments+xml"
	ctPerson         = "application/vnd.ms-excel.person+xml"
	fallbackAuthor   = "WhatIff user"
	unknownAuthor    = "Unknown"
	worksheetRelType = "/worksheet"
)

// book is a workbook's sheets and people.
type book struct {
	p           *pkg
	wbPath      string
	sheets      []sheet
	personsPath string
	persons     []person
}

type sheet struct {
	name, path string
}

type person struct {
	id, name, userID, provider string
}

func load(data []byte) (*book, error) {
	p, err := openPkg(data)
	if err != nil {
		return nil, err
	}
	b := &book{p: p, wbPath: p.relOf("", relOfficeDoc)}
	if b.wbPath == "" {
		b.wbPath = "xl/workbook.xml"
	}
	wb, ok := p.read(b.wbPath)
	if !ok {
		return nil, officecomments.ErrNotOffice
	}
	root, err := scan(wb, 2)
	if err != nil || root.name.Local != "workbook" {
		return nil, officecomments.ErrNotOffice
	}
	rels := p.rels(b.wbPath)
	if sheets := root.kid("sheets"); sheets != nil {
		for _, s := range sheets.kids("sheet") {
			for _, r := range rels {
				if r.id == s.relID() && strings.HasSuffix(r.typ, worksheetRelType) && !r.external {
					b.sheets = append(b.sheets, sheet{name: s.get("name"), path: resolve(b.wbPath, r.target)})
				}
			}
		}
	}
	b.personsPath = p.relOf(b.wbPath, relPerson)
	if data, ok := p.read(b.personsPath); ok && b.personsPath != "" {
		if root, err := scan(data, 1); err == nil {
			for _, n := range root.kids("person") {
				if id := n.get("id"); id != "" {
					b.persons = append(b.persons, person{id: id, name: n.get("displayName"), userID: n.get("userId"), provider: n.get("providerId")})
				}
			}
		}
	}
	return b, nil
}

func (b *book) personByID(id string) *person {
	for i := range b.persons {
		if strings.EqualFold(b.persons[i].id, id) {
			return &b.persons[i]
		}
	}
	return nil
}

// author is the panel's id and name for a comment's person, as the browser reads them.
func (b *book) author(personID string) (string, string) {
	pr := b.personByID(personID)
	if pr == nil {
		return officecomments.PersonID("", "", unknownAuthor), unknownAuthor
	}
	name := pr.name
	if name == "" && !(pr.provider == officecomments.WhatIffProvider && pr.userID != "") {
		name = unknownAuthor
	}
	return officecomments.PersonID(pr.provider, pr.userID, name), name
}

// personFor is the person id a new comment is written with, adding the person when the file has
// none for its author. A WhatIff user or agent is found by user id, anyone else by name.
func (b *book) personFor(c officecomments.Comment) string {
	name := c.AuthorName
	uid := officecomments.ProviderUserID(c.AuthorID)
	var np person
	if uid != "" {
		for _, pr := range b.persons {
			if pr.provider == officecomments.WhatIffProvider && pr.userID == uid {
				return pr.id
			}
		}
		if name == "" {
			name = fallbackAuthor
		}
		np = person{id: newGUID(), name: name, userID: uid, provider: officecomments.WhatIffProvider}
	} else {
		if name == "" {
			name = strings.TrimPrefix(c.AuthorID, "author:")
		}
		if name == "" {
			name = fallbackAuthor
		}
		for _, pr := range b.persons {
			if pr.name == name && pr.provider != officecomments.WhatIffProvider {
				return pr.id
			}
		}
		np = person{id: newGUID(), name: name, userID: name, provider: "None"}
	}
	b.addPerson(np)
	return np.id
}

func personXML(pfx string, pr person) string {
	return `<` + pfx + `person displayName="` + escapeXML(pr.name) + `" id="` + pr.id + `" userId="` + escapeXML(pr.userID) + `" providerId="` + escapeXML(pr.provider) + `"/>`
}

func (b *book) addPerson(pr person) {
	b.persons = append(b.persons, pr)
	if data, ok := b.p.read(b.personsPath); ok && b.personsPath != "" {
		if root, err := scan(data, 0); err == nil {
			b.p.put(b.personsPath, apply(data, []edit{appendChild(data, root, personXML(prefix(data, root), pr))}))
			return
		}
	}
	// The browser's persons part, at its path beside the workbook.
	b.personsPath = path.Join(path.Dir(b.wbPath), "persons/person.xml")
	if b.p.has(b.personsPath) {
		b.personsPath = b.p.unused(path.Join(path.Dir(b.wbPath), "persons/person%d.xml"))
	}
	var body strings.Builder
	for _, pr := range b.persons {
		body.WriteString(personXML("", pr))
	}
	b.p.put(b.personsPath, []byte(xmlHead+`<personList xmlns="`+nsThreaded+`" xmlns:x="`+nsMain+`">`+body.String()+`</personList>`))
	b.p.addOverride(b.personsPath, ctPerson)
	b.p.addRel(b.wbPath, relPerson, b.personsPath)
}

// sheetNamed finds a sheet by name (any case); an empty name is the first sheet.
func (b *book) sheetNamed(name string) (sheet, bool) {
	if len(b.sheets) == 0 {
		return sheet{}, false
	}
	if name == "" {
		return b.sheets[0], true
	}
	for _, s := range b.sheets {
		if s.name == name {
			return s, true
		}
	}
	for _, s := range b.sheets {
		if strings.EqualFold(s.name, name) {
			return s, true
		}
	}
	return sheet{}, false
}

// tc is one threadedComment element.
type tc struct {
	n                             *node
	id, ref, parent, personID, dT string
	text                          string
	done                          bool
}

// threaded is a sheet's threadedComments part.
type threaded struct {
	path string
	data []byte
	root *node
	all  []*tc
}

func (b *book) threaded(s sheet) *threaded {
	t := &threaded{path: b.p.relOf(s.path, relThreaded)}
	if t.path == "" {
		return t
	}
	data, ok := b.p.read(t.path)
	if !ok {
		return t
	}
	root, err := scan(data, 2)
	if err != nil {
		return t
	}
	t.data, t.root = data, root
	for _, n := range root.kids("threadedComment") {
		c := &tc{n: n, id: n.get("id"), ref: n.get("ref"), parent: n.get("parentId"), personID: n.get("personId"), dT: n.get("dT"), done: n.get("done") == "1"}
		if text := n.kid("text"); text != nil {
			c.text = cleanText(text.text)
		}
		if c.id != "" && c.ref != "" {
			t.all = append(t.all, c)
		}
	}
	return t
}

// thread is a root comment with its replies, in file order.
type thread struct {
	root     *tc
	comments []*tc
	row, col int
}

// threads are the part's threads on a real cell, as the browser reads them.
func (t *threaded) threads() []*thread {
	var out []*thread
	byID := map[string]*thread{}
	for _, c := range t.all {
		if c.parent != "" {
			continue
		}
		row, col, ok := parseCell(c.ref)
		if !ok {
			continue
		}
		th := &thread{root: c, comments: []*tc{c}, row: row, col: col}
		out = append(out, th)
		byID[c.id] = th
	}
	for _, c := range t.all {
		if th := byID[c.parent]; c.parent != "" && th != nil {
			th.comments = append(th.comments, c)
		}
	}
	return out
}

func (t *threaded) used(id string) bool {
	for _, c := range t.all {
		if strings.EqualFold(c.id, id) {
			return true
		}
	}
	return false
}

// cleanText is a comment's text as the browser's editor holds it.
func cleanText(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	return strings.NewReplacer("\u001e", "", "\u001f", "").Replace(s)
}

var guidRe = regexp.MustCompile(`^\{[0-9A-Fa-f]{8}-[0-9A-Fa-f]{4}-[0-9A-Fa-f]{4}-[0-9A-Fa-f]{4}-[0-9A-Fa-f]{12}\}$`)

// newGUID is an id as the browser makes them: an upper-case UUID in braces.
func newGUID() string {
	return "{" + strings.ToUpper(uuid.NewString()) + "}"
}

// excelDate is a dT: the time with no zone and hundredths of a second (UTC here).
func excelDate(t time.Time) string {
	if t.IsZero() {
		t = time.Now()
	}
	t = t.UTC()
	return t.Format("2006-01-02T15:04:05") + "." + twoDigits(t.Nanosecond()/1e7)
}

func twoDigits(n int) string {
	if n < 10 {
		return "0" + strconv.Itoa(n)
	}
	return strconv.Itoa(n)
}

// parseDate reads a dT; with no zone it is taken as UTC.
func parseDate(s string) time.Time {
	for _, layout := range []string{"2006-01-02T15:04:05Z07:00", "2006-01-02T15:04:05"} {
		if t, err := time.Parse(layout, s); err == nil {
			return t.UTC()
		}
	}
	return time.Unix(0, 0).UTC()
}

var cellRe = regexp.MustCompile(`^\$?([A-Za-z]{1,3})\$?(\d+)$`)

// parseCell reads an A1 reference to a zero-based row and column inside Excel's grid.
func parseCell(ref string) (int, int, bool) {
	m := cellRe.FindStringSubmatch(strings.TrimSpace(ref))
	if m == nil {
		return 0, 0, false
	}
	col := 0
	for _, ch := range strings.ToUpper(m[1]) {
		col = col*26 + int(ch-'A'+1)
	}
	row, err := strconv.Atoi(m[2])
	if err != nil || row < 1 || row > 1048576 || col > 16384 {
		return 0, 0, false
	}
	return row - 1, col - 1, true
}

func columnLetters(col int) string {
	n := col + 1
	out := ""
	for n > 0 {
		rem := (n - 1) % 26
		out = string(rune('A'+rem)) + out
		n = (n - 1) / 26
	}
	return out
}

func cellA1(row, col int) string {
	return columnLetters(col) + strconv.Itoa(row+1)
}

var plainSheetName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_.]*$`)

// sheetRef is "Sheet1!B3", quoting a sheet name as a formula would.
func sheetRef(name string, row, col int) string {
	if !plainSheetName.MatchString(name) || cellRe.MatchString(name) {
		name = "'" + strings.ReplaceAll(name, "'", "''") + "'"
	}
	return name + "!" + cellA1(row, col)
}
