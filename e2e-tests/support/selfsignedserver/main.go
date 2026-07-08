package main

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"math/big"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

const (
	defaultPort     = "18443"
	defaultPIDFile  = "e2e-tests/support/selfsignedserver/selfsignedserver.pid"
	expectedBody    = "self-signed e2e test content\n"
	readyTimeout    = 5 * time.Second
	stopGracePeriod = 2 * time.Second
)

var (
	startFlag          = flag.Bool("start", false, "start the server in the background and exit once ready")
	stopFlag           = flag.Bool("stop", false, "stop a server started with --start")
	restartIfRunning   = flag.Bool("restart-if-running", false, "with --start, stop an existing instance of this server on the port before starting")
	serveFlag          = flag.Bool("serve", false, "run the server in the foreground (used internally by --start)")
	portFlag           = flag.String("port", defaultPort, "TCP port to listen on")
	pidFileFlag        = flag.String("pid-file", "", "path to the PID file (default: next to this program)")
)

func main() {
	flag.Parse()

	port := strings.TrimSpace(*portFlag)
	if port == "" {
		log.Fatal("port must not be empty")
	}

	pidFile, err := resolvePIDFile(*pidFileFlag)
	if err != nil {
		log.Fatal(err)
	}

	switch {
	case *stopFlag:
		if err := stopServer(port, pidFile); err != nil {
			log.Fatal(err)
		}
	case *startFlag:
		if err := startServer(port, pidFile, *restartIfRunning); err != nil {
			log.Fatal(err)
		}
	case *serveFlag:
		if err := runServer(port, pidFile); err != nil {
			log.Fatal(err)
		}
	default:
		log.Fatal("one of --start, --stop, or --serve is required")
	}
}

func resolvePIDFile(path string) (string, error) {
	if path != "" {
		return path, nil
	}
	return defaultPIDFile, nil
}

func startServer(port, pidFile string, restart bool) error {
	if restart {
		if err := stopIfOwned(port, pidFile); err != nil {
			return err
		}
	} else if inUse, err := portInUse(port); err != nil {
		return err
	} else if inUse {
		return fmt.Errorf("port %s is already in use; pass --restart-if-running to replace an existing instance", port)
	}

	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("resolve executable path: %w", err)
	}

	cmd := exec.Command(exe, "--serve", "--port", port, "--pid-file", pidFile)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start server process: %w", err)
	}

	if err := waitForReady(port); err != nil {
		_ = cmd.Process.Kill()
		_, _ = cmd.Process.Wait()
		return err
	}

	log.Printf("self-signed e2e server started on https://127.0.0.1:%s/self-signed-test.txt (pid %d)", port, cmd.Process.Pid)
	return nil
}

func stopServer(port, pidFile string) error {
	pid, err := readPIDFile(pidFile)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			if isOurServer(port) {
				return fmt.Errorf("server is running on port %s but pid file %s is missing", port, pidFile)
			}
			return nil
		}
		return err
	}

	if err := signalProcess(pid, syscall.SIGTERM); err != nil {
		return err
	}

	deadline := time.Now().Add(stopGracePeriod)
	for time.Now().Before(deadline) {
		if !processAlive(pid) && !isOurServer(port) {
			_ = os.Remove(pidFile)
			log.Printf("self-signed e2e server stopped (pid %d)", pid)
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}

	if err := signalProcess(pid, syscall.SIGKILL); err != nil {
		return err
	}
	_ = os.Remove(pidFile)
	if isOurServer(port) {
		return fmt.Errorf("server on port %s did not stop after SIGKILL", port)
	}
	log.Printf("self-signed e2e server stopped (pid %d)", pid)
	return nil
}

func stopIfOwned(port, pidFile string) error {
	inUse, err := portInUse(port)
	if err != nil {
		return err
	}
	if !inUse {
		return nil
	}
	if !isOurServer(port) {
		pid, findErr := findListenerPID(port)
		if findErr != nil {
			return fmt.Errorf("port %s is already in use by another program", port)
		}
		return fmt.Errorf("port %s is already in use by another program (pid %d)", port, pid)
	}
	return stopServer(port, pidFile)
}

func runServer(port, pidFile string) error {
	if inUse, err := portInUse(port); err != nil {
		return err
	} else if inUse {
		if isOurServer(port) {
			return fmt.Errorf("server is already running on port %s", port)
		}
		pid, findErr := findListenerPID(port)
		if findErr != nil {
			return fmt.Errorf("port %s is already in use by another program", port)
		}
		return fmt.Errorf("port %s is already in use by another program (pid %d)", port, pid)
	}

	if err := os.WriteFile(pidFile, []byte(strconv.Itoa(os.Getpid())+"\n"), 0o644); err != nil {
		return fmt.Errorf("write pid file: %w", err)
	}
	defer os.Remove(pidFile)

	cert, err := selfSignedCert()
	if err != nil {
		return err
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/self-signed-test.txt", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte(expectedBody))
	})

	ln, err := tls.Listen("tcp", ":"+port, &tls.Config{Certificates: []tls.Certificate{cert}})
	if err != nil {
		return fmt.Errorf("listen on port %s: %w", port, err)
	}

	srv := &http.Server{Handler: mux}
	errCh := make(chan error, 1)
	go func() {
		errCh <- srv.Serve(ln)
	}()

	log.Printf("self-signed e2e server listening on https://127.0.0.1:%s/self-signed-test.txt", port)

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	select {
	case sig := <-sigCh:
		log.Printf("received %s, shutting down", sig)
	case err := <-errCh:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			return err
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	return srv.Shutdown(ctx)
}

func waitForReady(port string) error {
	deadline := time.Now().Add(readyTimeout)
	for time.Now().Before(deadline) {
		if isOurServer(port) {
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	return fmt.Errorf("server on port %s did not become ready within %s", port, readyTimeout)
}

func isOurServer(port string) bool {
	client := &http.Client{
		Timeout: 1 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		},
	}
	resp, err := client.Get("https://127.0.0.1:" + port + "/self-signed-test.txt")
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return false
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 256))
	if err != nil {
		return false
	}
	return string(body) == expectedBody
}

func portInUse(port string) (bool, error) {
	ln, err := net.Listen("tcp", ":"+port)
	if err != nil {
		var opErr *net.OpError
		if errors.As(err, &opErr) && errors.Is(opErr.Err, syscall.EADDRINUSE) {
			return true, nil
		}
		return false, fmt.Errorf("check port %s: %w", port, err)
	}
	_ = ln.Close()
	return false, nil
}

func readPIDFile(path string) (int, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		return 0, fmt.Errorf("parse pid file %s: %w", path, err)
	}
	return pid, nil
}

func signalProcess(pid int, sig syscall.Signal) error {
	if pid <= 0 {
		return fmt.Errorf("invalid pid %d", pid)
	}
	if err := syscall.Kill(pid, sig); err != nil {
		if errors.Is(err, syscall.ESRCH) {
			return nil
		}
		return fmt.Errorf("signal pid %d: %w", pid, err)
	}
	return nil
}

func processAlive(pid int) bool {
	return syscall.Kill(pid, 0) == nil
}

func findListenerPID(port string) (int, error) {
	portNum, err := strconv.Atoi(port)
	if err != nil {
		return 0, fmt.Errorf("parse port %q: %w", port, err)
	}

	inode, err := findTCPListenerInode(portNum)
	if err != nil {
		return 0, err
	}
	return findPIDBySocketInode(inode)
}

func findTCPListenerInode(port int) (string, error) {
	data, err := os.ReadFile("/proc/net/tcp")
	if err != nil {
		return "", fmt.Errorf("read /proc/net/tcp: %w", err)
	}

	targetPort := fmt.Sprintf("%04X", port)
	for _, line := range strings.Split(string(data), "\n")[1:] {
		fields := strings.Fields(line)
		if len(fields) < 10 {
			continue
		}
		localAddress := fields[1]
		state := fields[3]
		inode := fields[9]
		parts := strings.Split(localAddress, ":")
		if len(parts) != 2 {
			continue
		}
		if state != "0A" || parts[1] != targetPort {
			continue
		}
		return inode, nil
	}
	return "", fmt.Errorf("no listener found on port %d", port)
}

func findPIDBySocketInode(inode string) (int, error) {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return 0, fmt.Errorf("read /proc: %w", err)
	}

	socketPrefix := "socket:[" + inode + "]"
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		pid, err := strconv.Atoi(entry.Name())
		if err != nil {
			continue
		}
		fdDir := filepath.Join("/proc", entry.Name(), "fd")
		fds, err := os.ReadDir(fdDir)
		if err != nil {
			continue
		}
		for _, fd := range fds {
			link, err := os.Readlink(filepath.Join(fdDir, fd.Name()))
			if err != nil {
				continue
			}
			if link == socketPrefix {
				return pid, nil
			}
		}
	}
	return 0, fmt.Errorf("no process found for socket inode %s", inode)
}

func selfSignedCert() (tls.Certificate, error) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return tls.Certificate{}, fmt.Errorf("generate key: %w", err)
	}

	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return tls.Certificate{}, fmt.Errorf("generate serial: %w", err)
	}

	template := x509.Certificate{
		SerialNumber: serial,
		Subject: pkix.Name{
			CommonName: "godownload e2e self-signed",
		},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(24 * time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		DNSNames:              []string{"127.0.0.1", "localhost"},
	}

	der, err := x509.CreateCertificate(rand.Reader, &template, &template, &key.PublicKey, key)
	if err != nil {
		return tls.Certificate{}, fmt.Errorf("create certificate: %w", err)
	}

	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})

	return tls.X509KeyPair(certPEM, keyPEM)
}
