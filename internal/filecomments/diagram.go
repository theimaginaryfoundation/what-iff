package filecomments

import (
	"errors"
	"fmt"

	"github.com/theimaginaryfoundation/what-iff/internal/drawio"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
)

// Comments on a draw.io diagram's elements (ADR 0x049). A thread is on one element (its Cell: the
// cell's id and its page's) or on the whole file; never on a range of the XML. Its quote is the
// element's label, as the diagram has it now, so the panel and the agent can say what it is on.
//
// Elements keep their ids when they are moved, restyled or relabelled, so there is nothing to carry:
// once the diagram changed, each thread's element is looked up again, its label quoted afresh, and
// a thread whose element is gone is on the whole file with the label it had, as a text thread is
// once its words are gone.

// isDiagram reports whether the file is a draw.io diagram, whose threads are on its elements.
func isDiagram(file *models.FileAttachment) bool {
	return drawio.IsDiagramFile(file.Name, file.FileType)
}

// OnCells checks each thread against the diagram's XML: a thread on an element that is there is
// quoted with its label (and given its page when it had none); one whose element is gone is on the
// whole file, keeping the quote it had. No thread is on a range of the text. A diagram that cannot
// be read leaves the threads' elements as they are.
func OnCells(threads []models.FileCommentThread, xml string) []models.FileCommentThread {
	pages, err := drawio.List([]byte(xml))
	out := make([]models.FileCommentThread, len(threads))
	for i, t := range threads {
		if t.Comments == nil {
			t.Comments = []models.FileComment{}
		}
		t.Anchor = nil
		if t.Cell != nil && err == nil {
			if page, el, ok := findCell(pages, *t.Cell); ok {
				t.Cell = &models.FileCommentCell{ID: el.ID, Page: page}
				t.Quote = el.Label
			} else {
				t.Cell = nil
			}
		}
		out[i] = t
	}
	return out
}

// CellOn is the element of the diagram in xml a new thread is on: the cell with id cell, on the
// page page names (as drawio.FindPage reads it; empty for any page). A diagram that cannot be read
// is an ErrDiagramUnreadable error saying why; a page it does not have, a drawio.ErrNoPage one.
func CellOn(xml, cell, page string) (*models.FileCommentCell, string, error) {
	pages, err := drawio.List([]byte(xml))
	if err != nil {
		return nil, "", fmt.Errorf("%w: %w", ErrDiagramUnreadable, err)
	}
	if page != "" {
		i, err := drawio.FindPage(pages, page)
		if err != nil {
			return nil, "", err
		}
		page = pages[i].ID
		if page == "" {
			// A page without an id: look in it alone.
			pages = pages[i : i+1]
		}
	}
	pageID, el, ok := findCell(pages, models.FileCommentCell{ID: cell, Page: page})
	if !ok {
		return nil, "", ErrCellNotFound
	}
	return &models.FileCommentCell{ID: el.ID, Page: pageID}, el.Label, nil
}

var (
	// ErrCellNotFound means a diagram has no element with the id a new thread names.
	ErrCellNotFound = errors.New("the diagram has no element with that id")
	// ErrDiagramUnreadable means a diagram's XML could not be read (drawio.ErrCompressed,
	// drawio.ErrNotDiagram, …); the error says why.
	ErrDiagramUnreadable = errors.New("the diagram could not be read")
)

// findCell is the element a thread's cell names: on its page, or on any page when it names none
// (an id is unique only within its page, so the first page that has it).
func findCell(pages []drawio.Page, cell models.FileCommentCell) (string, drawio.Element, bool) {
	for _, p := range pages {
		if cell.Page != "" && p.ID != cell.Page {
			continue
		}
		for _, el := range p.Elements {
			if el.ID == cell.ID {
				return p.ID, el, true
			}
		}
	}
	return "", drawio.Element{}, false
}
