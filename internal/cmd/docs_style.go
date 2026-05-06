package cmd

import (
	"context"
	"fmt"
	"os"
	"strings"

	"google.golang.org/api/docs/v1"

	"github.com/steipete/gogcli/internal/outfmt"
	"github.com/steipete/gogcli/internal/ui"
)

// validNamedStyles lists all accepted Google Docs named style types.
var validNamedStyles = map[string]string{
	"normal":   "NORMAL_TEXT",
	"title":    "TITLE",
	"subtitle": "SUBTITLE",
	"heading1": "HEADING_1",
	"heading2": "HEADING_2",
	"heading3": "HEADING_3",
	"heading4": "HEADING_4",
	"heading5": "HEADING_5",
	"heading6": "HEADING_6",
	"h1":       "HEADING_1",
	"h2":       "HEADING_2",
	"h3":       "HEADING_3",
	"h4":       "HEADING_4",
	"h5":       "HEADING_5",
	"h6":       "HEADING_6",
	// Also accept raw API names directly.
	"NORMAL_TEXT": "NORMAL_TEXT",
	"TITLE":       "TITLE",
	"SUBTITLE":    "SUBTITLE",
	"HEADING_1":   "HEADING_1",
	"HEADING_2":   "HEADING_2",
	"HEADING_3":   "HEADING_3",
	"HEADING_4":   "HEADING_4",
	"HEADING_5":   "HEADING_5",
	"HEADING_6":   "HEADING_6",
}

// DocsStyleCmd applies a named paragraph style to a range of paragraphs.
//
// Usage examples:
//
//	gog docs style <docId> --paragraphs 2-11 --style normal
//	gog docs style <docId> --paragraphs 5 --style heading1
//	gog docs style <docId> --find "Week of 2026-04-27" --through "Sun:    (away)" --style normal
type DocsStyleCmd struct {
	DocID string `arg:"" name:"docId" help:"Doc ID"`

	// Selection: either --paragraphs or --find/--through
	Paragraphs string `name:"paragraphs" help:"Paragraph numbers to restyle: single (5), range (2-11), or comma-separated (1,3,5)"`
	Find       string `name:"find" help:"Text of first paragraph to restyle (matched by content)"`
	Through    string `name:"through" help:"Text of last paragraph to restyle (matched by content; omit to restyle only the --find paragraph)"`

	Style string `name:"style" short:"s" required:"" help:"Style to apply: normal, title, subtitle, h1-h6, heading1-heading6 (or raw API name like NORMAL_TEXT)"`
	TabID string `name:"tab-id" help:"Target a specific tab by ID"`
}

func (c *DocsStyleCmd) Run(ctx context.Context, flags *RootFlags) error {
	u := ui.FromContext(ctx)
	docID := strings.TrimSpace(c.DocID)
	if docID == "" {
		return usage("empty docId")
	}

	// Resolve style name.
	namedStyle, ok := validNamedStyles[c.Style]
	if !ok {
		return usage(fmt.Sprintf("unknown style %q — use: normal, title, subtitle, h1-h6, heading1-heading6", c.Style))
	}

	// Exactly one selection method must be provided.
	hasParagraphs := c.Paragraphs != ""
	hasFind := c.Find != ""
	if !hasParagraphs && !hasFind {
		return usage("required: --paragraphs or --find")
	}
	if hasParagraphs && hasFind {
		return usage("cannot use both --paragraphs and --find")
	}

	svc, err := requireDocsService(ctx, flags)
	if err != nil {
		return err
	}

	pm, err := fetchAndBuildMap(ctx, svc, docID, c.TabID)
	if err != nil {
		return err
	}

	// Collect the target paragraph numbers.
	var targets []int
	if hasParagraphs {
		targets, err = parseParagraphSelection(c.Paragraphs, len(pm.Paragraphs))
		if err != nil {
			return err
		}
	} else {
		targets, err = findParagraphRange(pm, c.Find, c.Through)
		if err != nil {
			return err
		}
	}

	if len(targets) == 0 {
		return fmt.Errorf("no paragraphs selected")
	}

	// Build one UpdateParagraphStyle request per selected paragraph.
	reqs := make([]*docs.Request, 0, len(targets))
	for _, num := range targets {
		p, perr := pm.get(num)
		if perr != nil {
			return perr
		}
		reqs = append(reqs, &docs.Request{
			UpdateParagraphStyle: &docs.UpdateParagraphStyleRequest{
				Range: &docs.Range{
					StartIndex: p.StartIndex,
					EndIndex:   p.EndIndex,
					TabId:      c.TabID,
				},
				ParagraphStyle: &docs.ParagraphStyle{
					NamedStyleType: namedStyle,
				},
				Fields: "namedStyleType",
			},
		})
	}

	result, err := svc.Documents.BatchUpdate(docID, &docs.BatchUpdateDocumentRequest{
		Requests: reqs,
	}).Context(ctx).Do()
	if err != nil {
		if isDocsNotFound(err) {
			return fmt.Errorf("doc not found or not a Google Doc (id=%s)", docID)
		}
		return fmt.Errorf("updating paragraph style: %w", err)
	}

	if outfmt.IsJSON(ctx) {
		payload := map[string]any{
			"documentId": result.DocumentId,
			"style":      namedStyle,
			"paragraphs": targets,
			"count":      len(targets),
		}
		if c.TabID != "" {
			payload["tabId"] = c.TabID
		}
		return outfmt.WriteJSON(ctx, os.Stdout, payload)
	}

	u.Out().Printf("documentId\t%s", result.DocumentId)
	u.Out().Printf("style\t%s", namedStyle)
	u.Out().Printf("paragraphs\t%v", targets)
	u.Out().Printf("count\t%d", len(targets))
	if c.TabID != "" {
		u.Out().Printf("tabId\t%s", c.TabID)
	}
	return nil
}

// parseParagraphSelection parses a selection string into 1-based paragraph numbers.
// Supports: "5", "2-11", "1,3,5", "2-5,8,10-12".
func parseParagraphSelection(sel string, total int) ([]int, error) {
	var nums []int
	seen := map[int]bool{}

	for _, part := range strings.Split(sel, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if dashIdx := strings.Index(part, "-"); dashIdx > 0 {
			// Range: "2-11"
			var start, end int
			if _, err := fmt.Sscanf(part[:dashIdx], "%d", &start); err != nil {
				return nil, fmt.Errorf("invalid range %q", part)
			}
			if _, err := fmt.Sscanf(part[dashIdx+1:], "%d", &end); err != nil {
				return nil, fmt.Errorf("invalid range %q", part)
			}
			if start < 1 || end < start || end > total {
				return nil, fmt.Errorf("range %d-%d out of bounds (document has %d paragraphs)", start, end, total)
			}
			for i := start; i <= end; i++ {
				if !seen[i] {
					nums = append(nums, i)
					seen[i] = true
				}
			}
		} else {
			// Single number.
			var n int
			if _, err := fmt.Sscanf(part, "%d", &n); err != nil {
				return nil, fmt.Errorf("invalid paragraph number %q", part)
			}
			if n < 1 || n > total {
				return nil, fmt.Errorf("paragraph %d out of bounds (document has %d paragraphs)", n, total)
			}
			if !seen[n] {
				nums = append(nums, n)
				seen[n] = true
			}
		}
	}
	return nums, nil
}

// findParagraphRange finds a contiguous range of paragraphs by text content.
// Returns all paragraph numbers from the one containing `find` through the one containing `through`.
// If `through` is empty, returns only the paragraph matching `find`.
func findParagraphRange(pm *paragraphMap, find, through string) ([]int, error) {
	findLower := strings.ToLower(strings.TrimSpace(find))
	throughLower := strings.ToLower(strings.TrimSpace(through))

	startNum := -1
	endNum := -1

	for _, p := range pm.Paragraphs {
		text := strings.ToLower(strings.TrimSpace(p.Text))
		if startNum == -1 && strings.Contains(text, findLower) {
			startNum = p.Num
			if throughLower == "" {
				endNum = p.Num
				break
			}
		}
		if startNum != -1 && throughLower != "" && strings.Contains(text, throughLower) {
			endNum = p.Num
			break
		}
	}

	if startNum == -1 {
		return nil, fmt.Errorf("--find text not found: %q", find)
	}
	if throughLower != "" && endNum == -1 {
		return nil, fmt.Errorf("--through text not found after paragraph %d: %q", startNum, through)
	}

	nums := make([]int, 0, endNum-startNum+1)
	for i := startNum; i <= endNum; i++ {
		nums = append(nums, i)
	}
	return nums, nil
}
