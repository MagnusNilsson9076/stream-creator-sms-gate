package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/infrai-examples/stream-creator-sms-gate/internal/infrai"
	"github.com/infrai-examples/stream-creator-sms-gate/internal/media"
)

type codeService interface {
	RequestCode(context.Context, string, string) error
	VerifyCode(context.Context, string, string, string) error
}

type service struct {
	sms      codeService
	pipeline *media.Pipeline
	mu       sync.RWMutex
	sessions map[string]string
}

type loginInput struct {
	Phone string `json:"phone"`
	Code  string `json:"code,omitempty"`
}

type assetInput struct {
	ID      string `json:"id"`
	Creator string `json:"creator"`
	Title   string `json:"title"`
}

type assetAction struct {
	AssetID string `json:"asset_id"`
}

func main() {
	apiKey := os.Getenv("INFRAI_API_KEY")
	if apiKey == "" {
		log.Fatal("INFRAI_API_KEY is required")
	}
	addr := os.Getenv("ADDR")
	if addr == "" {
		addr = ":8080"
	}

	app := &service{
		sms:      infrai.NewSMSClient(apiKey),
		pipeline: media.NewPipeline(),
		sessions: make(map[string]string),
	}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /login/code", app.requestCode)
	mux.HandleFunc("POST /login/verify", app.verifyCode)
	mux.HandleFunc("POST /assets", app.requireSession(app.ingestAsset))
	mux.HandleFunc("POST /jobs/start", app.requireSession(app.startJob))
	mux.HandleFunc("POST /jobs/complete", app.requireSession(app.completeJob))
	mux.HandleFunc("POST /deliveries", app.requireSession(app.deliverAsset))
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})

	server := &http.Server{Addr: addr, Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	go func() {
		log.Printf("stream access listening on %s", addr)
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatal(err)
		}
	}()

	stop, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()
	<-stop.Done()
	ctx, shutdown := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdown()
	if err := server.Shutdown(ctx); err != nil {
		log.Printf("shutdown: %v", err)
	}
}

func (s *service) requestCode(w http.ResponseWriter, r *http.Request) {
	var input loginInput
	requestID := r.Header.Get("Idempotency-Key")
	if decodeJSON(w, r, &input) != nil || input.Phone == "" || requestID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "phone and Idempotency-Key are required"})
		return
	}
	if err := s.sms.RequestCode(r.Context(), input.Phone, requestID+":request"); err != nil {
		writeUpstreamError(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]string{"status": "code_sent"})
}

func (s *service) verifyCode(w http.ResponseWriter, r *http.Request) {
	var input loginInput
	requestID := r.Header.Get("Idempotency-Key")
	if decodeJSON(w, r, &input) != nil || input.Phone == "" || input.Code == "" || requestID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "phone, code, and Idempotency-Key are required"})
		return
	}
	if err := s.sms.VerifyCode(r.Context(), input.Phone, input.Code, requestID+":verify"); err != nil {
		writeUpstreamError(w, err)
		return
	}
	token, err := randomToken()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "could not create session"})
		return
	}
	s.mu.Lock()
	s.sessions[token] = input.Phone
	s.mu.Unlock()
	writeJSON(w, http.StatusOK, map[string]string{"status": "verified", "session_token": token})
}

func (s *service) ingestAsset(w http.ResponseWriter, r *http.Request) {
	var input assetInput
	if decodeJSON(w, r, &input) != nil {
		return
	}
	asset, err := s.pipeline.Ingest(media.Asset{ID: input.ID, Creator: input.Creator, Title: input.Title})
	writeAsset(w, asset, err)
}

func (s *service) startJob(w http.ResponseWriter, r *http.Request) {
	s.advance(w, r, media.StageProcessing)
}

func (s *service) completeJob(w http.ResponseWriter, r *http.Request) {
	s.advance(w, r, media.StageReady)
}

func (s *service) deliverAsset(w http.ResponseWriter, r *http.Request) {
	s.advance(w, r, media.StageDelivered)
}

func (s *service) advance(w http.ResponseWriter, r *http.Request, target media.Stage) {
	var input assetAction
	if decodeJSON(w, r, &input) != nil {
		return
	}
	asset, err := s.pipeline.Advance(input.AssetID, target)
	writeAsset(w, asset, err)
}

func (s *service) requireSession(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		s.mu.RLock()
		_, verified := s.sessions[token]
		s.mu.RUnlock()
		if token == "" || !verified {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "verified session required"})
			return
		}
		next(w, r)
	}
}

func writeAsset(w http.ResponseWriter, asset media.Asset, err error) {
	if err != nil {
		writeJSON(w, http.StatusConflict, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, asset)
}

func writeUpstreamError(w http.ResponseWriter, err error) {
	var apiErr *infrai.APIError
	if errors.As(err, &apiErr) && apiErr.HTTPStatus >= 400 && apiErr.HTTPStatus < 500 {
		writeJSON(w, apiErr.HTTPStatus, map[string]string{"error": apiErr.Code})
		return
	}
	writeJSON(w, http.StatusBadGateway, map[string]string{"error": "sms service request failed"})
}

func decodeJSON(w http.ResponseWriter, r *http.Request, target any) error {
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON body"})
		return err
	}
	return nil
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(value); err != nil {
		log.Printf("encode response: %v", err)
	}
}

func randomToken() (string, error) {
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
