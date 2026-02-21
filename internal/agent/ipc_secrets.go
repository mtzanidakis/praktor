package agent

import (
	"fmt"
	"log/slog"
	"strconv"
	"strings"

	"github.com/mtzanidakis/praktor/internal/config"
	"github.com/mtzanidakis/praktor/internal/container"
	"github.com/mtzanidakis/praktor/internal/extensions"
)

const secretRefPrefix = "secret:"

// resolveSecrets resolves secret:name references in env vars and prepares
// file secrets for injection into the container. Secrets are decrypted from
// the vault and never pass through the LLM conversation.
func (o *Orchestrator) resolveSecrets(opts *container.AgentOpts, agentID string, def config.AgentDefinition, hasDef bool) {
	if o.vault == nil || !hasDef {
		return
	}

	// Resolve secret:name references in env vars
	for k, v := range opts.Env {
		if !strings.HasPrefix(v, secretRefPrefix) {
			continue
		}
		secretName := strings.TrimPrefix(v, secretRefPrefix)
		plaintext, err := o.decryptSecret(secretName)
		if err != nil {
			slog.Warn("failed to resolve env secret", "agent", agentID, "env", k, "secret", secretName, "error", err)
			delete(opts.Env, k)
			continue
		}
		opts.Env[k] = string(plaintext)
	}

	// Prepare file secrets
	for _, fm := range def.Files {
		plaintext, err := o.decryptSecret(fm.Secret)
		if err != nil {
			slog.Warn("failed to resolve file secret", "agent", agentID, "secret", fm.Secret, "target", fm.Target, "error", err)
			continue
		}

		mode := int64(0o600)
		if fm.Mode != "" {
			if parsed, err := strconv.ParseInt(fm.Mode, 8, 64); err == nil {
				mode = parsed
			}
		}

		opts.SecretFiles = append(opts.SecretFiles, container.SecretFile{
			Content: plaintext,
			Target:  fm.Target,
			Mode:    mode,
		})
	}
}

func (o *Orchestrator) decryptSecret(name string) ([]byte, error) {
	sec, err := o.store.GetSecret(name)
	if err != nil {
		return nil, err
	}
	if sec == nil {
		return nil, fmt.Errorf("secret %q not found", name)
	}
	return o.vault.Decrypt(sec.Value, sec.Nonce)
}

// redactSecrets replaces any plaintext secret values found in content with
// [REDACTED]. This is a hard security barrier that prevents secret leakage
// regardless of LLM behavior. Only secrets with values >= 8 bytes are checked
// to avoid false positives with short strings.
//
// Secrets are collected from two sources:
// 1. DB agent_secrets assignments + global secrets
// 2. YAML config: secret:name env var refs + files section
func (o *Orchestrator) redactSecrets(agentID, content string) string {
	if o.vault == nil {
		return content
	}

	// Collect unique secret names to decrypt
	secretNames := make(map[string]bool)

	// Source 1: DB assignments (agent-specific + global)
	if secrets, err := o.store.GetAgentSecrets(agentID); err == nil {
		for _, sec := range secrets {
			secretNames[sec.ID] = true
		}
	}

	// Source 2: YAML config references
	if def, ok := o.registry.GetDefinition(agentID); ok {
		for _, v := range def.Env {
			if strings.HasPrefix(v, secretRefPrefix) {
				secretNames[strings.TrimPrefix(v, secretRefPrefix)] = true
			}
		}
		for _, fm := range def.Files {
			secretNames[fm.Secret] = true
		}
	}

	// Source 3: Extension MCP server env/header secret refs
	if extJSON, err := o.store.GetAgentExtensions(agentID); err == nil {
		if ext, err := extensions.Parse(extJSON); err == nil {
			for _, srv := range ext.MCPServers {
				for _, v := range srv.Env {
					if name, ok := strings.CutPrefix(v, secretRefPrefix); ok {
						secretNames[name] = true
					}
				}
				for _, v := range srv.Headers {
					if name, ok := strings.CutPrefix(v, secretRefPrefix); ok {
						secretNames[name] = true
					}
				}
			}
		}
	}

	// Decrypt and redact vault secrets
	for name := range secretNames {
		plaintext, err := o.decryptSecret(name)
		if err != nil || len(plaintext) < 8 {
			continue
		}
		content = redactValue(content, string(plaintext), name, agentID)
	}

	// Redact global credentials injected into all containers
	for _, v := range []string{o.cfg.OAuthToken, o.cfg.AnthropicAPIKey} {
		if len(v) >= 8 && strings.Contains(content, v) {
			slog.Warn("redacted credential from agent output", "agent", agentID)
			content = strings.ReplaceAll(content, v, "[REDACTED]")
		}
	}

	return content
}

// redactValue replaces a secret value in content. It tries exact match first,
// then falls back to whitespace-normalized matching to catch cases where the
// agent reformats the content (e.g. pretty-printing compact JSON or vice versa).
func redactValue(content, value, label, agentID string) string {
	// Exact match
	if strings.Contains(content, value) {
		slog.Warn("redacted secret from agent output", "agent", agentID, "secret", label)
		return strings.ReplaceAll(content, value, "[REDACTED]")
	}

	// Whitespace-normalized match: collapse all whitespace to single space
	normValue := collapseWhitespace(strings.TrimSpace(value))
	if len(normValue) < 8 {
		return content
	}
	normContent := collapseWhitespace(content)
	if strings.Contains(normContent, normValue) {
		slog.Warn("redacted secret from agent output (normalized)", "agent", agentID, "secret", label)
		// Rebuild content by finding the match boundaries in the normalized
		// form and mapping back to the original content
		return redactNormalized(content, normValue)
	}

	return content
}

// redactNormalized replaces regions in content that match normValue when
// whitespace is collapsed. It walks both strings in lockstep to map normalized
// match positions back to the original content.
func redactNormalized(content, normValue string) string {
	var result strings.Builder
	i := 0 // position in content
	for i < len(content) {
		if matchEnd := matchNormalizedAt(content, i, normValue); matchEnd >= 0 {
			result.WriteString("[REDACTED]")
			i = matchEnd
		} else {
			result.WriteByte(content[i])
			i++
		}
	}
	return result.String()
}

// matchNormalizedAt checks if the normalized form of content starting at pos
// matches normValue. Returns the end position in content if matched, -1 otherwise.
func matchNormalizedAt(content string, pos int, normValue string) int {
	ci := pos
	ni := 0
	for ci < len(content) && ni < len(normValue) {
		cb := content[ci]
		nb := normValue[ni]
		if isSpace(cb) && isSpace(nb) {
			// Both at whitespace: skip all whitespace in both
			for ci < len(content) && isSpace(content[ci]) {
				ci++
			}
			for ni < len(normValue) && isSpace(normValue[ni]) {
				ni++
			}
		} else if cb == nb {
			ci++
			ni++
		} else if isSpace(cb) {
			// Extra whitespace in content that normalized form skips
			ci++
		} else {
			return -1
		}
	}
	// Skip trailing whitespace in normValue
	for ni < len(normValue) && isSpace(normValue[ni]) {
		ni++
	}
	if ni == len(normValue) {
		return ci
	}
	return -1
}

func collapseWhitespace(s string) string {
	var b strings.Builder
	inSpace := false
	for _, r := range s {
		if r == ' ' || r == '\t' || r == '\n' || r == '\r' {
			if !inSpace {
				b.WriteByte(' ')
				inSpace = true
			}
		} else {
			b.WriteRune(r)
			inSpace = false
		}
	}
	return b.String()
}

func isSpace(b byte) bool {
	return b == ' ' || b == '\t' || b == '\n' || b == '\r'
}

func cloneMap(m map[string]string) map[string]string {
	if m == nil {
		return nil
	}
	c := make(map[string]string, len(m))
	for k, v := range m {
		c[k] = v
	}
	return c
}
