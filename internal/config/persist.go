package config

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"

	"gopkg.in/yaml.v3"
)

// validDomainRe matches valid domain names: alphanumeric, dots, hyphens.
var validDomainRe = regexp.MustCompile(`^[a-zA-Z0-9]([a-zA-Z0-9\-]*[a-zA-Z0-9])?(\.[a-zA-Z0-9]([a-zA-Z0-9\-]*[a-zA-Z0-9])?)*$`)

// ValidateDomainName checks that a domain name contains only safe characters.
func ValidateDomainName(name string) error {
	if name == "" {
		return ErrEmptyDomainName
	}
	if len(name) > 253 {
		return fmt.Errorf("domain name too long: %d characters (max 253)", len(name))
	}
	if !validDomainRe.MatchString(name) {
		return fmt.Errorf("invalid domain name %q: must contain only alphanumeric characters, dots, and hyphens", name)
	}
	return nil
}

// ValidateProgramName checks that a forbidden-program name is safe to embed in
// the socket protocol (colon/comma are delimiters) and the YAML config. Program
// names are matched as a case-insensitive substring against each process's
// `comm`, so we only need to keep out control characters and our delimiters.
func ValidateProgramName(name string) error {
	if name == "" {
		return fmt.Errorf("program name cannot be empty")
	}
	if len(name) > 64 {
		return fmt.Errorf("program name too long: %d characters (max 64)", len(name))
	}
	for _, r := range name {
		if r < 0x20 || r == 0x7f {
			return fmt.Errorf("invalid program name %q: contains a control character", name)
		}
		if r == ':' || r == ',' {
			return fmt.Errorf("invalid program name %q: ':' and ',' are not allowed", name)
		}
	}
	return nil
}

// SaveForbiddenProgramsToConfig appends new forbidden programs (by name, no
// windows = killed always) to the config file on disk. Mirrors
// SaveDomainsToConfig: uses the yaml.v3 Node API to preserve comments and
// formatting, dedupes by name, and writes atomically around the chattr +i flag.
func SaveForbiddenProgramsToConfig(names []string) error {
	for _, n := range names {
		if err := ValidateProgramName(n); err != nil {
			return fmt.Errorf("refusing to save: %w", err)
		}
	}

	data, err := os.ReadFile(GlockerConfigFile)
	if err != nil {
		return fmt.Errorf("reading config file: %w", err)
	}

	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return fmt.Errorf("parsing config YAML: %w", err)
	}

	if doc.Kind != yaml.DocumentNode || len(doc.Content) == 0 {
		return fmt.Errorf("unexpected YAML structure: expected document node")
	}
	root := doc.Content[0]
	if root.Kind != yaml.MappingNode {
		return fmt.Errorf("unexpected YAML structure: expected mapping node at root")
	}

	// Find (or create) the "forbidden_programs" mapping.
	fpMap := findMapValue(root, "forbidden_programs")
	if fpMap == nil {
		keyNode := &yaml.Node{Kind: yaml.ScalarNode, Value: "forbidden_programs", Tag: "!!str"}
		fpMap = &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
		root.Content = append(root.Content, keyNode, fpMap)
	}
	if fpMap.Kind != yaml.MappingNode {
		return fmt.Errorf("unexpected YAML structure: forbidden_programs is not a mapping")
	}

	// Find (or create) the "programs" sequence inside forbidden_programs.
	programsSeq := findMapValue(fpMap, "programs")
	if programsSeq == nil {
		keyNode := &yaml.Node{Kind: yaml.ScalarNode, Value: "programs", Tag: "!!str"}
		programsSeq = &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
		fpMap.Content = append(fpMap.Content, keyNode, programsSeq)
	}
	if programsSeq.Kind != yaml.SequenceNode {
		return fmt.Errorf("unexpected YAML structure: programs is not a sequence")
	}

	// Build a set of existing program names to avoid duplicates.
	existing := make(map[string]bool, len(programsSeq.Content))
	for _, node := range programsSeq.Content {
		if node.Kind == yaml.MappingNode {
			if nameVal := findMapValue(node, "name"); nameVal != nil {
				existing[nameVal.Value] = true
			}
		}
	}

	// Append new programs using flow style to match: {name: "steam"}
	added := 0
	for _, n := range names {
		if existing[n] {
			log.Printf("Forbidden program %s already in config, skipping", n)
			continue
		}

		mapNode := &yaml.Node{
			Kind:  yaml.MappingNode,
			Tag:   "!!map",
			Style: yaml.FlowStyle,
			Content: []*yaml.Node{
				{Kind: yaml.ScalarNode, Value: "name", Tag: "!!str"},
				{Kind: yaml.ScalarNode, Value: n, Tag: "!!str"},
			},
		}
		programsSeq.Content = append(programsSeq.Content, mapNode)
		existing[n] = true
		added++
	}

	if added == 0 {
		log.Printf("No new forbidden programs to save (all already in config)")
		return nil
	}

	if err := writeConfigAtomic(&doc); err != nil {
		return err
	}

	log.Printf("Saved %d new forbidden program(s) to %s", added, GlockerConfigFile)
	return nil
}

// findMapValue returns the value node for the given key in a mapping node, or
// nil if the mapping doesn't contain it.
func findMapValue(mapNode *yaml.Node, key string) *yaml.Node {
	if mapNode.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i < len(mapNode.Content)-1; i += 2 {
		if mapNode.Content[i].Value == key {
			return mapNode.Content[i+1]
		}
	}
	return nil
}

// writeConfigAtomic marshals the YAML document and writes it to the config file
// atomically, handling the chattr +i immutable flag around the rename.
func writeConfigAtomic(doc *yaml.Node) error {
	out, err := yaml.Marshal(doc)
	if err != nil {
		return fmt.Errorf("marshaling config YAML: %w", err)
	}

	dir := filepath.Dir(GlockerConfigFile)
	tmp, err := os.CreateTemp(dir, "config-*.yaml.tmp")
	if err != nil {
		return fmt.Errorf("creating temp file: %w", err)
	}
	tmpPath := tmp.Name()

	if _, err := tmp.Write(out); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return fmt.Errorf("writing temp file: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return fmt.Errorf("syncing temp file: %w", err)
	}
	tmp.Close()

	if info, err := os.Stat(GlockerConfigFile); err == nil {
		os.Chmod(tmpPath, info.Mode())
	}

	exec.Command("chattr", "-i", GlockerConfigFile).Run()

	if err := os.Rename(tmpPath, GlockerConfigFile); err != nil {
		os.Remove(tmpPath)
		exec.Command("chattr", "+i", GlockerConfigFile).Run()
		return fmt.Errorf("renaming temp file to config: %w", err)
	}

	exec.Command("chattr", "+i", GlockerConfigFile).Run()
	return nil
}

// SaveDomainsToConfig appends new domains to the config file on disk.
// Uses yaml.v3 Node API to preserve existing comments and formatting.
// Handles chattr +i on the config file and writes atomically.
func SaveDomainsToConfig(domains []string) error {
	// Validate all domain names before touching the file
	for _, d := range domains {
		if err := ValidateDomainName(d); err != nil {
			return fmt.Errorf("refusing to save: %w", err)
		}
	}

	// Read the raw config file
	data, err := os.ReadFile(GlockerConfigFile)
	if err != nil {
		return fmt.Errorf("reading config file: %w", err)
	}

	// Parse into a yaml.Node tree to preserve structure
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return fmt.Errorf("parsing config YAML: %w", err)
	}

	// doc is a Document node; its first child is the root mapping
	if doc.Kind != yaml.DocumentNode || len(doc.Content) == 0 {
		return fmt.Errorf("unexpected YAML structure: expected document node")
	}
	root := doc.Content[0]
	if root.Kind != yaml.MappingNode {
		return fmt.Errorf("unexpected YAML structure: expected mapping node at root")
	}

	// Find the "domains" key in the root mapping
	var domainsSeq *yaml.Node
	for i := 0; i < len(root.Content)-1; i += 2 {
		if root.Content[i].Value == "domains" {
			domainsSeq = root.Content[i+1]
			break
		}
	}

	if domainsSeq == nil {
		// No domains key exists yet — create one
		keyNode := &yaml.Node{Kind: yaml.ScalarNode, Value: "domains", Tag: "!!str"}
		seqNode := &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
		root.Content = append(root.Content, keyNode, seqNode)
		domainsSeq = seqNode
	}

	if domainsSeq.Kind != yaml.SequenceNode {
		return fmt.Errorf("unexpected YAML structure: domains is not a sequence")
	}

	// Build a set of existing domain names to avoid duplicates
	existing := make(map[string]bool, len(domainsSeq.Content))
	for _, node := range domainsSeq.Content {
		if node.Kind == yaml.MappingNode {
			for j := 0; j < len(node.Content)-1; j += 2 {
				if node.Content[j].Value == "name" {
					existing[node.Content[j+1].Value] = true
					break
				}
			}
		}
	}

	// Append new domains using flow style to match: {name: "example.com"}
	added := 0
	for _, d := range domains {
		if existing[d] {
			log.Printf("Domain %s already in config, skipping", d)
			continue
		}

		mapNode := &yaml.Node{
			Kind:  yaml.MappingNode,
			Tag:   "!!map",
			Style: yaml.FlowStyle,
			Content: []*yaml.Node{
				{Kind: yaml.ScalarNode, Value: "name", Tag: "!!str"},
				{Kind: yaml.ScalarNode, Value: d, Tag: "!!str"},
			},
		}
		domainsSeq.Content = append(domainsSeq.Content, mapNode)
		existing[d] = true
		added++
	}

	if added == 0 {
		log.Printf("No new domains to save (all already in config)")
		return nil
	}

	if err := writeConfigAtomic(&doc); err != nil {
		return err
	}

	log.Printf("Saved %d new domains to %s", added, GlockerConfigFile)
	return nil
}
