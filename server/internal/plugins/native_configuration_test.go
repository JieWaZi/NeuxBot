package plugins_test

import (
	"github.com/JieWaZi/NeuxBot/server/internal/plugins"
	"os"
	"path/filepath"
	"testing"
)

func TestStandardConfigurationIsExplicitAndSecretsStaySeparate(t *testing.T) {
	root := t.TempDir()
	manifest := `{"$schema":"https://agent-plugins.org/schemas/1.0.0/plugin.schema.json","name":"configured","extensions":{"com.neuxbot":{"schemaVersion":1,"configuration":{"fields":[{"key":"TOKEN","label":"API token","secret":true,"required":true}],"bindings":[{"server":"remote","target":"header","name":"Authorization","value":"Bearer ${TOKEN}"}]}}}}`
	if err := os.WriteFile(filepath.Join(root, "plugin.json"), []byte(manifest), 0600); err != nil {
		t.Fatal(err)
	}
	mcp := `{"$schema":"https://agent-plugins.org/schemas/1.0.0/mcp.schema.json","mcpServers":{"remote":{"type":"streamable-http","url":"https://example.com/mcp"}}}`
	if err := os.WriteFile(filepath.Join(root, "mcp.json"), []byte(mcp), 0600); err != nil {
		t.Fatal(err)
	}
	pkg, err := plugins.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := plugins.ValidateValues(pkg, nil, nil); err == nil {
		t.Fatal("required secret missing")
	}
	if err := plugins.ValidateValues(pkg, map[string]string{"TOKEN": "fictional"}, nil); err == nil {
		t.Fatal("secret accepted in public values")
	}
	if err := plugins.ValidateValues(pkg, nil, map[string]string{"TOKEN": "fictional"}); err != nil {
		t.Fatal(err)
	}
}
