package agent

import (
	"fmt"
	"log/slog"
	"strconv"
	"strings"

	"github.com/mtzanidakis/praktor/internal/config"
	"github.com/mtzanidakis/praktor/internal/container"
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
