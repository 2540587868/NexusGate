package router

import (
	"sort"
	"strings"
)

type radixNode struct {
	path      string
	routes    []*Route
	children  []*radixNode
	wildcard  *radixNode
	isExact   bool
}

type RadixTree struct {
	root *radixNode
}

func NewRadixTree() *RadixTree {
	return &RadixTree{
		root: &radixNode{path: "/"},
	}
}

func (t *RadixTree) Insert(route *Route) {
	if route.Match.PathExact != "" {
		t.insertExact(route.Match.PathExact, route)
	} else if route.Match.PathPrefix != "" {
		t.insertPrefix(route.Match.PathPrefix, route)
	} else {
		t.root.routes = append(t.root.routes, route)
	}
}

func (t *RadixTree) insertExact(path string, route *Route) {
	node := t.findOrCreateNode(path)
	node.routes = append(node.routes, route)
	node.isExact = true
}

func (t *RadixTree) insertPrefix(prefix string, route *Route) {
	node := t.findOrCreateNode(prefix)
	node.routes = append(node.routes, route)
}

func (t *RadixTree) findOrCreateNode(path string) *radixNode {
	if path == "" || path == "/" {
		return t.root
	}

	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}

	current := t.root
	remaining := path

	for len(remaining) > 0 {
		if remaining == "/" {
			return current
		}

		child, commonPrefix := t.findChild(current, remaining)
		if child == nil {
			return t.appendChild(current, remaining)
		}

		prefixLen := len(commonPrefix)
		childPath := child.path

		if prefixLen == len(childPath) {
			current = child
			remaining = remaining[prefixLen:]
			continue
		}

		if prefixLen == len(remaining) {
			return t.splitNodeWithPrefix(current, child, remaining, prefixLen)
		}

		return t.splitNodePartial(current, child, remaining, childPath, prefixLen)
	}

	return current
}

func (t *RadixTree) appendChild(parent *radixNode, path string) *radixNode {
	newNode := &radixNode{path: path}
	parent.children = append(parent.children, newNode)
	sort.Slice(parent.children, func(i, j int) bool {
		return parent.children[i].path < parent.children[j].path
	})
	return newNode
}

func (t *RadixTree) splitNodeWithPrefix(parent *radixNode, child *radixNode, prefix string, prefixLen int) *radixNode {
	splitNode := &radixNode{
		path:     prefix,
		children: []*radixNode{{path: child.path[prefixLen:], routes: child.routes, children: child.children, isExact: child.isExact}},
	}
	if child.wildcard != nil {
		splitNode.children[0].wildcard = child.wildcard
	}

	t.replaceChild(parent, child, splitNode)
	return splitNode
}

func (t *RadixTree) splitNodePartial(parent *radixNode, child *radixNode, remaining string, childPath string, prefixLen int) *radixNode {
	splitNode := &radixNode{
		path:     childPath[:prefixLen],
		children: []*radixNode{},
	}

	oldChild := &radixNode{
		path:      childPath[prefixLen:],
		routes:    child.routes,
		children:  child.children,
		isExact:   child.isExact,
	}
	if child.wildcard != nil {
		oldChild.wildcard = child.wildcard
	}

	newChild := &radixNode{path: remaining[prefixLen:]}

	splitNode.children = []*radixNode{oldChild, newChild}
	sort.Slice(splitNode.children, func(i, j int) bool {
		return splitNode.children[i].path < splitNode.children[j].path
	})

	t.replaceChild(parent, child, splitNode)
	return newChild
}

func (t *RadixTree) replaceChild(parent *radixNode, old, new *radixNode) {
	for i, c := range parent.children {
		if c == old {
			parent.children[i] = new
			return
		}
	}
}

func (t *RadixTree) findChild(node *radixNode, path string) (*radixNode, string) {
	if len(node.children) == 0 {
		return nil, ""
	}

	for _, child := range node.children {
		if len(child.path) == 0 {
			continue
		}
		commonPrefix := commonPrefix(path, child.path)
		if len(commonPrefix) > 0 {
			return child, commonPrefix
		}
	}

	return nil, ""
}

func (t *RadixTree) Lookup(path string) []*Route {
	var results []*Route

	t.collectMatches(t.root, path, path, &results)

	return results
}

func (t *RadixTree) collectMatches(node *radixNode, remaining string, fullPath string, results *[]*Route) {
	if len(node.routes) > 0 {
		for _, r := range node.routes {
			if r.Match.PathExact != "" {
				if fullPath == r.Match.PathExact {
					*results = append(*results, r)
				}
			} else {
				*results = append(*results, r)
			}
		}
	}

	if len(node.children) == 0 || len(remaining) == 0 {
		return
	}

	for _, child := range node.children {
		if strings.HasPrefix(remaining, child.path) {
			t.collectMatches(child, remaining[len(child.path):], fullPath, results)
			break
		}
	}
}

func (t *RadixTree) LongestPrefixMatch(path string) *Route {
	var bestRoute *Route
	var bestLen int

	t.findLongestPrefix(t.root, path, 0, &bestRoute, &bestLen)

	return bestRoute
}

func (t *RadixTree) findLongestPrefix(node *radixNode, path string, matchedLen int, bestRoute **Route, bestLen *int) {
	if len(node.routes) > 0 {
		for _, r := range node.routes {
			if r.Match.PathPrefix != "" && matchedLen > *bestLen {
				*bestRoute = r
				*bestLen = matchedLen
			}
		}
	}

	if len(path) == 0 {
		return
	}

	for _, child := range node.children {
		if strings.HasPrefix(path, child.path) {
			t.findLongestPrefix(child, path[len(child.path):], matchedLen+len(child.path), bestRoute, bestLen)
			return
		}
	}
}

func (t *RadixTree) ExactMatch(path string) *Route {
	node := t.findExactNode(t.root, path)
	if node == nil {
		return nil
	}
	for _, r := range node.routes {
		if r.Match.PathExact == path {
			return r
		}
	}
	return nil
}

func (t *RadixTree) findExactNode(node *radixNode, path string) *radixNode {
	if len(path) == 0 {
		return node
	}

	for _, child := range node.children {
		if strings.HasPrefix(path, child.path) {
			return t.findExactNode(child, path[len(child.path):])
		}
	}

	return nil
}

func (t *RadixTree) AllRoutes() []*Route {
	var routes []*Route
	t.collectAllRoutes(t.root, &routes)
	return routes
}

func (t *RadixTree) collectAllRoutes(node *radixNode, routes *[]*Route) {
	*routes = append(*routes, node.routes...)
	for _, child := range node.children {
		t.collectAllRoutes(child, routes)
	}
}

func commonPrefix(a, b string) string {
	minLen := len(a)
	if len(b) < minLen {
		minLen = len(b)
	}
	i := 0
	for i < minLen && a[i] == b[i] {
		i++
	}
	return a[:i]
}
