// Copyright IBM Corp. 2014, 2026
// SPDX-License-Identifier: BUSL-1.1

package migrate

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclsyntax"
	"github.com/hashicorp/hcl/v2/hclwrite"
	"github.com/hashicorp/terraform/internal/ast"
	"github.com/zclconf/go-cty/cty"
)

// Migration represents a single JSON migration file.
type Migration struct {
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Match       Match    `json:"match"`
	Actions     []Action `json:"actions"`
}

// Match specifies which blocks to target.
type Match struct {
	BlockType string `json:"block_type"`
	Label     string `json:"label"`
}

// Action describes a single mutation to apply to matched blocks.
type Action struct {
	Action        string `json:"action"`
	From          string `json:"from,omitempty"`
	To            string `json:"to,omitempty"`
	Name          string `json:"name,omitempty"`
	Text          string `json:"text,omitempty"`
	Value         string `json:"value,omitempty"`
	OldValue      string `json:"old_value,omitempty"`
	NewValue      string `json:"new_value,omitempty"`
	BlockName     string `json:"block_name,omitempty"`
	WireAttribute string `json:"wire_attribute,omitempty"`
	WireTraversal string `json:"wire_traversal,omitempty"`
}

// ParseMigration parses a JSON migration file.
func ParseMigration(data []byte) (*Migration, error) {
	var m Migration
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("parsing migration JSON: %w", err)
	}
	if err := m.validate(); err != nil {
		return nil, err
	}
	return &m, nil
}

func (m *Migration) validate() error {
	if m.Name == "" {
		return fmt.Errorf("migration missing required field \"name\"")
	}
	if m.Match.BlockType == "" {
		return fmt.Errorf("migration %q: match missing required field \"block_type\"", m.Name)
	}
	if len(m.Actions) == 0 {
		return fmt.Errorf("migration %q: must have at least one action", m.Name)
	}
	for i, a := range m.Actions {
		if err := a.validate(); err != nil {
			return fmt.Errorf("migration %q action %d: %w", m.Name, i, err)
		}
	}
	return nil
}

var validActions = map[string]bool{
	"rename_attribute":       true,
	"remove_attribute":       true,
	"rename_resource":        true,
	"add_comment":            true,
	"set_attribute_value":    true,
	"add_attribute":          true,
	"replace_value":          true,
	"extract_to_resource":    true,
	"move_attribute_to_block": true,
	"flatten_block":          true,
	"remove_resource":        true,
}

func (a *Action) validate() error {
	if !validActions[a.Action] {
		return fmt.Errorf("unknown action %q", a.Action)
	}
	switch a.Action {
	case "rename_attribute":
		if a.From == "" || a.To == "" {
			return fmt.Errorf("rename_attribute requires \"from\" and \"to\"")
		}
	case "remove_attribute":
		if a.Name == "" {
			return fmt.Errorf("remove_attribute requires \"name\"")
		}
	case "rename_resource":
		if a.To == "" {
			return fmt.Errorf("rename_resource requires \"to\"")
		}
	case "add_comment":
		if a.Text == "" {
			return fmt.Errorf("add_comment requires \"text\"")
		}
	case "set_attribute_value":
		if a.Name == "" || a.Value == "" {
			return fmt.Errorf("set_attribute_value requires \"name\" and \"value\"")
		}
	case "add_attribute":
		if a.Name == "" || a.Value == "" {
			return fmt.Errorf("add_attribute requires \"name\" and \"value\"")
		}
	case "replace_value":
		if a.Name == "" || a.OldValue == "" || a.NewValue == "" {
			return fmt.Errorf("replace_value requires \"name\", \"old_value\", and \"new_value\"")
		}
	case "extract_to_resource":
		if a.Name == "" || a.To == "" {
			return fmt.Errorf("extract_to_resource requires \"name\" (nested block) and \"to\" (new resource type)")
		}
	case "move_attribute_to_block":
		if a.Name == "" || a.BlockName == "" || a.To == "" {
			return fmt.Errorf("move_attribute_to_block requires \"name\", \"block_name\", and \"to\"")
		}
	case "flatten_block":
		if a.Name == "" {
			return fmt.Errorf("flatten_block requires \"name\"")
		}
	case "remove_resource":
		if a.Text == "" {
			return fmt.Errorf("remove_resource requires \"text\" (FIXME comment)")
		}
	}
	return nil
}

// DiscoverMigrations recursively finds all *.json files under dir,
// parses them as migrations, and returns them sorted by name.
func DiscoverMigrations(dir string) ([]*Migration, error) {
	var migrations []*Migration

	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() || filepath.Ext(path) != ".json" {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("reading %s: %w", path, err)
		}
		m, err := ParseMigration(data)
		if err != nil {
			return fmt.Errorf("%s: %w", path, err)
		}
		migrations = append(migrations, m)
		return nil
	})
	if err != nil {
		return nil, err
	}

	sort.Slice(migrations, func(i, j int) bool {
		return migrations[i].Name < migrations[j].Name
	})
	return migrations, nil
}

// FilterMigrations returns only migrations whose name matches the glob pattern.
// An empty pattern matches all migrations.
func FilterMigrations(migrations []*Migration, pattern string) []*Migration {
	if pattern == "" {
		return migrations
	}
	var result []*Migration
	for _, m := range migrations {
		matched, err := filepath.Match(pattern, m.Name)
		if err != nil {
			continue
		}
		if matched {
			result = append(result, m)
		}
	}
	return result
}

// Execute applies a migration to all matching blocks in the module.
func Execute(m *Migration, mod *ast.Module) error {
	results := mod.FindBlocks(m.Match.BlockType, m.Match.Label)

	for _, r := range results {
		for _, action := range m.Actions {
			if err := executeAction(action, r, mod); err != nil {
				return fmt.Errorf("migration %q: %w", m.Name, err)
			}
		}
	}
	return nil
}

func executeAction(a Action, r *ast.BlockResult, mod *ast.Module) error {
	switch a.Action {
	case "rename_attribute":
		r.Block.RenameAttribute(a.From, a.To)

	case "remove_attribute":
		r.Block.RemoveAttribute(a.Name)

	case "rename_resource":
		oldLabels := r.Block.Labels()
		if len(oldLabels) == 0 {
			return fmt.Errorf("rename_resource: block has no labels")
		}
		oldType := oldLabels[0]
		newLabels := make([]string, len(oldLabels))
		copy(newLabels, oldLabels)
		newLabels[0] = a.To
		r.Block.SetLabels(newLabels)

		// Rename references across entire module
		oldTraversal := hcl.Traversal{hcl.TraverseRoot{Name: oldType}}
		newTraversal := hcl.Traversal{hcl.TraverseRoot{Name: a.To}}
		mod.RenameReferencePrefix(oldTraversal, newTraversal)

	case "add_comment":
		r.File.AppendComment(a.Text)

	case "set_attribute_value":
		val, err := parseValue(a.Value)
		if err != nil {
			return fmt.Errorf("set_attribute_value: %w", err)
		}
		r.Block.SetAttributeValue(a.Name, val)

	case "add_attribute":
		if r.Block.HasAttribute(a.Name) {
			return nil // already present, skip
		}
		val, err := parseValue(a.Value)
		if err != nil {
			return fmt.Errorf("add_attribute: %w", err)
		}
		r.Block.SetAttributeValue(a.Name, val)

	case "replace_value":
		expr := r.Block.GetAttributeExpression(a.Name)
		if expr == nil {
			return nil // attribute not present, skip
		}
		got := strings.TrimSpace(string(expr.BuildTokens(nil).Bytes()))
		if got == a.OldValue {
			tokens := hclwrite.Tokens{
				{Type: hclsyntax.TokenIdent, Bytes: []byte(a.NewValue)},
			}
			r.Block.SetAttributeRaw(a.Name, tokens)
		}

	case "extract_to_resource":
		labels := r.Block.Labels()
		if len(labels) < 2 {
			return fmt.Errorf("extract_to_resource: block needs at least 2 labels")
		}

		nested := r.Block.NestedBlocks(a.Name)
		if len(nested) == 0 {
			// Check if it's an attribute instead of a nested block
			expr := r.Block.GetAttributeExpression(a.Name)
			if expr == nil {
				return nil
			}
			tokens := expr.BuildTokens(nil)

			// Remove from parent
			r.Block.RemoveAttribute(a.Name)

			// Create new resource with same instance name
			newBlock := r.File.AddBlock("resource", []string{a.To, labels[1]})

			// Add wiring attribute if specified
			if a.WireAttribute != "" {
				suffix := a.WireTraversal
				if suffix == "" {
					suffix = "id"
				}
				newBlock.SetAttributeTraversal(a.WireAttribute, hcl.Traversal{
					hcl.TraverseRoot{Name: labels[0]},
					hcl.TraverseAttr{Name: labels[1]},
					hcl.TraverseAttr{Name: suffix},
				})
			}

			// Set the attribute on the new resource
			newBlock.SetAttributeRaw(a.Name, tokens)
			return nil
		}

		// Read attributes from the nested block
		attrs := nested[0].Attributes()

		// Remove the nested block from the parent
		r.Block.RemoveBlock(a.Name)

		// Create new resource with same instance name
		newBlock := r.File.AddBlock("resource", []string{a.To, labels[1]})

		// Add wiring attribute if specified
		if a.WireAttribute != "" {
			suffix := a.WireTraversal
			if suffix == "" {
				suffix = "id"
			}
			newBlock.SetAttributeTraversal(a.WireAttribute, hcl.Traversal{
				hcl.TraverseRoot{Name: labels[0]},
				hcl.TraverseAttr{Name: labels[1]},
				hcl.TraverseAttr{Name: suffix},
			})
		}

		// Copy attributes from the extracted nested block
		for name, expr := range attrs {
			newBlock.SetAttributeRaw(name, expr.BuildTokens(nil))
		}

		// Copy nested blocks from the extracted block
		copyNestedBlocks(nested[0], newBlock)

	case "move_attribute_to_block":
		expr := r.Block.GetAttributeExpression(a.Name)
		if expr == nil {
			return nil
		}
		tokens := expr.BuildTokens(nil)

		// Remove from parent
		r.Block.RemoveAttribute(a.Name)

		// Find or create the target nested block
		existing := r.Block.NestedBlocks(a.BlockName)
		var target *ast.Block
		if len(existing) > 0 {
			target = existing[0]
		} else {
			target = r.Block.AddBlock(a.BlockName)
		}

		// Set the attribute with the new name
		target.SetAttributeRaw(a.To, tokens)

	case "flatten_block":
		nested := r.Block.NestedBlocks(a.Name)
		if len(nested) == 0 {
			return nil
		}
		// Read all attributes from the nested block
		attrs := nested[0].Attributes()

		// Remove the nested block
		r.Block.RemoveBlock(a.Name)

		// Add each attribute to the parent block
		for name, expr := range attrs {
			r.Block.SetAttributeRaw(name, expr.BuildTokens(nil))
		}

	case "remove_resource":
		labels := r.Block.Labels()
		if len(labels) == 0 {
			return fmt.Errorf("remove_resource: block has no labels")
		}

		// Find files that reference this resource and add FIXME comments
		prefix := hcl.Traversal{hcl.TraverseRoot{Name: labels[0]}}
		warned := make(map[string]bool)
		for _, f := range mod.Files() {
			if f.ReferencesPrefix(prefix) && !warned[f.Filename()] {
				f.AppendComment(a.Text)
				warned[f.Filename()] = true
			}
		}

		// Remove the resource block
		r.File.RemoveBlock(r.Block.Type(), labels)

	default:
		return fmt.Errorf("unknown action %q", a.Action)
	}
	return nil
}

// copyNestedBlocks recursively copies all child blocks from src to dst.
func copyNestedBlocks(src, dst *ast.Block) {
	for _, child := range src.AllNestedBlocks() {
		newChild := dst.AddBlock(child.Type())
		for name, expr := range child.Attributes() {
			newChild.SetAttributeRaw(name, expr.BuildTokens(nil))
		}
		copyNestedBlocks(child, newChild)
	}
}

// parseValue converts a string to a cty.Value.
// Supports: "true"/"false" -> cty.BoolVal, integers -> cty.NumberIntVal,
// everything else -> cty.StringVal.
func parseValue(s string) (cty.Value, error) {
	if s == "true" {
		return cty.True, nil
	}
	if s == "false" {
		return cty.False, nil
	}
	var n int64
	if _, err := fmt.Sscanf(s, "%d", &n); err == nil {
		return cty.NumberIntVal(n), nil
	}
	return cty.StringVal(s), nil
}
