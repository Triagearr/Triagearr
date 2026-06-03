package main

import (
	"bufio"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/urfave/cli/v3"
	"golang.org/x/term"

	"github.com/Triagearr/Triagearr/internal/auth"
	"github.com/Triagearr/Triagearr/internal/store"
)

// authStore is the store subset the recovery commands touch. Kept narrow so the
// command cores can be exercised against a real *store.Store in tests.
type authStore interface {
	ClearAuthUsers(ctx context.Context) (int64, error)
	GetSoleAuthUser(ctx context.Context) (store.AuthUser, error)
	GetAuthUserByName(ctx context.Context, username string) (store.AuthUser, error)
	UpdateAuthPassword(ctx context.Context, id int64, passwordHash string) error
}

// authCommand groups the lockout-recovery operations. Both run against the
// configured SQLite store directly: holding the data dir is the trust boundary,
// so neither requires the (lost) current password the HTTP endpoints demand.
func authCommand(configFlag cli.Flag) *cli.Command {
	return &cli.Command{
		Name:  "auth",
		Usage: "recover access to the dashboard when the password is lost",
		Commands: []*cli.Command{
			{
				Name:  "disable",
				Usage: "remove the operator account, returning the dashboard to open mode",
				Flags: []cli.Flag{
					configFlag,
					&cli.BoolFlag{Name: "yes", Aliases: []string{"y"}, Usage: "skip the confirmation prompt"},
				},
				Action: authDisableAction,
			},
			{
				Name:  "set-password",
				Usage: "set a new password for the operator account, keeping auth enabled",
				Flags: []cli.Flag{
					configFlag,
					&cli.StringFlag{Name: "user", Usage: "account to target (defaults to the sole account)"},
					&cli.StringFlag{Name: "password", Usage: "new password; omit to auto-generate one"},
					&cli.BoolFlag{Name: "stdin", Usage: "read the new password from stdin (masked when interactive)"},
				},
				Action: authSetPasswordAction,
			},
		},
	}
}

func authDisableAction(ctx context.Context, cmd *cli.Command) error {
	cfg, err := loadConfigFromCmd(cmd)
	if err != nil {
		return err
	}
	s, err := openStoreAndMigrate(ctx, cfg)
	if err != nil {
		return err
	}
	defer func() { _ = s.Close() }()

	confirmed := cmd.Bool("yes")
	if !confirmed {
		confirmed, err = confirm(os.Stdin, os.Stdout,
			"This removes the operator account and disables authentication — the dashboard becomes open. Continue?")
		if err != nil {
			return err
		}
	}
	return runAuthDisable(ctx, s, os.Stdout, confirmed)
}

// runAuthDisable clears the operator account when confirmed. Idempotent: a
// store already in open mode reports zero accounts removed.
func runAuthDisable(ctx context.Context, st authStore, out io.Writer, confirmed bool) error {
	if !confirmed {
		_, _ = fmt.Fprintln(out, "aborted; authentication left unchanged")
		return nil
	}
	n, err := st.ClearAuthUsers(ctx)
	if err != nil {
		return fmt.Errorf("disabling auth: %w", err)
	}
	if n == 0 {
		_, _ = fmt.Fprintln(out, "authentication was already disabled (open mode)")
		return nil
	}
	_, _ = fmt.Fprintf(out, "authentication disabled — %d account(s) removed, dashboard is now in open mode\n", n)
	return nil
}

func authSetPasswordAction(ctx context.Context, cmd *cli.Command) error {
	flagPassword := cmd.String("password")
	useStdin := cmd.Bool("stdin")
	if flagPassword != "" && useStdin {
		return errors.New("--password and --stdin are mutually exclusive")
	}

	supplied := flagPassword
	if useStdin {
		s, err := readSecret(os.Stdin, os.Stdout)
		if err != nil {
			return err
		}
		supplied = s
	}

	cfg, err := loadConfigFromCmd(cmd)
	if err != nil {
		return err
	}
	s, err := openStoreAndMigrate(ctx, cfg)
	if err != nil {
		return err
	}
	defer func() { _ = s.Close() }()

	return runAuthSetPassword(ctx, s, os.Stdout, cmd.String("user"), supplied)
}

// runAuthSetPassword rotates the password of the targeted account (the sole
// account when username is empty), auto-generating one when supplied is empty.
// A generated password is printed once — it is never stored in plaintext.
func runAuthSetPassword(ctx context.Context, st authStore, out io.Writer, username, supplied string) error {
	var (
		user store.AuthUser
		err  error
	)
	if username != "" {
		user, err = st.GetAuthUserByName(ctx, username)
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("no account named %q", username)
		}
	} else {
		user, err = st.GetSoleAuthUser(ctx)
		if errors.Is(err, sql.ErrNoRows) {
			return errors.New("authentication is not enabled; nothing to reset (enable it from the dashboard first)")
		}
	}
	if err != nil {
		return fmt.Errorf("loading account: %w", err)
	}

	plain, hash, generated, err := auth.Resolve(supplied)
	if err != nil {
		return err
	}
	if err := st.UpdateAuthPassword(ctx, user.ID, hash); err != nil {
		return fmt.Errorf("updating password: %w", err)
	}

	if generated {
		_, _ = fmt.Fprintf(out, "password reset for %q. Save it now — it is not shown again:\n\n    %s\n\n", user.Username, plain)
	} else {
		_, _ = fmt.Fprintf(out, "password reset for %q\n", user.Username)
	}
	return nil
}

// confirm reads a y/N answer. Anything other than "y"/"yes" (case-insensitive)
// is a no.
func confirm(in io.Reader, out io.Writer, prompt string) (bool, error) {
	_, _ = fmt.Fprintf(out, "%s [y/N]: ", prompt)
	line, err := bufio.NewReader(in).ReadString('\n')
	if err != nil && err != io.EOF {
		return false, fmt.Errorf("reading confirmation: %w", err)
	}
	switch strings.ToLower(strings.TrimSpace(line)) {
	case "y", "yes":
		return true, nil
	default:
		return false, nil
	}
}

// readSecret reads a password from in. When in is an interactive terminal it
// disables echo and asks twice to catch typos; piped input is read as a single
// line (no confirmation — the caller controls both ends).
func readSecret(in *os.File, out io.Writer) (string, error) {
	fd := int(in.Fd())
	if !term.IsTerminal(fd) {
		line, err := bufio.NewReader(in).ReadString('\n')
		if err != nil && err != io.EOF {
			return "", fmt.Errorf("reading password from stdin: %w", err)
		}
		return strings.TrimRight(line, "\r\n"), nil
	}

	_, _ = fmt.Fprint(out, "New password: ")
	first, err := term.ReadPassword(fd)
	if err != nil {
		return "", fmt.Errorf("reading password: %w", err)
	}
	_, _ = fmt.Fprint(out, "\nConfirm password: ")
	second, err := term.ReadPassword(fd)
	if err != nil {
		return "", fmt.Errorf("reading password: %w", err)
	}
	_, _ = fmt.Fprintln(out)
	if string(first) != string(second) {
		return "", errors.New("passwords did not match")
	}
	return string(first), nil
}
