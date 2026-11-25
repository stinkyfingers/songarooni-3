package control_panel

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"songarooni/state"
)

type ControlPanelServer struct {
	port     int
	mediaDir string
}

func NewControlPanelServer(port int, mediaDir string) *ControlPanelServer {
	return &ControlPanelServer{
		port:     port,
		mediaDir: mediaDir,
	}
}

//go:embed web
var rawAssets embed.FS

// webAssets is the embedded web/ directory, rooted so that index.html,
// app.js and styles.css sit at its top level — matching the absolute paths
// (/styles.css, /app.js) that index.html itself references.
var webAssets = mustSub(rawAssets, "web")

func mustSub(f embed.FS, dir string) fs.FS {
	sub, err := fs.Sub(f, dir)
	if err != nil {
		panic(err)
	}
	return sub
}

// Run builds the control panel's mux (the JSON API plus the embedded phone
// page) and serves it until shutdown is called.
func (s *ControlPanelServer) Run(controller *state.Controller) (func(context.Context) error, error) {
	mux := http.NewServeMux()
	mux.Handle("/api/", s.apiHandler(controller))
	mux.Handle("/", noCache(http.FileServerFS(webAssets)))

	server := &http.Server{Addr: fmt.Sprintf(":%d", s.port), Handler: mux}
	shutdown := server.Shutdown
	go func() {
		err := server.ListenAndServe()
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Println("error starting control-panel server", err)
		}
	}()
	return shutdown, nil
}

func (s *ControlPanelServer) apiHandler(controller *state.Controller) http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /api/data", func(w http.ResponseWriter, r *http.Request) {
		allowCrossOrigin(w) // the slideshow kiosk page, on its own port, polls this
		catalog := controller.Data()
		writeJSON(w, http.StatusOK, catalog)
	})

	mux.HandleFunc("GET /api/state", func(w http.ResponseWriter, r *http.Request) {
		allowCrossOrigin(w) // the slideshow kiosk page, on its own port, polls this
		writeJSON(w, http.StatusOK, controller.GetState())
	})

	mux.HandleFunc("GET /api/config", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, nil)
	})

	mux.HandleFunc("POST /api/select", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			ID int `json:"id"`
		}
		if !decodeJSON(w, r, &body) {
			return
		}
		if err := controller.Select(body.ID); err != nil {
			writeError(w, statusFor(err), err)
			return
		}
		writeJSON(w, http.StatusOK, controller.GetState())
	})

	mux.HandleFunc("POST /api/select/next", func(w http.ResponseWriter, r *http.Request) {
		if err := controller.SelectNext(); err != nil {
			writeError(w, statusFor(err), err)
			return
		}
		writeJSON(w, http.StatusOK, controller.GetState())
	})

	mux.HandleFunc("POST /api/select/previous", func(w http.ResponseWriter, r *http.Request) {
		if err := controller.SelectPrevious(); err != nil {
			writeError(w, statusFor(err), err)
			return
		}
		writeJSON(w, http.StatusOK, controller.GetState())
	})

	mux.HandleFunc("POST /api/start", func(w http.ResponseWriter, r *http.Request) {
		if err := controller.Start(); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		writeJSON(w, http.StatusOK, controller.GetState())
	})

	mux.HandleFunc("POST /api/stop", func(w http.ResponseWriter, r *http.Request) {
		if err := controller.Stop(); err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		writeJSON(w, http.StatusOK, controller.GetState())
	})

	mux.HandleFunc("POST /api/candid/upload", func(w http.ResponseWriter, r *http.Request) {
		handleCandidUpload(w, r, s.mediaDir)
	})

	return mux
}

// maxCandidUploadBytes bounds the whole multipart request — a backstop
// behind the client's own canvas-resize-to-JPEG step (see app.js), not
// the primary size control. Generous enough for several resized photos
// at once without ever letting an unprocessed multi-megapixel original
// through by accident.
const maxCandidUploadBytes = 50 << 20 // 50MB

// candidContentTypes maps a sniffed MIME type (via http.DetectContentType,
// not the client-supplied Content-Type or filename extension, both
// trivially wrong) to the extension saved files get. Deliberately narrow:
// only what the client's own resize step can actually produce, or a
// plain image passed through unresized.
var candidContentTypes = map[string]string{
	"image/jpeg": ".jpg",
	"image/png":  ".png",
	"image/gif":  ".gif",
}

// handleCandidUpload saves one or more uploaded photos into
// slideDir/candid/ — Slideshow.Show already picks up anything there fresh
// on its next call (see internal/slideshow's Resolve), so nothing here
// needs to notify the display explicitly.
func handleCandidUpload(w http.ResponseWriter, r *http.Request, slideDir string) {
	r.Body = http.MaxBytesReader(w, r.Body, maxCandidUploadBytes)
	if err := r.ParseMultipartForm(10 << 20); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	files := r.MultipartForm.File["photos"]
	if len(files) == 0 {
		writeError(w, http.StatusBadRequest, fmt.Errorf("no files in request"))
		return
	}

	dir := filepath.Join(slideDir, "candid")
	if err := os.MkdirAll(dir, 0755); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}

	saved := 0
	var failures []string
	for i, fh := range files {
		if err := saveCandidUpload(dir, i, fh); err != nil {
			failures = append(failures, fmt.Sprintf("%s: %v", fh.Filename, err))
			continue
		}
		saved++
	}

	status := http.StatusOK
	if saved == 0 {
		status = http.StatusBadRequest
	}
	writeJSON(w, status, map[string]any{"saved": saved, "errors": failures})
}

// saveCandidUpload writes one uploaded file into dir under a
// collision-safe name — a nanosecond timestamp plus the file's index
// within this request, since multiple phones uploading at the same
// moment (or one phone uploading several files in one picker trip) is a
// real scenario here, unlike every other slide category whose files are
// prepared and copied on ahead of time.
func saveCandidUpload(dir string, seq int, fh *multipart.FileHeader) error {
	f, err := fh.Open()
	if err != nil {
		return err
	}
	defer f.Close()

	sniff := make([]byte, 512)
	n, err := io.ReadFull(f, sniff)
	if err != nil && err != io.ErrUnexpectedEOF && err != io.EOF {
		return err
	}
	contentType := http.DetectContentType(sniff[:n])
	ext, ok := candidContentTypes[contentType]
	if !ok {
		return fmt.Errorf("unsupported file type %q", contentType)
	}

	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return err
	}

	name := fmt.Sprintf("%d_%d%s", time.Now().UnixNano(), seq, ext)
	out, err := os.Create(filepath.Join(dir, name))
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, f)
	return err
}

// statusFor picks 404 for an unknown song id and 400 for everything else
// SelectSong can return, matching Phase 6's "invalid song id -> 400/404"
// requirement.
func statusFor(err error) int {
	if err == nil {
		return http.StatusOK
	}
	const notFoundPrefix = "no song with id"
	msg := err.Error()
	if len(msg) >= len(notFoundPrefix) && msg[:len(notFoundPrefix)] == notFoundPrefix {
		return http.StatusNotFound
	}
	return http.StatusBadRequest
}

func decodeJSON(w http.ResponseWriter, r *http.Request, v any) bool {
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return false
	}
	return true
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Printf("control-panel: encoding response: %v", err)
	}
}

func writeError(w http.ResponseWriter, status int, err error) {
	writeJSON(w, status, map[string]string{"error": err.Error()})
}

func allowCrossOrigin(w http.ResponseWriter) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
}

func noCache(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		next.ServeHTTP(w, r)
	})
}
