package main

import (
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"
)

// Config uchovává nastavení aplikace získané z příkazové řádky
type Config struct {
	IsServer bool
	IsClient bool
	Port     int
	Host     string
	Address  string
	Input    string
	Output   string
	Timeout  int
}

func parseCLI() (*Config, error) {
	cfg := &Config{}
	
	// Server vs Client mod
	flag.BoolVar(&cfg.IsServer, "s", false, "Start server")
	flag.BoolVar(&cfg.IsClient, "c", false, "Start client")
	
	// Port
	flag.IntVar(&cfg.Port, "p", 0, "UDP port")
	
	// Server parameters
	flag.StringVar(&cfg.Address, "a", "", "Server bind address (optional) OR Client host address")
	
	// Client / Server IO
	flag.StringVar(&cfg.Input, "i", "", "Input file (client)")
	flag.StringVar(&cfg.Output, "o", "", "Output file (server)")
	
	// Timeout
	flag.IntVar(&cfg.Timeout, "w", 1, "Timeout in seconds")
	
	// Own usage if user uses -h
	flag.Usage = func() {
		fmt.Fprintf(os.Stdout, "Usage: \n")
		fmt.Fprintf(os.Stdout, "Server: ./ipk-rdt -s -p PORT [-a ADDRESS] [-o OUTPUT] [-w TIMEOUT]\n")
		fmt.Fprintf(os.Stdout, "Client: ./ipk-rdt -c -a HOST -p PORT [-i INPUT] [-w TIMEOUT]\n")
	}

	flag.Parse()

	// Validate arguments
	if cfg.IsServer && cfg.IsClient {
		return nil, fmt.Errorf("cannot be both server and client")
	}
	if !cfg.IsServer && !cfg.IsClient {
		return nil, fmt.Errorf("must specify either -s or -c")
	}
	if cfg.Port <= 0 || cfg.Port > 65535 {
		return nil, fmt.Errorf("missing or invalid port (-p)")
	}
	if cfg.Timeout <= 0 {
		return nil, fmt.Errorf("timeout must be greater than 0")
	}

	if cfg.IsClient {
		if cfg.Address == "" {
			return nil, fmt.Errorf("client must specify host (-a)")
		}
		cfg.Host = cfg.Address // unify semantics for client
	}

	return cfg, nil
}

func main() {
	cfg, err := parseCLI()
	if err != nil {
		if err.Error() == "flag: help requested" {
			os.Exit(0)
		}
		fmt.Fprintf(os.Stderr, "Error parsing arguments: %v\n", err)
		os.Exit(1)
	}

	// Catch signals SIGINT and SIGTERM
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	errCh := make(chan error, 1)

	// Run server / client in goroutine
	go func() {
		if cfg.IsServer {
			errCh <- runServer(cfg)
		} else {
			errCh <- runClient(cfg)
		}
	}()

	select {
	case sig := <-sigCh:
		// If signal is received, terminate program correctly
		fmt.Fprintf(os.Stderr, "Received signal %v, terminating promptly.\n", sig)
		os.Exit(0)
	case err := <-errCh:
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
	}
}
