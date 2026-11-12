package docx

import (
	"strings"
	"time"

	"github.com/theimaginaryfoundation/what-iff/internal/officecomments"
)

// commentPart is one of the parts Word keeps comments in, as docx-comments.ts COMMENT_PARTS.
type commentPart struct {
	key, rel, file, contentType string
}

const ctPrefix = "application/vnd.openxmlformats-officedocument.wordprocessingml"

var commentParts = []commentPart{
	{"comments", "http://schemas.openxmlformats.org/officeDocument/2006/relationships/comments", "comments.xml", ctPrefix + ".comments+xml"},
	{"extended", "http://schemas.microsoft.com/office/2011/relationships/commentsExtended", "commentsExtended.xml", ctPrefix + ".commentsExtended+xml"},
	{"ids", "http://schemas.microsoft.com/office/2016/09/relationships/commentsIds", "commentsIds.xml", ctPrefix + ".commentsIds+xml"},
	{"extensible", "http://schemas.microsoft.com/office/2018/08/relationships/commentsExtensible", "commentsExtensible.xml", ctPrefix + ".commentsExtensible+xml"},
	{"people", "http://schemas.microsoft.com/office/2011/relationships/people", "people.xml", ctPrefix + ".people+xml"},
}

const xmlHead = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>`

// emptyPart is a part as the browser starts it, with no entries yet.
func emptyPart(key string) string {
	mc := `xmlns:mc="` + nsMC + `"`
	switch key {
	case "comments":
		return xmlHead + `<w:comments xmlns:w="` + nsW + `" xmlns:w14="` + nsW14 + `" xmlns:w15="` + nsW15 + `" xmlns:r="` + nsR + `" ` + mc + ` mc:Ignorable="w14 w15"></w:comments>`
	case "extended":
		return xmlHead + `<w15:commentsEx xmlns:w15="` + nsW15 + `" ` + mc + ` mc:Ignorable="w15"></w15:commentsEx>`
	case "ids":
		return xmlHead + `<w16cid:commentsIds xmlns:w16cid="` + nsW16CID + `" ` + mc + ` mc:Ignorable="w16cid"></w16cid:commentsIds>`
	case "extensible":
		return xmlHead + `<w16cex:commentsExtensible xmlns:w16cex="` + nsW16CEX + `" ` + mc + ` mc:Ignorable="w16cex"></w16cex:commentsExtensible>`
	default:
		return xmlHead + `<w15:people xmlns:w15="` + nsW15 + `" ` + mc + ` mc:Ignorable="w15"></w15:people>`
	}
}

// rootNamespaces are the declarations a part's root needs for what this package writes into it.
var rootNamespaces = map[string][][2]string{
	"comments":   {{"w", nsW}, {"w14", nsW14}},
	"extended":   {{"w15", nsW15}},
	"ids":        {{"w16cid", nsW16CID}},
	"extensible": {{"w16cex", nsW16CEX}},
	"people":     {{"w15", nsW15}},
}

// wordComment is one w:comment as the file has it.
type wordComment struct {
	wordID string
	// id is the browser's id for it: its durable id, else its last paragraph id, else "W"+w:id.
	id           string
	author       string
	date         time.Time
	text         string
	lastParaID   string
	durableID    string
	parentParaID string
	done         bool
}

type person struct {
	author, providerID, userID string
}

// comments is what a package's comment parts hold.
type comments struct {
	p        *pkg
	mainPath string
	// paths are the parts by key, as the main document relates to them.
	paths    map[string]string
	comments []*wordComment
	people   []person
}

func readComments(p *pkg) (*comments, error) {
	mainPath, err := p.mainPart()
	if err != nil {
		return nil, err
	}
	rels, err := p.rels(mainPath)
	if err != nil {
		return nil, err
	}
	c := &comments{p: p, mainPath: mainPath, paths: map[string]string{}}
	for _, part := range commentParts {
		for _, r := range rels {
			if r.typ == part.rel && !r.external {
				c.paths[part.key] = resolveTarget(mainPath, r.target)
				break
			}
		}
	}
	root := func(key string) (*node, error) {
		name, ok := c.paths[key]
		if !ok {
			return nil, nil
		}
		data, err := p.read(name)
		if err != nil || data == nil {
			return nil, err
		}
		n, err := parseXML(data)
		if err != nil {
			return nil, officecomments.ErrNotOffice
		}
		return n, nil
	}

	type ex struct {
		parent string
		done   bool
	}
	extended := map[string]ex{}
	if r, err := root("extended"); err != nil {
		return nil, err
	} else if r != nil {
		for _, el := range r.all(nsW15, "commentEx") {
			paraID, _ := el.attr(nsW15, "paraId")
			if paraID == "" {
				continue
			}
			parent, _ := el.attr(nsW15, "paraIdParent")
			done, _ := el.attr(nsW15, "done")
			extended[strings.ToUpper(paraID)] = ex{strings.ToUpper(parent), done == "1"}
		}
	}
	durable := map[string]string{}
	if r, err := root("ids"); err != nil {
		return nil, err
	} else if r != nil {
		for _, el := range r.all(nsW16CID, "commentId") {
			paraID, _ := el.attr(nsW16CID, "paraId")
			id, _ := el.attr(nsW16CID, "durableId")
			if paraID != "" && id != "" {
				durable[strings.ToUpper(paraID)] = strings.ToUpper(id)
			}
		}
	}
	utc := map[string]time.Time{}
	if r, err := root("extensible"); err != nil {
		return nil, err
	} else if r != nil {
		for _, el := range r.all(nsW16CEX, "commentExtensible") {
			id, _ := el.attr(nsW16CEX, "durableId")
			date, _ := el.attr(nsW16CEX, "dateUtc")
			if t, err := time.Parse(time.RFC3339, date); err == nil && id != "" {
				utc[strings.ToUpper(id)] = t.UTC()
			}
		}
	}

	r, err := root("comments")
	if err != nil {
		return nil, err
	}
	used := map[string]bool{}
	if r != nil {
		for _, el := range r.all(nsW, "comment") {
			wordID, ok := el.attr(nsW, "id")
			if !ok {
				continue
			}
			var lastParaID string
			paragraphs := el.all(nsW, "p")
			if len(paragraphs) > 0 {
				id, _ := paragraphs[len(paragraphs)-1].attr(nsW14, "paraId")
				lastParaID = strings.ToUpper(id)
			}
			durableID := ""
			if lastParaID != "" {
				durableID = durable[lastParaID]
			}
			id := durableID
			if id == "" {
				id = lastParaID
			}
			if id == "" || used[id] {
				id = "W" + wordID
			}
			used[id] = true
			author, _ := el.attr(nsW, "author")
			texts := make([]string, len(paragraphs))
			for i, p := range paragraphs {
				texts[i] = paragraphText(p)
			}
			wc := &wordComment{wordID: wordID, id: id, author: author, text: strings.Join(texts, "\n"),
				lastParaID: lastParaID, durableID: durableID}
			if t, ok := utc[durableID]; ok && durableID != "" {
				wc.date = t
			} else {
				date, _ := el.attr(nsW, "date")
				wc.date = wordDate(date)
			}
			if e, ok := extended[lastParaID]; ok && lastParaID != "" {
				wc.parentParaID, wc.done = e.parent, e.done
			}
			c.comments = append(c.comments, wc)
		}
	}

	if r, err := root("people"); err != nil {
		return nil, err
	} else if r != nil {
		for _, el := range r.all(nsW15, "person") {
			pr := person{}
			pr.author, _ = el.attr(nsW15, "author")
			if presence := el.all(nsW15, "presenceInfo"); len(presence) > 0 {
				pr.providerID, _ = presence[0].attr(nsW15, "providerId")
				pr.userID, _ = presence[0].attr(nsW15, "userId")
			}
			c.people = append(c.people, pr)
		}
	}
	return c, nil
}

// paragraphText is a comment paragraph's text, as the browser reads it.
func paragraphText(p *node) string {
	var b strings.Builder
	p.walk(func(n *node) bool {
		if n.space == nsW {
			switch n.local {
			case "t":
				b.WriteString(n.text)
			case "tab":
				b.WriteString("\t")
			case "br", "cr":
				b.WriteString("\n")
			}
		}
		return true
	}, nil)
	return b.String()
}

// wordDate reads w:date as written: Word writes local time marked Z.
func wordDate(value string) time.Time {
	v := strings.TrimSuffix(value, "Z")
	for _, layout := range []string{"2006-01-02T15:04:05.999999999", "2006-01-02T15:04", "2006-01-02"} {
		if t, err := time.Parse(layout, v); err == nil {
			return t
		}
	}
	return time.Unix(0, 0).UTC()
}

// personID is the panel's id for an author, as the browser's personFor.
func (c *comments) personID(author string) string {
	for _, p := range c.people {
		if p.author == author {
			return officecomments.PersonID(p.providerID, p.userID, author)
		}
	}
	return "author:" + author
}

// thread is one of the file's threads: its comments, root first, in file order.
type thread struct {
	id       string
	resolved bool
	comments []*wordComment
}

// threads groups the comments into threads as the browser's wordThreads does, in the order
// their roots come in comments.xml.
func (c *comments) threads() ([]*thread, map[string]*thread) {
	byPara := map[string]*wordComment{}
	for _, wc := range c.comments {
		if wc.lastParaID != "" {
			byPara[wc.lastParaID] = wc
		}
	}
	rootOf := func(wc *wordComment) *wordComment {
		at := wc
		for hops := 0; at.parentParaID != "" && hops < 50; hops++ {
			parent := byPara[at.parentParaID]
			if parent == nil || parent == at {
				break
			}
			at = parent
		}
		return at
	}
	var list []*thread
	byWordID := map[string]*thread{}
	byID := map[string]*thread{}
	for _, wc := range c.comments {
		root := rootOf(wc)
		t := byID[root.id]
		if t == nil {
			t = &thread{id: root.id, resolved: root.done}
			byID[root.id] = t
			list = append(list, t)
		}
		if wc == root {
			t.comments = append([]*wordComment{wc}, t.comments...)
		} else {
			t.comments = append(t.comments, wc)
		}
		byWordID[wc.wordID] = t
	}
	return list, byWordID
}

func (c *comments) toThread(t *thread) officecomments.Thread {
	out := officecomments.Thread{ID: t.id, Resolved: t.resolved}
	for _, wc := range t.comments {
		name := wc.author
		if name == "" {
			name = "Unknown"
		}
		out.Comments = append(out.Comments, officecomments.Comment{
			ID: wc.id, AuthorID: c.personID(wc.author), AuthorName: name, CreatedAt: wc.date, Text: wc.text,
		})
	}
	return out
}
