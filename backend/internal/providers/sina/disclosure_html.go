package sina

import (
	"fmt"
	"golang.org/x/net/html"
	"io"
	"strings"
)

// Parse public disclosure HTML with bounded work before building a DOM. Source
// markup is data; declarative pagination parameters are never executed.
func disclosureDOM(document string) (*html.Node, error) {
	if len(document) > 4<<20 {
		return nil, fmt.Errorf("decoded disclosure HTML exceeds limit")
	}
	z := html.NewTokenizer(strings.NewReader(document))
	depth, tokens := 0, 0
	for {
		kind := z.Next()
		if kind == html.ErrorToken {
			if z.Err() != io.EOF {
				return nil, z.Err()
			}
			break
		}
		tokens++
		if tokens > 100000 {
			return nil, fmt.Errorf("disclosure DOM token limit exceeded")
		}
		if kind == html.StartTagToken {
			name, _ := z.TagName()
			switch string(name) {
			case "area", "base", "br", "col", "embed", "hr", "img", "input", "link", "meta", "param", "source", "track", "wbr":
			default:
				depth++
			}
		} else if kind == html.EndTagToken && depth > 0 {
			depth--
		}
		if depth > 128 {
			return nil, fmt.Errorf("disclosure DOM nesting limit exceeded")
		}
	}
	return html.Parse(strings.NewReader(document))
}
