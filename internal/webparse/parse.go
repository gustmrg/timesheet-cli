package webparse

import (
	"bytes"
	"fmt"
	"io"
	"strconv"
	"strings"

	"golang.org/x/net/html"
)

type Entry struct {
	ID         int64  `json:"id"`
	EvaluateID *int64 `json:"evaluateId"`
	Customer   string `json:"customer"`
	Project    string `json:"project"`
	Category   string `json:"category"`
	Date       string `json:"date"`
	Start      string `json:"start"`
	End        string `json:"end"`
	Total      string `json:"total"`
	Status     string `json:"status,omitempty"`
}

type Option struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

type InvoicePreviewItem struct {
	Customer           string
	Project            string
	MonetizedHours     string
	NonMonetizedHours  string
	MonetizedAmount    string
	NonMonetizedAmount string
}

func Entries(r io.Reader) ([]Entry, error) {
	doc, err := html.Parse(r)
	if err != nil {
		return nil, fmt.Errorf("parse worksheet HTML: %w", err)
	}
	table := find(doc, func(n *html.Node) bool {
		return n.Type == html.ElementNode && n.Data == "table" && attr(n, "id") == "tbWorksheet"
	})
	if table == nil {
		return []Entry{}, nil
	}
	var entries []Entry
	walk(table, func(n *html.Node) {
		if n.Type != html.ElementNode || n.Data != "tr" {
			return
		}
		cells := directElements(n, "td")
		if len(cells) < 9 {
			return
		}
		id, evaluateID, status := entryAnchor(cells[7])
		if id == 0 {
			id, _, _ = entryAnchor(cells[8])
		}
		if id == 0 {
			return
		}
		entries = append(entries, Entry{
			ID: id, EvaluateID: evaluateID,
			Customer: text(cells[0]), Project: text(cells[1]), Category: text(cells[2]),
			Date: text(cells[3]), Start: text(cells[4]), End: text(cells[5]), Total: text(cells[6]),
			Status: status,
		})
	})
	if entries == nil {
		entries = []Entry{}
	}
	return entries, nil
}

func Selects(r io.Reader) (map[string][]Option, error) {
	doc, err := html.Parse(r)
	if err != nil {
		return nil, fmt.Errorf("parse metadata HTML: %w", err)
	}
	result := make(map[string][]Option)
	walk(doc, func(n *html.Node) {
		if n.Type != html.ElementNode || n.Data != "select" {
			return
		}
		name := attr(n, "name")
		classes := strings.Fields(attr(n, "class"))
		var options []Option
		walk(n, func(option *html.Node) {
			if option.Type != html.ElementNode || option.Data != "option" {
				return
			}
			value := attr(option, "value")
			if value == "" {
				return
			}
			id, parseErr := strconv.ParseInt(value, 10, 64)
			if parseErr == nil {
				options = append(options, Option{ID: id, Name: text(option)})
			}
		})
		if name != "" {
			result[name] = options
		}
		for _, class := range classes {
			if class != "form-control" {
				result[class] = options
			}
		}
	})
	return result, nil
}

func InvoicePreviewItems(r io.Reader) ([]InvoicePreviewItem, error) {
	doc, err := html.Parse(r)
	if err != nil {
		return nil, fmt.Errorf("parse invoice preview HTML: %w", err)
	}
	table := find(doc, func(n *html.Node) bool {
		return n.Type == html.ElementNode && n.Data == "table" && attr(n, "id") == "tbWorksheet"
	})
	if table == nil {
		return []InvoicePreviewItem{}, nil
	}
	var items []InvoicePreviewItem
	walk(table, func(n *html.Node) {
		if n.Type != html.ElementNode || n.Data != "tr" {
			return
		}
		cells := directElements(n, "td")
		if len(cells) < 6 {
			return
		}
		items = append(items, InvoicePreviewItem{
			Customer: text(cells[0]), Project: text(cells[1]),
			MonetizedHours: text(cells[2]), NonMonetizedHours: text(cells[3]),
			MonetizedAmount: text(cells[4]), NonMonetizedAmount: text(cells[5]),
		})
	})
	if items == nil {
		items = []InvoicePreviewItem{}
	}
	return items, nil
}

func IsLoginPage(body []byte) bool {
	doc, err := html.Parse(bytes.NewReader(body))
	if err != nil {
		return false
	}
	login := find(doc, func(n *html.Node) bool { return n.Type == html.ElementNode && attr(n, "name") == "Login" }) != nil
	password := find(doc, func(n *html.Node) bool { return n.Type == html.ElementNode && attr(n, "name") == "Password" }) != nil
	return login && password
}

func VerificationToken(body []byte) string {
	doc, err := html.Parse(bytes.NewReader(body))
	if err != nil {
		return ""
	}
	n := find(doc, func(n *html.Node) bool {
		return n.Type == html.ElementNode && n.Data == "input" && attr(n, "name") == "__RequestVerificationToken"
	})
	return attr(n, "value")
}

func FragmentText(fragment string) (string, error) {
	doc, err := html.Parse(strings.NewReader("<div id=\"description-root\">" + fragment + "</div>"))
	if err != nil {
		return "", fmt.Errorf("parse description HTML: %w", err)
	}
	root := find(doc, func(n *html.Node) bool { return n.Type == html.ElementNode && attr(n, "id") == "description-root" })
	if root == nil {
		return "", nil
	}
	var builder strings.Builder
	var render func(*html.Node)
	render = func(n *html.Node) {
		if n.Type == html.TextNode {
			builder.WriteString(n.Data)
			return
		}
		if n.Type == html.ElementNode && n.Data == "br" {
			builder.WriteByte('\n')
			return
		}
		for child := n.FirstChild; child != nil; child = child.NextSibling {
			render(child)
		}
		if n.Type == html.ElementNode && n.Data == "p" {
			builder.WriteString("\n\n")
		}
	}
	render(root)
	return strings.TrimRight(builder.String(), "\n"), nil
}

func entryAnchor(cell *html.Node) (int64, *int64, string) {
	anchor := find(cell, func(n *html.Node) bool { return n.Type == html.ElementNode && n.Data == "a" && attr(n, "id") != "" })
	if anchor == nil {
		return 0, nil, ""
	}
	id, _ := strconv.ParseInt(attr(anchor, "id"), 10, 64)
	var evaluateID *int64
	if _, value, ok := strings.Cut(attr(anchor, "name"), "|"); ok {
		if parsed, err := strconv.ParseInt(value, 10, 64); err == nil {
			evaluateID = &parsed
		}
	}
	status := attr(anchor, "title")
	if status == "" {
		status = text(cell)
	}
	return id, evaluateID, status
}

func attr(n *html.Node, key string) string {
	if n == nil {
		return ""
	}
	for _, a := range n.Attr {
		if a.Key == key {
			return a.Val
		}
	}
	return ""
}

func text(n *html.Node) string {
	var parts []string
	walk(n, func(current *html.Node) {
		if current.Type == html.TextNode {
			parts = append(parts, current.Data)
		}
	})
	return strings.Join(strings.Fields(strings.Join(parts, " ")), " ")
}

func directElements(n *html.Node, name string) []*html.Node {
	var result []*html.Node
	for child := n.FirstChild; child != nil; child = child.NextSibling {
		if child.Type == html.ElementNode && child.Data == name {
			result = append(result, child)
		}
	}
	return result
}

func find(n *html.Node, predicate func(*html.Node) bool) *html.Node {
	if n == nil {
		return nil
	}
	if predicate(n) {
		return n
	}
	for child := n.FirstChild; child != nil; child = child.NextSibling {
		if match := find(child, predicate); match != nil {
			return match
		}
	}
	return nil
}

func walk(n *html.Node, visit func(*html.Node)) {
	if n == nil {
		return
	}
	visit(n)
	for child := n.FirstChild; child != nil; child = child.NextSibling {
		walk(child, visit)
	}
}
