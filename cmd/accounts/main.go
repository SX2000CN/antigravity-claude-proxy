// Package main provides the account management CLI tool.
// This file corresponds to src/cli/accounts.js in the Node.js version.
package main

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/poemonsense/antigravity-proxy-go/internal/auth"
	"github.com/poemonsense/antigravity-proxy-go/internal/config"
	"github.com/poemonsense/antigravity-proxy-go/pkg/redis"
)

var (
	serverPort = config.DefaultPort
)

func main() {
	// Parse command and flags
	args := os.Args[1:]
	command := "add"
	noBrowser := false

	for _, arg := range args {
		if arg == "--no-browser" {
			noBrowser = true
		} else if !strings.HasPrefix(arg, "-") && command == "add" {
			command = arg
		}
	}

	// Check for PORT env var
	if port := os.Getenv("PORT"); port != "" {
		if p, err := strconv.Atoi(port); err == nil {
			serverPort = p
		}
	}

	printBanner()

	scanner := bufio.NewScanner(os.Stdin)

	switch command {
	case "add":
		ensureServerStopped()
		interactiveAdd(scanner, noBrowser)
	case "list":
		listAccounts()
	case "clear":
		ensureServerStopped()
		clearAccounts(scanner)
	case "verify":
		verifyAccounts()
	case "remove":
		ensureServerStopped()
		interactiveRemove(scanner)
	case "help":
		printHelp()
	default:
		fmt.Printf("Unknown command: %s\n", command)
		fmt.Println("Run with \"help\" for usage information.")
	}
}

func printBanner() {
	fmt.Println("╔════════════════════════════════════════╗")
	fmt.Println("║   Antigravity Proxy Account Manager    ║")
	fmt.Println("║   Use --no-browser for headless mode   ║")
	fmt.Println("╚════════════════════════════════════════╝")
}

func printHelp() {
	fmt.Println("\nUsage:")
	fmt.Println("  antigravity-accounts add     Add new account(s)")
	fmt.Println("  antigravity-accounts list    List all accounts")
	fmt.Println("  antigravity-accounts verify  Verify account tokens")
	fmt.Println("  antigravity-accounts clear   Remove all accounts")
	fmt.Println("  antigravity-accounts help    Show this help")
	fmt.Println("\nOptions:")
	fmt.Println("  --no-browser    Manual authorization code input (for headless servers)")
}

// isServerRunning checks if the proxy server is running on the configured port
func isServerRunning() bool {
	conn, err := net.DialTimeout("tcp", fmt.Sprintf("localhost:%d", serverPort), time.Second)
	if err != nil {
		return false
	}
	conn.Close()
	return true
}

// ensureServerStopped exits if the server is running
func ensureServerStopped() {
	if isServerRunning() {
		fmt.Printf("\n\033[31mError: Antigravity Proxy server is currently running on port %d.\033[0m\n\n", serverPort)
		fmt.Println("Please stop the server (Ctrl+C) before adding or managing accounts.")
		fmt.Println("This ensures that your account changes are loaded correctly when you restart the server.")
		os.Exit(1)
	}
}

// openBrowser opens the URL in the default browser
func openBrowser(url string) {
	var cmd *exec.Cmd

	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "windows":
		cmd = exec.Command("cmd", "/c", "start", "", strings.ReplaceAll(url, "&", "^&"))
	default:
		cmd = exec.Command("xdg-open", url)
	}

	if err := cmd.Start(); err != nil {
		fmt.Println("\n⚠ Could not open browser automatically.")
		fmt.Println("Please open this URL manually:", url)
	}
}

// loadAccounts loads accounts from Redis
func loadAccounts() []*redis.Account {
	client, err := redis.NewClient(redis.Config{
		Addr: "localhost:6379",
	})
	if err != nil {
		fmt.Println("Error connecting to Redis:", err)
		return nil
	}
	defer client.Close()

	store := redis.NewAccountStore(client)
	ctx := context.Background()

	accounts, err := store.ListAccounts(ctx)
	if err != nil {
		fmt.Println("Error loading accounts:", err)
		return nil
	}

	return accounts
}

// saveAccount saves an account to Redis
func saveAccount(acc *redis.Account) error {
	client, err := redis.NewClient(redis.Config{
		Addr: "localhost:6379",
	})
	if err != nil {
		return err
	}
	defer client.Close()

	store := redis.NewAccountStore(client)
	ctx := context.Background()

	return store.SetAccount(ctx, acc)
}

// deleteAccount removes an account from Redis
func deleteAccount(email string) error {
	client, err := redis.NewClient(redis.Config{
		Addr: "localhost:6379",
	})
	if err != nil {
		return err
	}
	defer client.Close()

	store := redis.NewAccountStore(client)
	ctx := context.Background()

	return store.DeleteAccount(ctx, email)
}

// clearAllAccounts removes all accounts from Redis
func clearAllAccountsFromStore() error {
	client, err := redis.NewClient(redis.Config{
		Addr: "localhost:6379",
	})
	if err != nil {
		return err
	}
	defer client.Close()

	store := redis.NewAccountStore(client)
	ctx := context.Background()

	accounts, err := store.ListAccounts(ctx)
	if err != nil {
		return err
	}

	for _, acc := range accounts {
		if err := store.DeleteAccount(ctx, acc.Email); err != nil {
			return err
		}
	}

	return nil
}

// displayAccounts shows the list of accounts
func displayAccounts(accounts []*redis.Account) {
	if len(accounts) == 0 {
		fmt.Println("\nNo accounts configured.")
		return
	}

	fmt.Printf("\n%d account(s) saved:\n", len(accounts))
	for i, acc := range accounts {
		status := ""
		if acc.IsInvalid {
			status = " (invalid)"
		} else if !acc.Enabled {
			status = " (disabled)"
		}
		fmt.Printf("  %d. %s%s\n", i+1, acc.Email, status)
	}
}

// prompt reads a line of input
func prompt(scanner *bufio.Scanner, message string) string {
	fmt.Print(message)
	if scanner.Scan() {
		return strings.TrimSpace(scanner.Text())
	}
	return ""
}

// addAccount adds a new account via OAuth with automatic callback
func addAccount(existingAccounts []*redis.Account) *redis.Account {
	fmt.Println("\n=== Add Google Account ===")

	// Generate authorization URL
	result, err := auth.GetAuthorizationURL("")
	if err != nil {
		fmt.Println("Error generating auth URL:", err)
		return nil
	}

	fmt.Println("Opening browser for Google sign-in...")
	fmt.Println("(If browser does not open, copy this URL manually)")
	fmt.Printf("   %s\n\n", result.URL)

	// Open browser
	openBrowser(result.URL)

	// Start callback server and wait for code
	fmt.Println("Waiting for authentication (timeout: 2 minutes)...")

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	callbackServer := auth.NewCallbackServer(result.State, 120000)
	code, err := callbackServer.Start(ctx)
	if err != nil {
		fmt.Printf("\n✗ Authentication failed: %v\n", err)
		return nil
	}

	fmt.Println("Received authorization code. Exchanging for tokens...")

	accountData, err := auth.CompleteOAuthFlow(ctx, code, result.Verifier)
	if err != nil {
		fmt.Printf("\n✗ Authentication failed: %v\n", err)
		return nil
	}

	// Check if account already exists
	for _, acc := range existingAccounts {
		if acc.Email == accountData.Email {
			fmt.Printf("\n⚠ Account %s already exists. Updating tokens.\n", accountData.Email)
			acc.RefreshToken = accountData.RefreshToken
			acc.LastUsed = time.Now().UnixMilli()
			if err := saveAccount(acc); err != nil {
				fmt.Println("Error saving account:", err)
			}
			return nil // Don't add duplicate
		}
	}

	fmt.Printf("\n✓ Successfully authenticated: %s\n", accountData.Email)
	fmt.Println("  Project will be discovered on first API request.")

	return &redis.Account{
		Email:        accountData.Email,
		RefreshToken: accountData.RefreshToken,
		Source:       "oauth",
		Enabled:      true,
	}
}

// addAccountNoBrowser adds a new account via manual code input
func addAccountNoBrowser(existingAccounts []*redis.Account, scanner *bufio.Scanner) *redis.Account {
	fmt.Println("\n=== Add Google Account (No-Browser Mode) ===")

	// Generate authorization URL
	result, err := auth.GetAuthorizationURL("")
	if err != nil {
		fmt.Println("Error generating auth URL:", err)
		return nil
	}

	fmt.Println("Copy the following URL and open it in a browser on another device:")
	fmt.Printf("   %s\n\n", result.URL)
	fmt.Println("After signing in, you will be redirected to a localhost URL.")
	fmt.Println("Copy the ENTIRE redirect URL or just the authorization code.")

	input := prompt(scanner, "Paste the callback URL or authorization code: ")
	if input == "" {
		fmt.Println("\n✗ No input provided.")
		return nil
	}

	codeResult, err := auth.ExtractCodeFromInput(input)
	if err != nil {
		fmt.Printf("\n✗ %v\n", err)
		return nil
	}

	// Validate state if present
	if codeResult.State != "" && codeResult.State != result.State {
		fmt.Println("\n⚠ State mismatch detected. This could indicate a security issue.")
		fmt.Println("Proceeding anyway as this is manual mode...")
	}

	fmt.Println("\nExchanging authorization code for tokens...")

	ctx := context.Background()
	accountData, err := auth.CompleteOAuthFlow(ctx, codeResult.Code, result.Verifier)
	if err != nil {
		fmt.Printf("\n✗ Authentication failed: %v\n", err)
		return nil
	}

	// Check if account already exists
	for _, acc := range existingAccounts {
		if acc.Email == accountData.Email {
			fmt.Printf("\n⚠ Account %s already exists. Updating tokens.\n", accountData.Email)
			acc.RefreshToken = accountData.RefreshToken
			acc.LastUsed = time.Now().UnixMilli()
			if err := saveAccount(acc); err != nil {
				fmt.Println("Error saving account:", err)
			}
			return nil // Don't add duplicate
		}
	}

	fmt.Printf("\n✓ Successfully authenticated: %s\n", accountData.Email)
	fmt.Println("  Project will be discovered on first API request.")

	return &redis.Account{
		Email:        accountData.Email,
		RefreshToken: accountData.RefreshToken,
		Source:       "oauth",
		Enabled:      true,
	}
}

// interactiveAdd handles the interactive add flow
func interactiveAdd(scanner *bufio.Scanner, noBrowser bool) {
	if noBrowser {
		fmt.Println("\n📋 No-browser mode: You will manually paste the authorization code.")
	}

	accounts := loadAccounts()
	if accounts == nil {
		accounts = []*redis.Account{}
	}

	if len(accounts) > 0 {
		displayAccounts(accounts)

		choice := prompt(scanner, "\n(a)dd new, (r)emove existing, (f)resh start, or (e)xit? [a/r/f/e]: ")
		c := strings.ToLower(choice)

		switch c {
		case "r":
			interactiveRemove(scanner)
			return
		case "f":
			fmt.Println("\nStarting fresh - existing accounts will be replaced.")
			if err := clearAllAccountsFromStore(); err != nil {
				fmt.Println("Error clearing accounts:", err)
				return
			}
			accounts = []*redis.Account{}
		case "e":
			fmt.Println("\nExiting...")
			return
		case "a":
			fmt.Println("\nAdding to existing accounts.")
		default:
			fmt.Println("\nInvalid choice, defaulting to add.")
		}
	}

	// Add single account
	if len(accounts) >= config.MaxAccounts {
		fmt.Printf("\nMaximum of %d accounts reached.\n", config.MaxAccounts)
		return
	}

	var newAccount *redis.Account
	if noBrowser {
		newAccount = addAccountNoBrowser(accounts, scanner)
	} else {
		newAccount = addAccount(accounts)
	}

	if newAccount != nil {
		if err := saveAccount(newAccount); err != nil {
			fmt.Println("Error saving account:", err)
		} else {
			fmt.Printf("\n✓ Saved account %s\n", newAccount.Email)
		}
		accounts = append(accounts, newAccount)
	}

	if len(accounts) > 0 {
		displayAccounts(accounts)
		fmt.Println("\nTo add more accounts, run this command again.")
	} else {
		fmt.Println("\nNo accounts to save.")
	}
}

// interactiveRemove handles removing accounts interactively
func interactiveRemove(scanner *bufio.Scanner) {
	for {
		accounts := loadAccounts()
		if len(accounts) == 0 {
			fmt.Println("\nNo accounts to remove.")
			return
		}

		displayAccounts(accounts)
		fmt.Println("\nEnter account number to remove (or 0 to cancel)")

		answer := prompt(scanner, "> ")
		index, err := strconv.Atoi(answer)
		if err != nil || index < 0 || index > len(accounts) {
			fmt.Println("\n❌ Invalid selection.")
			continue
		}

		if index == 0 {
			return // Exit
		}

		removed := accounts[index-1] // 1-based to 0-based
		confirm := prompt(scanner, fmt.Sprintf("\nAre you sure you want to remove %s? [y/N]: ", removed.Email))

		if strings.ToLower(confirm) == "y" {
			if err := deleteAccount(removed.Email); err != nil {
				fmt.Println("Error removing account:", err)
			} else {
				fmt.Printf("\n✓ Removed %s\n", removed.Email)
			}
		} else {
			fmt.Println("\nCancelled.")
		}

		removeMore := prompt(scanner, "\nRemove another account? [y/N]: ")
		if strings.ToLower(removeMore) != "y" {
			break
		}
	}
}

// listAccounts displays all accounts
func listAccounts() {
	accounts := loadAccounts()
	displayAccounts(accounts)
}

// clearAccounts removes all accounts
func clearAccounts(scanner *bufio.Scanner) {
	accounts := loadAccounts()

	if len(accounts) == 0 {
		fmt.Println("No accounts to clear.")
		return
	}

	displayAccounts(accounts)

	confirm := prompt(scanner, "\nAre you sure you want to remove all accounts? [y/N]: ")
	if strings.ToLower(confirm) == "y" {
		if err := clearAllAccountsFromStore(); err != nil {
			fmt.Println("Error clearing accounts:", err)
		} else {
			fmt.Println("All accounts removed.")
		}
	} else {
		fmt.Println("Cancelled.")
	}
}

// verifyAccounts tests all account refresh tokens
func verifyAccounts() {
	accounts := loadAccounts()

	if len(accounts) == 0 {
		fmt.Println("No accounts to verify.")
		return
	}

	fmt.Println("\nVerifying accounts...")

	ctx := context.Background()
	for _, acc := range accounts {
		tokens, err := auth.RefreshAccessToken(ctx, acc.RefreshToken)
		if err != nil {
			fmt.Printf("  ✗ %s - %v\n", acc.Email, err)
			continue
		}

		email, err := auth.GetUserEmail(ctx, tokens.AccessToken)
		if err != nil {
			fmt.Printf("  ✗ %s - %v\n", acc.Email, err)
			continue
		}

		fmt.Printf("  ✓ %s - OK\n", email)
	}
}
