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

	// Marshal back to YAML
	out, err := yaml.Marshal(&doc)
	if err != nil {
		return fmt.Errorf("marshaling config YAML: %w", err)
	}

	// Write atomically: temp file in same directory, then rename
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

	// Match ownership and permissions of original file
	if info, err := os.Stat(GlockerConfigFile); err == nil {
		os.Chmod(tmpPath, info.Mode())
	}

	// Remove immutable flag from config file
	exec.Command("chattr", "-i", GlockerConfigFile).Run()

	// Atomic rename
	if err := os.Rename(tmpPath, GlockerConfigFile); err != nil {
		os.Remove(tmpPath)
		// Re-set immutable flag even on failure
		exec.Command("chattr", "+i", GlockerConfigFile).Run()
		return fmt.Errorf("renaming temp file to config: %w", err)
	}

	// Re-set immutable flag
	exec.Command("chattr", "+i", GlockerConfigFile).Run()

	log.Printf("Saved %d new domains to %s", added, GlockerConfigFile)
	return nil
}
