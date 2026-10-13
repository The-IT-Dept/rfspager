package recvhub

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/the-it-dept/rfspager/internal/db"
	"github.com/the-it-dept/rfspager/internal/rxproto"
)

// MaxCaptureBytes caps one capture upload. A 30 s IQ capture at 38.4 kS/s
// complex int16 is 4.6 MB and audio half that; the cap leaves room for
// wider captures without letting a misbehaving receiver fill the disk.
const MaxCaptureBytes int64 = 64 << 20

// captureCap is the cap the handler enforces; tests lower it so the
// over-limit path can be exercised without a 64 MB body.
var captureCap = MaxCaptureBytes

// captureUploadTimeout replaces the server's ordinary read/write deadlines
// for the duration of an upload: a Pi on a 4G uplink needs minutes, not
// the ten seconds API calls get.
const captureUploadTimeout = 10 * time.Minute

// CaptureUploadHandler builds PUT /api/receiver/captures/{id}: a receiver
// uploads the file for a capture it was commanded to make. apikey auth
// (same headers as the websocket and /receiver/me), body is the raw
// capture, X-Capture-Kind / X-Capture-Rate / X-Capture-Channel describe
// it. The file lands at dir/{system_id}/{id}.{json|bin} and the row is
// marked ready. Must be mounted outside the request-timeout middleware.
func CaptureUploadHandler(database *db.DB, dir string) http.HandlerFunc {
	if abs, err := filepath.Abs(dir); err == nil {
		dir = abs
	}
	return func(w http.ResponseWriter, r *http.Request) {
		apiKey := r.Header.Get("apikey")
		if apiKey == "" {
			apiKey = r.Header.Get("X-Api-Key")
		}
		if apiKey == "" {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "apikey required"})
			return
		}
		sys, err := database.GetSystemByAPIKey(r.Context(), apiKey)
		if err != nil {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unknown api key"})
			return
		}

		id := chi.URLParam(r, "id")
		if _, err := uuid.Parse(id); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid capture id"})
			return
		}
		capture, err := database.GetSystemCapture(r.Context(), id)
		if err != nil || capture.SystemID != sys.ID {
			// Another system's capture looks the same as a missing one, so
			// a receiver cannot probe for ids.
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "unknown capture"})
			return
		}
		if capture.Status == db.CaptureReady {
			writeJSON(w, http.StatusConflict, map[string]string{"error": "capture already uploaded"})
			return
		}
		if kind := r.Header.Get("X-Capture-Kind"); kind != "" && kind != capture.Kind {
			writeJSON(w, http.StatusBadRequest, map[string]string{
				"error": fmt.Sprintf("capture %s is %s, not %s", id, capture.Kind, kind)})
			return
		}
		rate, _ := strconv.Atoi(r.Header.Get("X-Capture-Rate"))
		channel := r.Header.Get("X-Capture-Channel")

		rc := http.NewResponseController(w)
		_ = rc.SetReadDeadline(time.Now().Add(captureUploadTimeout))
		_ = rc.SetWriteDeadline(time.Now().Add(captureUploadTimeout))

		ext := "bin"
		if capture.Kind == rxproto.CaptureSpectrum {
			ext = "json"
		}
		sysDir := filepath.Join(dir, strconv.FormatInt(sys.ID, 10))
		if err := os.MkdirAll(sysDir, 0o750); err != nil {
			slog.Error("capture upload: create directory", "dir", sysDir, "err", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "cannot store capture"})
			return
		}
		final := filepath.Join(sysDir, id+"."+ext)
		part := final + ".part"
		f, err := os.OpenFile(part, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o640)
		if err != nil {
			slog.Error("capture upload: create file", "path", part, "err", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "cannot store capture"})
			return
		}

		// One byte over the cap is enough to know the body is too big
		// without reading the rest of it.
		n, copyErr := io.Copy(f, io.LimitReader(r.Body, captureCap+1))
		closeErr := f.Close()

		// The client may already be gone by the time we record the outcome,
		// so status writes use their own context.
		bg, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		var tooBig *http.MaxBytesError
		switch {
		case errors.As(copyErr, &tooBig) || n > captureCap:
			_ = os.Remove(part)
			reason := fmt.Sprintf("upload exceeded the %d byte cap", captureCap)
			if err := database.UpdateSystemCaptureStatus(bg, id, db.CaptureError, reason); err != nil {
				slog.Warn("capture upload: mark error", "capture_id", id, "err", err)
			}
			writeJSON(w, http.StatusRequestEntityTooLarge, map[string]string{"error": reason})
			return
		case copyErr != nil:
			_ = os.Remove(part)
			slog.Warn("capture upload: read body", "capture_id", id, "err", copyErr)
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "upload interrupted"})
			return
		case n == 0:
			_ = os.Remove(part)
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "empty capture body"})
			return
		case closeErr != nil:
			_ = os.Remove(part)
			slog.Error("capture upload: close file", "path", part, "err", closeErr)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "cannot store capture"})
			return
		}
		if err := os.Rename(part, final); err != nil {
			_ = os.Remove(part)
			slog.Error("capture upload: finalise file", "path", final, "err", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "cannot store capture"})
			return
		}

		if err := database.MarkSystemCaptureReady(bg, id, final, n, rate, channel); err != nil {
			slog.Error("capture upload: mark ready", "capture_id", id, "err", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "cannot record capture"})
			return
		}
		if channel == "" && capture.Channel != nil {
			channel = *capture.Channel
		}
		slog.Info("capture uploaded", "system_id", sys.ID, "capture_id", id, "kind", capture.Kind, "bytes", n)
		EmitEvent(bg, database.Conn(), sys.ID, EvtReceiverCaptureReady, SeverityInfo,
			fmt.Sprintf("Capture ready: %s (%s)", capture.Kind, humanBytes(n)),
			map[string]any{
				"capture_id":  id,
				"command_id":  capture.CommandID,
				"kind":        capture.Kind,
				"channel":     channel,
				"size_bytes":  n,
				"sample_rate": rate,
			})
		writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "id": id, "size_bytes": n})
	}
}

func humanBytes(n int64) string {
	switch {
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(n)/float64(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.0f KB", float64(n)/float64(1<<10))
	}
	return fmt.Sprintf("%d B", n)
}
