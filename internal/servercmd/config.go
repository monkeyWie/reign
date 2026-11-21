package servercmd

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

type commandAction int

const (
	serve commandAction = iota
	initConfig
	printVersion
	printRelayPin
)

type command struct {
	file   string
	action commandAction
}

func parseCommand(args []string, output io.Writer) (command, error) {
	var cmd command
	f := flag.NewFlagSet("reignhub-server", flag.ContinueOnError)
	f.SetOutput(output)
	f.StringVar(&cmd.file, "config", "config.json", "path to the service configuration")
	init := f.Bool("init", false, "create a default config file and exit")
	pin := f.Bool("print-relay-pin", false, "print the relay public pin from the configured state and exit")
	version := f.Bool("version", false, "print the version and exit")
	if err := f.Parse(args); err != nil {
		return cmd, err
	}
	if f.NArg() != 0 || cmd.file == "" {
		return cmd, errors.New("expected --config PATH or a management action (see --help)")
	}
	for _, opt := range []struct {
		enabled bool
		action  commandAction
	}{{*init, initConfig}, {*pin, printRelayPin}, {*version, printVersion}} {
		if opt.enabled {
			if cmd.action != serve {
				return cmd, errors.New("choose one of --init, --print-relay-pin or --version")
			}
			cmd.action = opt.action
		}
	}
	return cmd, nil
}

func writeDefaultConfig(path string) error {
	data, err := json.MarshalIndent(DefaultConfig(), "", "  ")
	if err != nil {
		return err
	}
	// Publish a complete file exclusively, including when two initializers race.
	tmp, err := os.CreateTemp(filepath.Dir(path), ".reignhub-config-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	_, err = tmp.Write(append(data, '\n'))
	closeErr := tmp.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	if err := os.Link(tmp.Name(), path); err != nil {
		if errors.Is(err, os.ErrExist) {
			return fmt.Errorf("config %q already exists", path)
		}
		return err
	}
	return nil
}

func loadConfig(path string) (Config, error) {
	c := DefaultConfig()
	f, err := os.Open(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return c, fmt.Errorf("config %q is missing; use --init to create it: %w", path, err)
		}
		return c, err
	}
	defer f.Close()
	const maxSize = 1 << 20
	data, err := io.ReadAll(io.LimitReader(f, maxSize+1))
	if err != nil {
		return c, err
	}
	if len(data) > maxSize {
		return c, fmt.Errorf("config %q exceeds 1 MiB", path)
	}
	data = bytes.TrimSpace(data)
	if len(data) == 0 || data[0] != '{' {
		return c, fmt.Errorf("config %q must contain one JSON object", path)
	}
	d := json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	if err := d.Decode(&c); err != nil {
		return c, fmt.Errorf("config %q: %w", path, err)
	}
	if d.Decode(new(any)) != io.EOF {
		return c, fmt.Errorf("config %q must contain one JSON object", path)
	}
	if c.ExternalRelays == nil || c.ExternalSTUN == nil {
		return c, fmt.Errorf("config %q: externalRelays and externalStun must be arrays", path)
	}
	if err := c.Validate(); err != nil {
		return c, fmt.Errorf("config %q: %w", path, err)
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return c, err
	}
	for _, value := range []*string{&c.State, &c.Web, &c.TLSCert, &c.TLSKey} {
		if *value != "" && !filepath.IsAbs(*value) {
			*value = filepath.Join(filepath.Dir(abs), *value)
		}
	}
	return c, nil
}
