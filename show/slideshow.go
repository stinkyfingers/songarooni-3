package show

import (
	"context"
	_ "embed"
	"errors"
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"

	"songarooni/data"
)

//go:embed bkgd.jpg
var embeddedBkgd []byte

// Slideshow serves the kiosk page over HTTP.
type Slideshow struct {
	showPort  int
	parentDir string // config's SlideDir
	data      data.Data
	dataMap   map[int]data.Feature

	defaultLength    time.Duration
	controlPanelPort int
	stateChan        chan string

	// advanceChan carries one signal per POST /advance — the browser's way
	// of telling displayData it actually revealed the current slide, so
	// the real dwell timer can start from when it was genuinely shown
	// rather than from whenever it was pushed. Buffered so a POST never
	// blocks waiting for displayData to be ready to receive it.
	advanceChan chan struct{}

	showMu     sync.Mutex
	ctx        context.Context    // set by Run; Show's cycling goroutines exit when this is done
	cancelShow context.CancelFunc // cancels whichever deck Show previously started cycling, if any
}

// New prepares a Slideshow listening on addr. apiPort is the main server's
// listen port (parsed from config.yaml's addr) — the kiosk browser always
// reaches the main server at the same hostname it reaches this one at,
// just a different port, so only the port number needs to travel to the
// page, not a full URL.
func New(showPort int, parentDir string, featureData data.Data, defaultLength time.Duration, controlPanelPort int) *Slideshow {
	dataMap := make(map[int]data.Feature)
	for i := range featureData {
		dataMap[featureData[i].ID] = featureData[i]
	}
	return &Slideshow{
		showPort:         showPort,
		parentDir:        parentDir,
		data:             featureData,
		dataMap:          dataMap,
		defaultLength:    defaultLength,
		controlPanelPort: controlPanelPort,
		stateChan:        make(chan string),
		advanceChan:      make(chan struct{}, 1),
		ctx:              context.Background(), // overwritten by Run; keeps Show safe to call even earlier
	}
}

// Run serves the kiosk page and its assets until ctx is done.
func (s *Slideshow) Run(ctx context.Context) (func(context.Context) error, error) {
	s.showMu.Lock()
	s.ctx = ctx
	s.showMu.Unlock()

	mux := http.NewServeMux()
	mux.HandleFunc("GET /bkgd.jpg", s.handleBkgd)
	mux.HandleFunc("GET /images/", s.handleImages)
	mux.HandleFunc("GET /events", s.sseHandler)
	mux.HandleFunc("POST /advance", s.handleAdvance)
	mux.Handle("GET /", http.FileServerFS(webAssets)) // index.html, app.js, styles.css

	server := &http.Server{Addr: fmt.Sprintf(":%d", s.showPort), Handler: mux}
	shutdown := server.Shutdown
	go func() {
		err := server.ListenAndServe()
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Println("error starting show server", err)
		}
	}()
	return shutdown, nil
}

// handleAdvance is the browser's "I actually revealed the current slide"
// signal (called from app.js's reveal() and its load-error path alike —
// either way, displayData should stop waiting). Non-blocking send: if a
// signal is already pending (a duplicate POST, or one that arrived after
// displayData gave up on it), this is a no-op rather than piling up a
// second one that would wrongly short-circuit a later slide's own wait.
func (s *Slideshow) handleAdvance(w http.ResponseWriter, r *http.Request) {
	select {
	case s.advanceChan <- struct{}{}:
	default:
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Slideshow) sseHandler(w http.ResponseWriter, r *http.Request) {
	// Set required SSE headers
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	// Ensure the ResponseWriter supports flushing
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming unsupported", http.StatusInternalServerError)
		return
	}

	for {
		select {
		case msg := <-s.stateChan:
			w.Write([]byte("event: data\ndata: " + msg + "\n\n"))
			flusher.Flush()
		case <-r.Context().Done():
			return
		}
	}

}

func (s *Slideshow) handleImages(w http.ResponseWriter, r *http.Request) {
	http.StripPrefix("/images/", http.FileServer(http.Dir(s.parentDir))).ServeHTTP(w, r)
}

func (s *Slideshow) handleBkgd(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "image/jpeg")
	_, _ = w.Write(embeddedBkgd)
}
