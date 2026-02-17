package main

import (
	"fmt"
	"os"
	"path/filepath"
	"text/tabwriter"

	"github.com/mtzanidakis/praktor/internal/config"
	"github.com/mtzanidakis/praktor/internal/store"
	"github.com/mtzanidakis/praktor/internal/vault"
)

func runVault(args []string) error {
	if len(args) == 0 {
		printVaultUsage()
		return nil
	}

	passphrase := os.Getenv("PRAKTOR_VAULT_PASSPHRASE")
	if passphrase == "" {
		return fmt.Errorf("PRAKTOR_VAULT_PASSPHRASE environment variable is required")
	}

	v := vault.New(passphrase)

	db, err := store.New(config.StorePath)
	if err != nil {
		return fmt.Errorf("open store: %w", err)
	}
	defer db.Close()

	switch args[0] {
	case "list":
		return vaultList(db)
	case "set":
		return vaultSet(db, v, args[1:])
	case "get":
		return vaultGet(db, v, args[1:])
	case "delete":
		return vaultDelete(db, args[1:])
	case "assign":
		return vaultAssign(db, args[1:])
	case "unassign":
		return vaultUnassign(db, args[1:])
	case "global":
		return vaultGlobal(db, v, args[1:])
	default:
		printVaultUsage()
		return fmt.Errorf("unknown vault command: %s", args[0])
	}
}

func printVaultUsage() {
	fmt.Fprintf(os.Stderr, `Usage: praktor vault <command>

Commands:
  list                              List all secrets (metadata only)
  set <name> --value <str> [--description <text>]   Store a string secret
  set <name> --file <path> [--description <text>]  Store a file secret
  get <name>                        Retrieve and decrypt a secret
  delete <name>                     Delete a secret
  assign <name> --agent <id>        Assign a secret to an agent
  unassign <name> --agent <id>      Remove a secret from an agent
  global <name> --enable|--disable  Toggle global access

Environment:
  PRAKTOR_VAULT_PASSPHRASE          Required. Encryption passphrase.
`)
}

func vaultList(db *store.Store) error {
	secrets, err := db.ListSecrets()
	if err != nil {
		return err
	}
	if len(secrets) == 0 {
		fmt.Println("No secrets stored.")
		return nil
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "NAME\tKIND\tGLOBAL\tDESCRIPTION\tAGENTS")
	for _, s := range secrets {
		global := ""
		if s.Global {
			global = "yes"
		}
		agentIDs, _ := db.GetSecretAgentIDs(s.ID)
		agents := ""
		for i, id := range agentIDs {
			if i > 0 {
				agents += ", "
			}
			agents += id
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n", s.Name, s.Kind, global, s.Description, agents)
	}
	return w.Flush()
}

func vaultSet(db *store.Store, v *vault.Vault, args []string) error {
	if len(args) < 3 {
		return fmt.Errorf("usage: praktor vault set <name> --value <string> | --file <path> [--description <text>]")
	}

	name := args[0]
	var value []byte
	kind := "string"
	filename := ""

	switch args[1] {
	case "--value":
		value = []byte(args[2])
	case "--file":
		data, err := os.ReadFile(args[2])
		if err != nil {
			return fmt.Errorf("read file: %w", err)
		}
		value = data
		kind = "file"
		filename = filepath.Base(args[2])
	default:
		return fmt.Errorf("expected --value or --file, got %s", args[1])
	}

	// Check for optional --description flag
	description := ""
	for i := 3; i < len(args)-1; i++ {
		if args[i] == "--description" {
			description = args[i+1]
			break
		}
	}

	ciphertext, nonce, err := v.Encrypt(value)
	if err != nil {
		return fmt.Errorf("encrypt: %w", err)
	}

	sec := &store.Secret{
		ID:          name,
		Name:        name,
		Description: description,
		Kind:        kind,
		Filename:    filename,
		Value:       ciphertext,
		Nonce:       nonce,
	}

	// Preserve global flag if updating
	existing, _ := db.GetSecret(name)
	if existing != nil {
		sec.Global = existing.Global
	}

	if err := db.SaveSecret(sec); err != nil {
		return err
	}
	fmt.Printf("Secret %q saved (%s)\n", name, kind)
	return nil
}

func vaultGet(db *store.Store, v *vault.Vault, args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("usage: praktor vault get <name>")
	}

	sec, err := db.GetSecret(args[0])
	if err != nil {
		return err
	}
	if sec == nil {
		return fmt.Errorf("secret %q not found", args[0])
	}

	plaintext, err := v.Decrypt(sec.Value, sec.Nonce)
	if err != nil {
		return fmt.Errorf("decrypt: %w", err)
	}

	if sec.Kind == "file" {
		fmt.Printf("File: %s\n", sec.Filename)
	}
	fmt.Print(string(plaintext))
	if len(plaintext) > 0 && plaintext[len(plaintext)-1] != '\n' {
		fmt.Println()
	}
	return nil
}

func vaultDelete(db *store.Store, args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("usage: praktor vault delete <name>")
	}
	if err := db.DeleteSecret(args[0]); err != nil {
		return err
	}
	fmt.Printf("Secret %q deleted\n", args[0])
	return nil
}

func vaultAssign(db *store.Store, args []string) error {
	if len(args) < 3 || args[1] != "--agent" {
		return fmt.Errorf("usage: praktor vault assign <name> --agent <id>")
	}
	if err := db.AddAgentSecret(args[2], args[0]); err != nil {
		return err
	}
	fmt.Printf("Secret %q assigned to agent %q\n", args[0], args[2])
	return nil
}

func vaultUnassign(db *store.Store, args []string) error {
	if len(args) < 3 || args[1] != "--agent" {
		return fmt.Errorf("usage: praktor vault unassign <name> --agent <id>")
	}
	if err := db.RemoveAgentSecret(args[2], args[0]); err != nil {
		return err
	}
	fmt.Printf("Secret %q unassigned from agent %q\n", args[0], args[2])
	return nil
}

func vaultGlobal(db *store.Store, v *vault.Vault, args []string) error {
	if len(args) < 2 {
		return fmt.Errorf("usage: praktor vault global <name> --enable|--disable")
	}

	name := args[0]
	sec, err := db.GetSecret(name)
	if err != nil {
		return err
	}
	if sec == nil {
		return fmt.Errorf("secret %q not found", name)
	}

	switch args[1] {
	case "--enable":
		sec.Global = true
	case "--disable":
		sec.Global = false
	default:
		return fmt.Errorf("expected --enable or --disable, got %s", args[1])
	}

	if err := db.SaveSecret(sec); err != nil {
		return err
	}
	fmt.Printf("Secret %q global=%v\n", name, sec.Global)
	return nil
}
