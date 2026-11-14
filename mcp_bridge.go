package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/mcp"
)

// MCPServerConfig describes an external MCP server executable reachable via stdio.
type MCPServerConfig struct {
	Name    string
	Command string
	Args    []string
	Env     []string
}

// MCPArticle represents a single structured news item extracted by mcp-web-actor.
type MCPArticle struct {
	Title       string `json:"title"`
	URL         string `json:"url"`
	Summary     string `json:"summary"`
	Author      string `json:"author"`
	PublishedAt string `json:"published_at"`
	Category    string `json:"category"`
	Image       string `json:"image"`
	SourceLayer string `json:"source_layer"`
}

// MCPExtractedPage mirrors the JSON payload returned by extract_structured_news.
type MCPExtractedPage struct {
	PageURL        string       `json:"page_url"`
	PageTitle      string       `json:"page_title"`
	SiteName       string       `json:"site_name"`
	RSSURL         string       `json:"rss_url"`
	StrategiesUsed []string     `json:"strategies_used"`
	TotalExtracted int          `json:"total_extracted"`
	Articles       []MCPArticle `json:"articles"`
}

// MCPBridge manages persistent stdio MCP client connections so timeline-token
// can invoke mcp-web-actor and future MCP servers cleanly.
type MCPBridge struct {
	mu          sync.Mutex
	configs     map[string]MCPServerConfig
	clients     map[string]*client.Client
	webActorBin string
}

func isExecutableFile(path string) bool {
	if strings.TrimSpace(path) == "" {
		return false
	}
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return false
	}
	return info.Mode()&0111 != 0
}

// ResolveWebActorBinary locates the mcp-web-actor binary using priority order:
// explicit flag -> MCP_WEB_ACTOR_BIN env -> /home/jose/go/bin/mcp-web-actor -> PATH -> sibling fallback.
func ResolveWebActorBinary(explicit string) string {
	candidates := []string{
		strings.TrimSpace(explicit),
		strings.TrimSpace(os.Getenv("MCP_WEB_ACTOR_BIN")),
		"/home/jose/go/bin/mcp-web-actor",
	}

	if lp, err := exec.LookPath("mcp-web-actor"); err == nil {
		candidates = append(candidates, lp)
	}

	candidates = append(candidates,
		filepath.Clean("../mcp-webdriverio/mcp-server"),
		"/home/jose/workspace/sandbox/gemini/mcp-webdriverio/mcp-server",
	)

	for _, c := range candidates {
		if isExecutableFile(c) {
			return c
		}
	}
	return ""
}

// NewMCPBridge initializes an MCPBridge with the default "web-actor" server registered.
func NewMCPBridge(webActorBin string) (*MCPBridge, error) {
	resolved := ResolveWebActorBinary(webActorBin)
	if resolved == "" {
		if strings.TrimSpace(webActorBin) != "" {
			return nil, fmt.Errorf("mcp-web-actor binary not found or not executable at %q", webActorBin)
		}
		return nil, errors.New("mcp-web-actor binary not found (run mcp-webdriverio/start-services.sh or set MCP_WEB_ACTOR_BIN)")
	}

	b := &MCPBridge{
		configs:     make(map[string]MCPServerConfig),
		clients:     make(map[string]*client.Client),
		webActorBin: resolved,
	}
	b.RegisterServer(MCPServerConfig{
		Name:    "web-actor",
		Command: resolved,
	})
	return b, nil
}

// WebActorPath returns the resolved path for the mcp-web-actor binary.
func (b *MCPBridge) WebActorPath() string {
	if b == nil {
		return ""
	}
	return b.webActorBin
}

// RegisterServer adds or updates a named MCP server definition for future extensions.
func (b *MCPBridge) RegisterServer(cfg MCPServerConfig) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.configs[cfg.Name] = cfg
}

func (b *MCPBridge) connectLocked(ctx context.Context, serverName string) (*client.Client, error) {
	cfg, ok := b.configs[serverName]
	if !ok {
		return nil, fmt.Errorf("MCP server %q not registered", serverName)
	}

	c, err := client.NewStdioMCPClient(cfg.Command, cfg.Env, cfg.Args...)
	if err != nil {
		return nil, fmt.Errorf("launching MCP server %q (%s): %w", serverName, cfg.Command, err)
	}

	initCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()

	initReq := mcp.InitializeRequest{}
	initReq.Params.ProtocolVersion = mcp.LATEST_PROTOCOL_VERSION
	initReq.Params.ClientInfo = mcp.Implementation{
		Name:    "timeline-token",
		Version: "1.0.0",
	}
	if _, err := c.Initialize(initCtx, initReq); err != nil {
		_ = c.Close()
		return nil, fmt.Errorf("initializing MCP server %q: %w", serverName, err)
	}

	b.clients[serverName] = c
	return c, nil
}

func (b *MCPBridge) getOrConnectLocked(ctx context.Context, serverName string) (*client.Client, error) {
	if c, ok := b.clients[serverName]; ok && c != nil {
		return c, nil
	}
	return b.connectLocked(ctx, serverName)
}

func (b *MCPBridge) closeClientLocked(serverName string) {
	if c, ok := b.clients[serverName]; ok && c != nil {
		_ = c.Close()
	}
	delete(b.clients, serverName)
}

func (b *MCPBridge) reconnectLocked(ctx context.Context, serverName string) (*client.Client, error) {
	b.closeClientLocked(serverName)
	return b.connectLocked(ctx, serverName)
}

func extractTextFromToolResult(toolName string, res *mcp.CallToolResult) (string, error) {
	if res == nil {
		return "", fmt.Errorf("tool %s returned nil result", toolName)
	}
	var texts []string
	for _, item := range res.Content {
		if tc, ok := mcp.AsTextContent(item); ok {
			texts = append(texts, tc.Text)
		}
	}
	joined := strings.Join(texts, "\n")
	if res.IsError {
		return "", fmt.Errorf("tool %s error: %s", toolName, joined)
	}
	return joined, nil
}

// CallTool executes an MCP tool on the named server, transparently connecting
// and reconnecting once if the underlying stdio subprocess dropped.
func (b *MCPBridge) CallTool(ctx context.Context, serverName, toolName string, args map[string]any) (string, error) {
	if b == nil {
		return "", errors.New("MCPBridge is nil")
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	c, err := b.getOrConnectLocked(ctx, serverName)
	if err != nil {
		return "", err
	}

	req := mcp.CallToolRequest{}
	req.Params.Name = toolName
	req.Params.Arguments = args

	res, err := c.CallTool(ctx, req)
	if err != nil {
		if ctx.Err() != nil {
			// Reset stuck browser session so subsequent portals start clean.
			b.closeClientLocked(serverName)
			return "", err
		}
		c, reconnErr := b.reconnectLocked(ctx, serverName)
		if reconnErr != nil {
			return "", fmt.Errorf("%v (reconnect failed: %w)", err, reconnErr)
		}
		res, err = c.CallTool(ctx, req)
		if err != nil {
			b.closeClientLocked(serverName)
			return "", err
		}
	}

	return extractTextFromToolResult(toolName, res)
}

// ExtractStructuredNews navigates targetURL with mcp-web-actor and returns
// deterministically extracted news items and optional RSS feed discovery.
func (b *MCPBridge) ExtractStructuredNews(ctx context.Context, targetURL string, maxItems int) (*MCPExtractedPage, error) {
	if maxItems <= 0 {
		maxItems = 25
	}
	callCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	raw, err := b.CallTool(callCtx, "web-actor", "extract_structured_news", map[string]any{
		"url":       targetURL,
		"max_items": maxItems,
	})
	if err != nil {
		return nil, err
	}

	var page MCPExtractedPage
	if err := json.Unmarshal([]byte(raw), &page); err != nil {
		return nil, fmt.Errorf("invalid JSON from extract_structured_news: %w", err)
	}
	return &page, nil
}

// Close terminates all active MCP client subprocesses cleanly.
func (b *MCPBridge) Close() error {
	if b == nil {
		return nil
	}
	b.mu.Lock()
	defer b.mu.Unlock()

	var firstErr error
	for name, c := range b.clients {
		if c != nil {
			if err := c.Close(); err != nil && firstErr == nil {
				firstErr = err
			}
		}
		delete(b.clients, name)
	}
	return firstErr
}

