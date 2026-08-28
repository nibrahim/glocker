package config

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// includeTag is the custom YAML tag that pulls another file's contents in place.
// It keeps the hand-edited config small while the big generated lists (blocklists
// from update_domains.py) live in their own files under conf.d/.
//
// Two forms:
//
//	domains: !include conf.d/domains.yaml     # value is replaced by the file
//
//	domains:                                  # each include is flattened into
//	  - !include conf.d/bon-appetit.yaml      # the surrounding sequence, so
//	  - !include conf.d/stevenblack.yaml      # several list files concatenate
//
// Paths are relative to the including file's directory. Includes may nest.
const includeTag = "!include"

const maxIncludeDepth = 20

// ResolveIncludesBytes parses YAML, expands its !include directives relative to
// baseDir, and re-marshals the result. Callers that need a strict decode (e.g.
// configcheck's KnownFields check) resolve first, then decode the bytes.
func ResolveIncludesBytes(data []byte, baseDir string) ([]byte, error) {
	var root yaml.Node
	if err := yaml.Unmarshal(data, &root); err != nil {
		return nil, fmt.Errorf("config: parsing: %w", err)
	}
	if err := resolveIncludes(&root, baseDir, 0); err != nil {
		return nil, err
	}
	return yaml.Marshal(&root)
}

// resolveIncludes walks a parsed YAML tree and expands every !include node in
// place. baseDir is the directory of the file that produced this node, so
// relative include paths resolve against it.
func resolveIncludes(node *yaml.Node, baseDir string, depth int) error {
	if depth > maxIncludeDepth {
		return fmt.Errorf("config: !include nested deeper than %d — a cycle?", maxIncludeDepth)
	}

	for i := 0; i < len(node.Content); {
		child := node.Content[i]

		if child.Tag != includeTag {
			if err := resolveIncludes(child, baseDir, depth); err != nil {
				return err
			}
			i++
			continue
		}

		loaded, err := loadInclude(child.Value, baseDir, depth)
		if err != nil {
			return err
		}

		// Inside a sequence, a sequence-valued include is flattened (its items
		// spliced in) and an empty include contributes nothing — so a list of
		// !include lines concatenates cleanly. Anything else here is a mistake
		// (e.g. a mapping file where a list was expected) and is a hard error
		// rather than a silently-blank list entry.
		if node.Kind == yaml.SequenceNode {
			switch {
			case loaded.Kind == yaml.SequenceNode:
				spliced := append([]*yaml.Node{}, node.Content[:i]...)
				spliced = append(spliced, loaded.Content...)
				spliced = append(spliced, node.Content[i+1:]...)
				node.Content = spliced
				i += len(loaded.Content)
				continue
			case isEmptyNode(loaded):
				node.Content = append(node.Content[:i], node.Content[i+1:]...)
				continue
			default:
				return fmt.Errorf("config: !include %q inside a list must contain a list, got %s", child.Value, kindName(loaded.Kind))
			}
		}

		*child = *loaded
		i++
	}
	return nil
}

// loadInclude reads and fully resolves one included file, returning its content
// node (the value below the YAML document wrapper).
func loadInclude(rel, baseDir string, depth int) (*yaml.Node, error) {
	path := rel
	if !filepath.IsAbs(path) {
		path = filepath.Join(baseDir, rel)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("config: !include %q: %w", rel, err)
	}

	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("config: parsing !include %q: %w", rel, err)
	}

	content := &doc
	if doc.Kind == yaml.DocumentNode && len(doc.Content) == 1 {
		content = doc.Content[0]
	}

	// An included file may itself use !include, resolved relative to its own dir.
	if err := resolveIncludes(content, filepath.Dir(path), depth+1); err != nil {
		return nil, err
	}
	return content, nil
}

// kindName gives a readable name for a YAML node kind, for error messages.
func kindName(k yaml.Kind) string {
	switch k {
	case yaml.SequenceNode:
		return "a list"
	case yaml.MappingNode:
		return "a mapping"
	case yaml.ScalarNode:
		return "a scalar"
	case yaml.DocumentNode:
		return "a document"
	default:
		return "an empty value"
	}
}

// isEmptyNode reports whether a node carries no value (an empty file, or an
// explicit null) — used so an empty include drops out of a sequence.
func isEmptyNode(n *yaml.Node) bool {
	if n == nil || n.Kind == 0 {
		return true
	}
	if n.Kind == yaml.DocumentNode && len(n.Content) == 0 {
		return true
	}
	return n.Kind == yaml.ScalarNode && (n.Tag == "!!null" || n.Value == "")
}
