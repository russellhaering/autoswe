package mcp

import (
	"encoding/json"
	"fmt"
	"os"
)

// ServerConfig describes one configured MCP server. Only the stdio transport
// is implemented in this phase; HTTP servers are accepted in config files for
// forward compatibility but produce a clear error at load time.
type ServerConfig struct {
	// Stdio fields:
	Command string            `json:"command,omitempty"`
	Args    []string          `json:"args,omitempty"`
	Env     map[string]string `json:"env,omitempty"`

	// HTTP fields (reserved):
	URL string `json:"url,omitempty"`
}

// ConfigFile is the top-level shape of an mcp-config JSON file:
//
//	{
//	  "mcpServers": {
//	    "filesystem": {"command":"npx","args":["-y","@modelcontextprotocol/server-filesystem","/tmp"]}
//	  }
//	}
type ConfigFile struct {
	MCPServers map[string]ServerConfig `json:"mcpServers"`
}

func LoadConfigFile(path string) (ConfigFile, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return ConfigFile{}, fmt.Errorf("mcp config %s: %w", path, err)
	}
	var cfg ConfigFile
	if err := json.Unmarshal(b, &cfg); err != nil {
		return ConfigFile{}, fmt.Errorf("mcp config %s parse: %w", path, err)
	}
	return cfg, nil
}
