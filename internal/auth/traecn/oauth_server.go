package traecn

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	log "github.com/sirupsen/logrus"
)

const successRedirectURL = "https://www.trae.com.cn/success"
const errorRedirectURL = "https://www.trae.com.cn/error"

// OAuthResult captures the outcome of the local OAuth callback.
type OAuthResult struct {
	Params map[string]string
	Error  error
}

// OAuthServer provides a minimal HTTP server for handling the Trae CN OAuth callback.
type OAuthServer struct {
	server  *http.Server
	port    int
	result  chan *OAuthResult
	errChan chan error
	mu      sync.Mutex
	running bool
}

// NewOAuthServer constructs a new OAuthServer bound to the provided port.
func NewOAuthServer(port int) *OAuthServer {
	return &OAuthServer{
		port:    port,
		result:  make(chan *OAuthResult, 1),
		errChan: make(chan error, 1),
	}
}

// Start launches the callback listener.
func (s *OAuthServer) Start() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.running {
		return fmt.Errorf("trae-cn oauth server already running")
	}
	if !s.isPortAvailable() {
		return fmt.Errorf("port %d is already in use", s.port)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/authorize", s.handleCallback)

	s.server = &http.Server{
		Addr:         fmt.Sprintf("127.0.0.1:%d", s.port),
		Handler:      mux,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
	}

	s.running = true

	go func() {
		if err := s.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			s.errChan <- err
		}
	}()

	time.Sleep(100 * time.Millisecond)
	return nil
}

// Stop gracefully terminates the callback listener.
func (s *OAuthServer) Stop(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.running || s.server == nil {
		return nil
	}
	defer func() {
		s.running = false
		s.server = nil
	}()
	return s.server.Shutdown(ctx)
}

// WaitForCallback blocks until a callback result, server error, or timeout occurs.
func (s *OAuthServer) WaitForCallback(timeout time.Duration) (*OAuthResult, error) {
	select {
	case res := <-s.result:
		return res, nil
	case err := <-s.errChan:
		return nil, err
	case <-time.After(timeout):
		return nil, fmt.Errorf("timeout waiting for Trae CN OAuth callback (%ds)", int(timeout.Seconds()))
	}
}

func (s *OAuthServer) handleCallback(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	query := r.URL.Query()
	params := make(map[string]string)
	for k, v := range query {
		if len(v) > 0 {
			params[k] = v[0]
		}
	}

	if len(params) == 0 {
		s.sendResult(&OAuthResult{Error: fmt.Errorf("no parameters received in callback")})
		s.writeSuccessPage(w, "No parameters received")
		return
	}

	s.sendResult(&OAuthResult{Params: params})
	s.writeSuccessPage(w, "Login Successful. You can close this window and return to the terminal.")
}

func (s *OAuthServer) writeSuccessPage(w http.ResponseWriter, message string) {
	html := fmt.Sprintf(`<!DOCTYPE html>
<html>
<head><title>Trae CN Authentication</title></head>
<body>
<h2>%s</h2>
<p>You can close this window and return to the terminal.</p>
</body>
</html>`, message)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(html))
}

func (s *OAuthServer) sendResult(res *OAuthResult) {
	select {
	case s.result <- res:
	default:
		log.Debug("trae-cn oauth result channel full, dropping result")
	}
}

func (s *OAuthServer) isPortAvailable() bool {
	addr := fmt.Sprintf("127.0.0.1:%d", s.port)
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return false
	}
	_ = listener.Close()
	return true
}
