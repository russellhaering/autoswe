package mcp

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/russellhaering/autoswe/pkg/tools"
)

// IndexEntry catalogs one MCP-advertised tool we know about but haven't yet
// surfaced to the model. ToolSearch returns matches; on match the entry is
// promoted into the agent's tools.Registry.
type IndexEntry struct {
	Server       string
	Tool         Tool
	ExposedName  string // server-prefixed name registered into the agent
}

// Index is the searchable catalog of MCP tools across all configured servers.
type Index struct {
	mu       sync.RWMutex
	entries  []IndexEntry
	clients  map[string]Client // server name → client
	prefixed bool
}

// NewIndex builds an index by initializing each server in cfg and listing its
// tools. Errors connecting to or listing tools from individual servers are
// returned as a joined error but other servers continue.
func NewIndex(ctx context.Context, cfg ConfigFile, prefixed bool) (*Index, error) {
	idx := &Index{clients: map[string]Client{}, prefixed: prefixed}
	var errs []string
	for name, srv := range cfg.MCPServers {
		if srv.Command == "" {
			if srv.URL != "" {
				errs = append(errs, fmt.Sprintf("%s: HTTP MCP transport not yet implemented", name))
				continue
			}
			errs = append(errs, fmt.Sprintf("%s: no command and no url", name))
			continue
		}
		client, err := NewStdioClient(ctx, StdioConfig{
			Name:    name,
			Command: srv.Command,
			Args:    srv.Args,
			Env:     srv.Env,
		})
		if err != nil {
			errs = append(errs, fmt.Sprintf("%s: %v", name, err))
			continue
		}
		if err := client.Initialize(ctx); err != nil {
			_ = client.Close()
			errs = append(errs, fmt.Sprintf("%s: %v", name, err))
			continue
		}
		toolList, err := client.ListTools(ctx)
		if err != nil {
			_ = client.Close()
			errs = append(errs, fmt.Sprintf("%s: %v", name, err))
			continue
		}
		idx.clients[name] = client
		for _, t := range toolList {
			exposed := t.Name
			if prefixed {
				exposed = name + "__" + t.Name
			}
			idx.entries = append(idx.entries, IndexEntry{
				Server:      name,
				Tool:        t,
				ExposedName: exposed,
			})
		}
	}
	if len(errs) > 0 {
		return idx, fmt.Errorf("mcp: server errors: %s", strings.Join(errs, "; "))
	}
	return idx, nil
}

// Close releases all underlying clients.
func (idx *Index) Close() error {
	idx.mu.Lock()
	defer idx.mu.Unlock()
	for _, c := range idx.clients {
		_ = c.Close()
	}
	return nil
}

// Entries returns a snapshot of all indexed entries.
func (idx *Index) Entries() []IndexEntry {
	idx.mu.RLock()
	defer idx.mu.RUnlock()
	out := make([]IndexEntry, len(idx.entries))
	copy(out, idx.entries)
	return out
}

// Search returns entries whose name or description contains all space-separated
// terms in query (case-insensitive). An empty query returns everything.
func (idx *Index) Search(query string) []IndexEntry {
	terms := strings.Fields(strings.ToLower(query))
	idx.mu.RLock()
	defer idx.mu.RUnlock()
	if len(terms) == 0 {
		out := make([]IndexEntry, len(idx.entries))
		copy(out, idx.entries)
		return out
	}
	var matches []IndexEntry
	for _, e := range idx.entries {
		hay := strings.ToLower(e.ExposedName + " " + e.Tool.Description)
		ok := true
		for _, term := range terms {
			if !strings.Contains(hay, term) {
				ok = false
				break
			}
		}
		if ok {
			matches = append(matches, e)
		}
	}
	return matches
}

// Promote registers the given entries' tools into target. Idempotent — entries
// already registered are skipped.
func (idx *Index) Promote(target *tools.Registry, entries []IndexEntry) []string {
	var promoted []string
	for _, e := range entries {
		if _, exists := target.Get(e.ExposedName); exists {
			continue
		}
		client := idx.clients[e.Server]
		if client == nil {
			continue
		}
		tool := NewTool(client, e.Tool, idx.prefixed)
		if err := target.Register(tool); err == nil {
			promoted = append(promoted, e.ExposedName)
		}
	}
	return promoted
}
