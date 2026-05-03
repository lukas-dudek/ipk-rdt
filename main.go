package main

import (
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"
)

// Config stores app settings from CLI
type Config struct {
	IsServer bool
	IsClient bool
	Port     int
	Host     string
	Address  string
	Input    string
	Output   string
	Timeout  int
	Verbose  bool
}

// verbose is set from Config after CLI parsing
var verbose bool

// logf writes a diagnostic message to stderr only in verbose mode.
func logf(format string, args ...interface{}) {
	if verbose {
		fmt.Fprintf(os.Stderr, format, args...)
	}
}

func printHelp() {
	fmt.Fprintf(os.Stdout, "Usage:\n")
	fmt.Fprintf(os.Stdout, "  Server: ./ipk-rdt -s -p PORT [-a ADDRESS] [-o OUTPUT] [-w TIMEOUT]\n")
	fmt.Fprintf(os.Stdout, "  Client: ./ipk-rdt -c -a HOST -p PORT [-i INPUT] [-w TIMEOUT]\n")
	fmt.Fprintf(os.Stdout, "\nOptions:\n")
	fmt.Fprintf(os.Stdout, "  -s          Run as server (receiver)\n")
	fmt.Fprintf(os.Stdout, "  -c          Run as client (sender)\n")
	fmt.Fprintf(os.Stdout, "  -p PORT     UDP port number (required)\n")
	fmt.Fprintf(os.Stdout, "  -a ADDR     Server bind address (server) or host address (client)\n")
	fmt.Fprintf(os.Stdout, "  -i FILE     Input file for client (default: stdin)\n")
	fmt.Fprintf(os.Stdout, "  -o FILE     Output file for server (default: stdout)\n")
	fmt.Fprintf(os.Stdout, "  -w SECONDS  Inactivity timeout in seconds (default: 1)\n")
	fmt.Fprintf(os.Stdout, "  -h, --help  Show this help message\n")
}

func parseCLI() (*Config, error) {
	cfg := &Config{}

	// Intercept -h --help before flag.Parse() so it prints to stdout and exit 0
	for _, arg := range os.Args[1:] {
		if arg == "-h" || arg == "--help" {
			printHelp()
			os.Exit(0)
		}
	}

	// Suppress default flag use
	flag.Usage = func() {}

	// Server and Client mode
	flag.BoolVar(&cfg.IsServer, "s", false, "Start server")
	flag.BoolVar(&cfg.IsClient, "c", false, "Start client")

	// Port
	flag.IntVar(&cfg.Port, "p", 0, "UDP port")

	// Server bind address or client host address
	flag.StringVar(&cfg.Address, "a", "", "Server bind address or client host address")

	// Input and Output
	flag.StringVar(&cfg.Input, "i", "", "Input file")
	flag.StringVar(&cfg.Output, "o", "", "Output file")

	// Timeout
	flag.IntVar(&cfg.Timeout, "w", 1, "Timeout in seconds")

	// Verbose mode
	flag.BoolVar(&cfg.Verbose, "v", false, "Verbose output")

	flag.Parse()

	// Validate arguments
	if cfg.IsServer && cfg.IsClient {
		return nil, fmt.Errorf("cannot be both server and client")
	}
	if !cfg.IsServer && !cfg.IsClient {
		return nil, fmt.Errorf("must specify either -s or -c")
	}
	if cfg.Port == 0 {
		return nil, fmt.Errorf("missing port (-p)")
	}

	if cfg.IsClient {
		if cfg.Address == "" {
			return nil, fmt.Errorf("client must specify host (-a)")
		}
		cfg.Host = cfg.Address
	}

	return cfg, nil
}

func main() {
	cfg, err := parseCLI()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	// Set global verbose flag from config
	verbose = cfg.Verbose

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
		logf("Received signal %v, terminating promptly.\n", sig)
		os.Exit(0)
	case err := <-errCh:
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
	}
}
