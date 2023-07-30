package html_parser

import (
	"bytes"
	"io"
	"log"
	"strings"

	"golang.org/x/net/html"
)

func htmlGetAttr(n *html.Node, key string) (string, bool) {
	for _, attr := range n.Attr {
		if attr.Key == key {
			return attr.Val, true
		}
	}
	return "", false
}

// htmlRenderNode renders a node, excluding its children
func htmlRenderNode(n *html.Node) string {
	var buf bytes.Buffer
	w := io.Writer(&buf)

	// walk backwards through the document tree to find the root
	for {
		if n.Parent != nil {
			n = n.Parent
			continue
		}
		break
	}

	err := html.Render(w, n)
	if err != nil {
		return ""
	}

	return buf.String()
}

func htmlHasID(n *html.Node, id string) bool {
	if n.Type == html.ElementNode {
		s, ok := htmlGetAttr(n, "id")
		if ok && s == id {
			return true
		}
	}

	return false
}

func htmlWalkTree(n *html.Node, id string) *html.Node {
	if htmlHasID(n, id) {
		return n
	}

	for c := n.FirstChild; c != nil; c = c.NextSibling {
		res := htmlWalkTree(c, id)
		if res != nil {
			return res
		}
	}

	return nil
}

func htmlGetByID(n *html.Node, id string) *html.Node {
	return htmlWalkTree(n, id)
}

// ParseAndSplice parses HTML within 'file' and splices `htmlContent` into its node tree at `id`
//
// Returns rendered HTML content as a string
func ParseAndSplice(file io.Reader, id, htmlContent string) (content string) {
	doc, err := html.Parse(file)
	if err != nil {
		log.Fatal(err)
	}

	log.Println("HERE", htmlContent)
	doc = htmlGetByID(doc, id)
	if doc == nil {
		return
	}

	// Get rid of the element's children, as we want to swap our own content in as this element's _only_ content
	doc.FirstChild = nil
	doc.LastChild = nil

	// Parse the content we're swapping in
	// This creates a "well-formed" tree, meaning that it adds <html><head></head><body><our node/></body></html>
	// but we don't want a well-formed tree, we want our new content as a single Node. The tree parents will be
	// removed
	newHtml, err := html.Parse(strings.NewReader(htmlContent))
	if err != nil {
		log.Fatal(err)
	}

	// Remove the tree parents, as we do not want a "well-formed" tree
	for {
		if newHtml.LastChild == nil {
			break
		}
		newHtml = newHtml.LastChild
	}

	// We've walked all the way down the new tree to the lowermost node of our new HTML tree.
	// Get its parent to get the outermost element of the new content (spliced in nodes should
	// not consist of nested node)
	newHtml = newHtml.Parent

	// "detach" the new node so it can be spliced into the tree
	newHtml.Parent = nil
	newHtml.PrevSibling = nil
	newHtml.NextSibling = nil

	doc.AppendChild(newHtml)

	content = htmlRenderNode(doc)

	return
}
