package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/shaojie-li/stocks-marketing/internal/config"
	"github.com/shaojie-li/stocks-marketing/internal/storage/postgres/db"
)

const maxSettingBytes = 1 << 20

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "settings:", err)
		os.Exit(1)
	}
}

func run() error {
	setFlags := flag.NewFlagSet("set", flag.ContinueOnError)
	secret := setFlags.Bool("secret", false, "encrypt the value before storing it")
	if len(os.Args) < 2 || os.Args[1] != "set" {
		return errors.New("usage: settings set [--secret] <key> (value is read from stdin)")
	}
	if err := setFlags.Parse(os.Args[2:]); err != nil {
		return err
	}
	if setFlags.NArg() != 1 {
		return errors.New("set requires exactly one setting key")
	}
	value, err := io.ReadAll(io.LimitReader(os.Stdin, maxSettingBytes+1))
	if err != nil {
		return errors.New("read setting value from stdin")
	}
	if len(value) > maxSettingBytes {
		return errors.New("setting value exceeds 1 MiB")
	}
	trimmed := strings.TrimSuffix(strings.TrimSuffix(string(value), "\n"), "\r")

	bootstrap, err := config.LoadBootstrap()
	if err != nil {
		return err
	}
	cipher, err := config.NewCipher(bootstrap.MasterKey)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, bootstrap.DatabaseURL)
	if err != nil {
		return errors.New("connect to settings database")
	}
	defer pool.Close()
	store := config.NewStore(db.New(pool), cipher)
	key := setFlags.Arg(0)
	if err := store.Set(ctx, key, trimmed, *secret); err != nil {
		return err
	}
	fmt.Printf("stored setting %s (secret=%t)\n", key, *secret)
	return nil
}
