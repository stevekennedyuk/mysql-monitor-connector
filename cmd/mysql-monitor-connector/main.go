package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/example/mysql-monitor-connector/internal/agent"
	"github.com/example/mysql-monitor-connector/internal/config"
	"github.com/example/mysql-monitor-connector/internal/enroll"
	"github.com/example/mysql-monitor-connector/internal/mysqltarget"
)

const defaultConfigDir = "/etc/mysql-monitor-connector"

func main() {
	if err := run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func run(args []string, stdin io.Reader, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		usage(stderr)
		return flag.ErrHelp
	}

	switch args[0] {
	case "run":
		fs := flag.NewFlagSet("run", flag.ContinueOnError)
		fs.SetOutput(stderr)
		configDir := fs.String("config-dir", defaultConfigDir, "configuration directory")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if fs.NArg() != 0 {
			return fmt.Errorf("run takes no positional arguments")
		}
		logger := slog.New(slog.NewJSONHandler(stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
		ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
		defer stop()
		return agent.Run(ctx, *configDir, logger)

	case "enroll":
		fs := flag.NewFlagSet("enroll", flag.ContinueOnError)
		fs.SetOutput(stderr)
		configDir := fs.String("config-dir", defaultConfigDir, "configuration directory")
		serviceURL := fs.String("service-url", "", "HTTPS base URL of the monitoring service")
		tokenFile := fs.String("token-file", "", "file containing the single-use enrollment token")
		caFile := fs.String("ca-file", "", "optional private CA bundle for enrollment")
		dataDir := fs.String("data-dir", "/var/lib/mysql-monitor-connector", "runtime state directory")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		return enroll.Run(context.Background(), enroll.Options{
			ConfigDir: *configDir, ServiceURL: *serviceURL, TokenFile: *tokenFile,
			CAFile: *caFile, DataDir: *dataDir,
		})

	case "target":
		return runTarget(args[1:], stdin, stdout, stderr)

	case "version":
		fmt.Fprintln(stdout, "mysql-monitor-connector dev")
		return nil
	default:
		usage(stderr)
		return fmt.Errorf("unknown command %q", args[0])
	}
}

func runTarget(args []string, stdin io.Reader, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		return fmt.Errorf("target requires add, list, or test")
	}
	switch args[0] {
	case "add":
		fs := flag.NewFlagSet("target add", flag.ContinueOnError)
		fs.SetOutput(stderr)
		configDir := fs.String("config-dir", defaultConfigDir, "configuration directory")
		name := fs.String("name", "", "local target name")
		network := fs.String("network", "tcp", "tcp or unix")
		address := fs.String("address", "", "host:port or Unix socket path")
		user := fs.String("user", "", "MySQL monitoring user")
		database := fs.String("database", "", "optional default database")
		tlsCA := fs.String("tls-ca", "", "CA bundle for the MySQL server")
		tlsServerName := fs.String("tls-server-name", "", "verified MySQL TLS server name")
		passwordStdin := fs.Bool("password-stdin", false, "read the password from stdin")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if !*passwordStdin {
			return fmt.Errorf("--password-stdin is required; passwords are never accepted as arguments")
		}
		password, err := bufio.NewReader(io.LimitReader(stdin, config.MaxSecretBytes+2)).ReadBytes('\n')
		if err != nil && err != io.EOF {
			return fmt.Errorf("read password: %w", err)
		}
		return config.AddTarget(*configDir, config.Target{
			Name: *name, Network: *network, Address: *address, User: *user,
			Database: *database, TLSCAFile: *tlsCA, TLSServerName: *tlsServerName,
		}, password)

	case "list":
		fs := flag.NewFlagSet("target list", flag.ContinueOnError)
		fs.SetOutput(stderr)
		configDir := fs.String("config-dir", defaultConfigDir, "configuration directory")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		targets, err := config.ListTargets(*configDir)
		if err != nil {
			return err
		}
		for _, target := range targets {
			fmt.Fprintf(stdout, "%s\t%s\t%s\t%s\n", target.Name, target.Network, target.Address, target.User)
		}
		return nil

	case "test":
		fs := flag.NewFlagSet("target test", flag.ContinueOnError)
		fs.SetOutput(stderr)
		configDir := fs.String("config-dir", defaultConfigDir, "configuration directory")
		name := fs.String("name", "", "local target name")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		target, password, err := config.LoadTarget(*configDir, *name)
		if err != nil {
			return err
		}
		db, err := mysqltarget.Open(target, password)
		if err != nil {
			return err
		}
		defer db.Close()
		ctx, cancel := context.WithTimeout(context.Background(), config.DefaultQueryTimeout)
		defer cancel()
		if err := db.PingContext(ctx); err != nil {
			return fmt.Errorf("MySQL connectivity test failed: %w", err)
		}
		fmt.Fprintln(stdout, "connection successful")
		return nil
	default:
		return fmt.Errorf("unknown target command %q", args[0])
	}
}

func usage(w io.Writer) {
	fmt.Fprintln(w, "usage: mysql-monitor-connector <run|enroll|target|version> [options]")
}
