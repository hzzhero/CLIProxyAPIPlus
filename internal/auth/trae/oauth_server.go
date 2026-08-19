package trae

import (
	"context"
	"errors"
	"fmt"
	"html"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	log "github.com/sirupsen/logrus"
)

// OAuthServer handles the local HTTP server for Trae OAuth callbacks.
// It listens on the `/authorize` path (matching the cockpit-tools
// callback layout) and parses the full Trae callback parameter set.
type OAuthServer struct {
	server       *http.Server
	port         int
	callbackPath string
	resultChan   chan *CallbackParams
	errorChan    chan error
	mu           sync.Mutex
	running      bool
}

// NewOAuthServer creates a new Trae OAuth callback server.
// If port == 0 the OS assigns an available port; the caller reads
// the effective port back via Port() after Start().
func NewOAuthServer(port int) *OAuthServer {
	return &OAuthServer{
		port:         port,
		callbackPath: "/authorize",
		resultChan:   make(chan *CallbackParams, 1),
		errorChan:    make(chan error, 1),
	}
}

// Port returns the effective listen port. Useful when NewOAuthServer(0)
// was used to request a kernel-assigned port.
func (s *OAuthServer) Port() int { return s.port }

// Start binds the HTTP listener and begins serving.
func (s *OAuthServer) Start() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.running {
		return fmt.Errorf("trae oauth server: already running")
	}

	// If caller asked for port 0, bind now to discover the actual port
	listener, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", s.port))
	if err != nil {
		return fmt.Errorf("trae oauth server: port %d bind failed: %w", s.port, err)
	}
	// Extract the real port (important when caller passed 0)
	tcpAddr, ok := listener.Addr().(*net.TCPAddr)
	if !ok {
		_ = listener.Close()
		return fmt.Errorf("trae oauth server: listener is not TCP")
	}
	s.port = tcpAddr.Port

	mux := http.NewServeMux()
	mux.HandleFunc(s.callbackPath, s.handleCallback)

	s.server = &http.Server{
		Addr:         fmt.Sprintf("127.0.0.1:%d", s.port),
		Handler:      mux,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
	}
	s.running = true

	go func() {
		if serveErr := s.server.Serve(listener); serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) {
			s.errorChan <- fmt.Errorf("trae oauth server: serve error: %w", serveErr)
		}
	}()
	// Give the listener a moment to stabilize (codex pattern)
	time.Sleep(100 * time.Millisecond)
	return nil
}

// Stop gracefully shuts the server down. Safe to call multiple times.
func (s *OAuthServer) Stop(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.running || s.server == nil {
		return nil
	}
	log.Debug("trae oauth server: stopping")
	shutdownCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	err := s.server.Shutdown(shutdownCtx)
	s.running = false
	s.server = nil
	return err
}

// WaitForCallback blocks until a callback is delivered, an internal
// server error surfaces, or timeout elapses. Trae timeout is 10
// minutes (600s) per the cockpit-tools reference.
func (s *OAuthServer) WaitForCallback(timeout time.Duration) (*CallbackParams, error) {
	select {
	case result := <-s.resultChan:
		return result, nil
	case err := <-s.errorChan:
		return nil, err
	case <-time.After(timeout):
		return nil, fmt.Errorf("trae oauth server: timeout waiting for callback")
	}
}

// handleCallback parses the `/authorize` callback, extracts Trae's
// full parameter vocabulary, and renders a human-facing HTML page.
// Trae sometimes places parameters in the URL fragment (after #) so
// the pending page ships a script that rewrites the hash into the
// query portion and retries the request.
func (s *OAuthServer) handleCallback(w http.ResponseWriter, r *http.Request) {
	log.Debugf("trae oauth server: callback %s?%s", r.URL.Path, r.URL.RawQuery)
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	cb := ParseCallbackFromURL(r.URL)
	if cb == nil {
		writeHTML(w, http.StatusOK, callbackPendingHTML())
		return
	}

	if cb.Error != "" {
		msg := cb.Error
		if cb.ErrorDescription != "" {
			msg = fmt.Sprintf("%s (%s)", cb.Error, cb.ErrorDescription)
		}
		log.Warnf("trae oauth server: callback error %s", msg)
		s.sendCallback(cb)
		writeHTML(w, http.StatusBadRequest, callbackFailureHTML(msg))
		return
	}

	if cb.AuthCode == "" && cb.RefreshToken == "" {
		if r.URL.RawQuery == "" && r.URL.Fragment == "" {
			writeHTML(w, http.StatusOK, callbackPendingHTML())
			return
		}
		if r.URL.Fragment == "" {
			msg := "callback missing authCode/authCodeInfo or refreshToken"
			log.Warnf("trae oauth server: %s (params=%v)", msg, cb.RawQuery)
			cb.Error = "missing_payload"
			cb.ErrorDescription = msg
			s.sendCallback(cb)
			writeHTML(w, http.StatusBadRequest, callbackFailureHTML(msg))
			return
		}
		writeHTML(w, http.StatusOK, callbackPendingHTML())
		return
	}

	s.sendCallback(cb)
	writeHTML(w, http.StatusOK, callbackSuccessHTML())
}

// sendCallback publishes a CallbackParams exactly once to the buffered
// resultChan.
func (s *OAuthServer) sendCallback(cb *CallbackParams) {
	select {
	case s.resultChan <- cb:
		log.Debug("trae oauth server: callback delivered")
	default:
		log.Warn("trae oauth server: resultChan full (already delivered); dropping duplicate callback")
	}
}

// writeHTML writes an HTML response with the appropriate Content-Type.
func writeHTML(w http.ResponseWriter, status int, body string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	_, _ = w.Write([]byte(body))
}

// ---------------------------------------------------------------------------
// HTML pages (mirror cockpit-tools' visual style: dark card, branded)
// ---------------------------------------------------------------------------

const pageShell = `<!doctype html>
<html lang="zh-CN">
<head>
<meta charset="utf-8" />
<meta name="viewport" content="width=device-width, initial-scale=1" />
<title>Trae OAuth - CLIProxyAPI</title>
<style>
:root { --bg:#0f172a; --panel:#111827; --panel-strong:#172033; --line:rgba(148,163,184,.24); --text:#e5edf8; --muted:#9aa7bc; --success:#22c55e; --warning:#38bdf8; --danger:#ef4444; }
*{box-sizing:border-box;}
body{margin:0;min-height:100vh;display:grid;place-items:center;padding:24px;background:#0f172a;color:var(--text);font-family:-apple-system,BlinkMacSystemFont,"SF Pro Text","Segoe UI",sans-serif;}
.card{width:min(520px,100%);padding:30px;border:1px solid var(--line);border-radius:18px;background:rgba(17,24,39,.94);box-shadow:0 24px 80px rgba(0,0,0,.34);}
.brand{display:flex;align-items:center;gap:12px;margin-bottom:24px;color:#bfdbfe;font-size:13px;font-weight:700;letter-spacing:.04em;text-transform:uppercase;}
.mark{display:grid;place-items:center;width:32px;height:32px;border-radius:9px;background:#1d4ed8;color:#eff6ff;font-size:17px;}
.status{display:inline-flex;align-items:center;gap:8px;margin-bottom:14px;padding:7px 10px;border-radius:999px;background:var(--panel-strong);color:var(--muted);font-size:13px;font-weight:650;}
.status.success{color:var(--success);} .status.pending{color:var(--warning);} .status.failure{color:var(--danger);}
h1{margin:0 0 10px;font-size:26px;line-height:1.25;}
p{margin:0;color:var(--muted);font-size:15px;line-height:1.7;word-break:break-word;}
.foot{margin-top:24px;padding-top:18px;border-top:1px solid var(--line);color:#64748b;font-size:13px;}
</style>
</head>
<body><main class="card">
<div class="brand"><span class="mark">T</span><span>CLIProxyAPI</span></div>
__STATUS__
<h1>__TITLE__</h1>
<p id="hint">__MESSAGE__</p>
<div class="foot">完成后可以关闭此页面，回到命令行继续操作。</div>
</main>__SCRIPT__</body></html>`

func renderPage(tone, badge, title, message, script string) string {
	out := pageShell
	out = strings.Replace(out, "__STATUS__", fmt.Sprintf(`<div class="status %s">%s</div>`, tone, html.EscapeString(badge)), 1)
	out = strings.Replace(out, "__TITLE__", html.EscapeString(title), 1)
	out = strings.Replace(out, "__MESSAGE__", html.EscapeString(message), 1)
	out = strings.Replace(out, "__SCRIPT__", script, 1)
	return out
}

func callbackSuccessHTML() string {
	return renderPage("success", "授权成功",
		"Trae 登录回调已完成",
		"授权结果已经传回本机服务。",
		"")
}

func callbackPendingHTML() string {
	// If a fragment is present the inline script rewrites the hash to
	// the query string and reloads; otherwise it hints the user to retry.
	const fragmentScript = `<script>(function(){
if(window.location.hash&&window.location.hash.length>1){
var h=window.location.hash.slice(1);
window.location.replace(window.location.origin+window.location.pathname+'?'+h);
return;}
document.getElementById('hint').textContent='未检测到授权参数，请完成登录后重试。';})();</script>`
	return renderPage("pending", "正在处理",
		"正在解析授权结果",
		"请稍候，页面将自动完成回调。",
		fragmentScript)
}

func callbackFailureHTML(message string) string {
	return renderPage("failure", "授权失败",
		"Trae 登录回调失败",
		message,
		"")
}
