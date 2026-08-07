// Getting the whisper model onto disk.
//
// The rules — what the model is, whether what is on disk is usable, how to word a progress line —
// are pure and live in store.WhisperModel and friends. This file is only the I/O: an HTTP GET that
// can resume, a hash, and a progress callback.
//
// WHY RESUMPTION IS NOT OPTIONAL: the file is 465 MB. On a domestic connection an interrupted
// download is the NORMAL case, not the rare one, and without a Range request every retry starts from
// zero. That is the difference between a failed attempt costing nothing and costing everything, and
// it is why store.ModelIncomplete is a verdict of its own rather than a flavour of "corrupt".
package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"syscall"
	"time"

	"github.com/Juan-Motta/loqui-go/internal/i18n"
	"github.com/Juan-Motta/loqui-go/internal/store"
)

// ModelProgress is one report while the download runs.
type ModelProgress struct {
	Received int64 `json:"received"`
	Total    int64 `json:"total"`
	Percent  int   `json:"percent"`
	// Text is the line the row shows, already worded — "120 MB de 465 MB · quedan 2 min".
	Text string `json:"text"`
}

// ModelStatus is what the row paints from.
type ModelStatus struct {
	Verdict store.ModelVerdict `json:"verdict"`
	// Bytes is what is on disk right now, so a resumable download can say how far it got.
	Bytes int64  `json:"bytes"`
	Total int64  `json:"total"`
	Label string `json:"label"`
	// Downloading is whether one is running, so the row shows Cancel instead of Download after a
	// repaint it did not trigger.
	Downloading bool `json:"downloading"`
	// Bundled marks a model that shipped beside the helper: a development copy this app must not
	// offer to delete, because it did not put it there and cannot get it back.
	Bundled bool `json:"bundled"`
	// StateText and StateClass are the row's sentence and the class that colours it, both decided in
	// Go — see modelStateText. The class travels for the same reason the connection badge's does: it
	// is what colours the line, and a page deriving it from the sentence would reimplement the
	// decision in another language.
	StateText  string `json:"stateText"`
	StateClass string `json:"stateClass"`
}

// modelDownloader owns the file and the one download that may be in flight.
type modelDownloader struct {
	spec store.ModelSpec
	// path is a function, not a string: the model lives beside the helper in a dev build and in the
	// data directory otherwise, and WhisperModelPath decides that per call.
	path   func() string
	client *http.Client
	// locale words the progress line. A function for the same reason the services use one: this
	// downloader outlives any single language.
	locale func() string

	mu      sync.Mutex
	running bool
	// cancelFn cancels the in-flight request's context. A context rather than a channel because a
	// channel cannot interrupt a blocked read — see download().
	cancelFn context.CancelFunc
}

func newModelDownloader(dataDir string, locale func() string) *modelDownloader {
	return &modelDownloader{
		spec:   store.WhisperModel,
		locale: locale,
		path:   func() string { return WhisperModelPath(dataDir) },
		// No overall timeout: this is a 465 MB transfer and a deadline that fits a small file would
		// kill a legitimate slow download halfway. Cancellation is explicit, through cancelCh.
		client: &http.Client{},
	}
}

// status reads what is on disk. It does NOT hash — see store.ModelUnverified for why that is a state
// of its own: hashing 465 MB takes seconds, and this runs on every repaint of the Conexiones view.
func (d *modelDownloader) status() ModelStatus {
	path := d.path()
	st := ModelStatus{Total: d.spec.Bytes, Label: d.spec.Label}
	d.mu.Lock()
	st.Downloading = d.running
	d.mu.Unlock()

	info, err := os.Stat(path)
	if err != nil {
		// No finished model. A .part is what an interrupted download left, and it is what makes the
		// row able to say "incomplete" and offer Resume rather than starting over.
		if part, perr := os.Stat(path + partSuffix); perr == nil {
			st.Bytes = part.Size()
			st.Verdict = store.VerdictFor(&store.ModelOnDisk{Bytes: part.Size()})
			st.Bundled = HelperPath(d.spec.File) != ""
			return st
		}
		st.Verdict = store.VerdictFor(nil)
		st.Bundled = HelperPath(d.spec.File) != ""
		return st
	}
	st.Bytes = info.Size()
	// A complete file at THIS path is reported ok without hashing, and that is only sound because of
	// how it got there: a download writes to `<path>.part` and is renamed in only after its digest
	// matches, so the canonical path never holds unverified bytes. Re-hashing 465 MB on every repaint
	// would freeze the view for seconds.
	//
	// The narrowing was UNSOUND before the .part file existed — a crash between the last byte and the
	// hash left a full-size unverified file here, and this said "ready". A cross-engine review caught
	// it, and the fix was structural rather than a stronger check.
	if info.Size() == d.spec.Bytes {
		st.Verdict = store.ModelVerdict{OK: true}
	} else {
		st.Verdict = store.VerdictFor(&store.ModelOnDisk{Bytes: info.Size()})
	}
	// A bundled copy sits beside the helper rather than in the data directory. Offering "Delete" for
	// it would remove something this app cannot download back into that location.
	st.Bundled = HelperPath(d.spec.File) != ""
	return st
}

// partSuffix names the in-progress file.
//
// THE WHOLE DESIGN TURNS ON THIS. Everything else in the app judges the model by its canonical path —
// dictation.go by existence, provider_fallback.go by size — so a partial or unverified file sitting
// there means Whisper is launched against garbage, and garbage transcription reads as a bug in this
// app rather than as a bad download. Bytes land in `<path>.part`; the rename happens only after the
// digest matches. A crash therefore leaves a resumable partial, never a fake model.
const partSuffix = ".part"

// download fetches the model, resuming when there is something to resume from.
func (d *modelDownloader) download(onProgress func(ModelProgress)) error {
	d.mu.Lock()
	if d.running {
		d.mu.Unlock()
		return errors.New("ya hay una descarga en curso")
	}
	// The CONTEXT is what cancellation rides on, not a bare channel. A channel checked before each
	// read cannot interrupt a server that stalls before headers or mid-body — Cancel would report
	// success while the transfer sat there for ever. Review finding.
	ctx, cancel := context.WithCancel(context.Background())
	d.running = true
	d.cancelFn = cancel
	d.mu.Unlock()
	defer func() {
		d.mu.Lock()
		d.running = false
		d.cancelFn = nil
		d.mu.Unlock()
		cancel()
	}()

	final := d.path()
	part := final + partSuffix
	if err := os.MkdirAll(filepath.Dir(final), 0o755); err != nil {
		return fmt.Errorf("no se pudo crear la carpeta del modelo: %w", err)
	}

	// What is in the .part decides whether this resumes. Anything at or past the full size is not
	// resumable — there is nothing left to ask for — so it is discarded rather than appended to.
	var existing int64
	if info, err := os.Stat(part); err == nil {
		if info.Size() >= d.spec.Bytes {
			if err := os.Remove(part); err != nil {
				return fmt.Errorf("no se pudo descartar el modelo anterior: %w", err)
			}
		} else {
			existing = info.Size()
		}
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, d.spec.URL, nil)
	if err != nil {
		return fmt.Errorf("no se pudo preparar la descarga: %w", err)
	}
	if r := store.RangeHeader(existing); r != "" {
		req.Header.Set("Range", r)
	}
	resp, err := d.client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return errCancelled
		}
		return fmt.Errorf("no se pudo descargar el modelo: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusPartialContent {
		return fmt.Errorf("el servidor del modelo respondió %d", resp.StatusCode)
	}

	// THREE ANSWERS TO ONE RANGE REQUEST, and only one of them may be appended.
	//
	//  · 206 starting where we asked      → append.
	//  · 200 (Range ignored)              → start over. Appending the whole body to a partial file
	//                                       makes a file of the right LENGTH out of the wrong bytes,
	//                                       which the digest catches only after another 465 MB.
	//  · 206 starting somewhere else      → refuse. A caching proxy is the realistic source, and the
	//                                       result would be a good prefix glued to a wrong suffix.
	appending := false
	if existing > 0 && resp.StatusCode == http.StatusPartialContent {
		start, ok := rangeStart(resp.Header.Get("Content-Range"))
		if !ok || start != existing {
			return fmt.Errorf("el servidor mandó un tramo que no encaja con la descarga a medias — "+
				"vuelve a intentarlo (pedimos desde %d)", existing)
		}
		appending = true
	}

	flags := os.O_CREATE | os.O_WRONLY
	if appending {
		flags |= os.O_APPEND
	} else {
		flags |= os.O_TRUNC
		existing = 0
	}
	// O_NOFOLLOW: this path is inside our own data directory, but a symlink planted there would have
	// this code truncate or append to whatever it points at. Cheap to refuse, awkward to explain.
	f, err := os.OpenFile(part, flags|syscall.O_NOFOLLOW, 0o644)
	if err != nil {
		return fmt.Errorf("no se pudo escribir el modelo: %w", err)
	}

	received, copyErr := d.stream(f, resp.Body, existing, ctx, onProgress)
	closeErr := f.Close()
	if copyErr != nil {
		// The .part is KEPT on a network failure or a cancel — that is the entire point of resuming.
		// It is only discarded when it is known to be wrong.
		return copyErr
	}
	if closeErr != nil {
		return fmt.Errorf("no se pudo cerrar el modelo: %w", closeErr)
	}
	if received != d.spec.Bytes {
		return fmt.Errorf("el modelo llegó incompleto (%s de %s)",
			store.FormatBytes(received), store.FormatBytes(d.spec.Bytes))
	}
	if err := d.verify(part); err != nil {
		return err
	}
	// PUBLISHED LAST, by rename, which on the same filesystem is atomic. Until this line the app has
	// no model; after it, the model it has is one whose digest was checked.
	if err := os.Rename(part, final); err != nil {
		return fmt.Errorf("no se pudo instalar el modelo descargado: %w", err)
	}
	return nil
}

// rangeStart reads the first byte offset out of a Content-Range header ("bytes 100-999/1000").
func rangeStart(header string) (int64, bool) {
	var start, end, total int64
	if _, err := fmt.Sscanf(header, "bytes %d-%d/%d", &start, &end, &total); err != nil {
		return 0, false
	}
	return start, true
}

// stream copies with progress and cancellation, returning the total size on disk.
func (d *modelDownloader) stream(dst io.Writer, src io.Reader, already int64, ctx context.Context,
	onProgress func(ModelProgress)) (int64, error) {
	received := already
	buf := make([]byte, 256*1024)
	started := time.Now()
	// THROTTLED. At this size the raw stream fires thousands of times a second, and every report
	// crosses into the webview — the original throttles for the same reason, in main.
	var lastReport time.Time
	report := func(force bool) {
		if onProgress == nil {
			return
		}
		if !force && time.Since(lastReport) < 200*time.Millisecond {
			return
		}
		lastReport = time.Now()
		onProgress(d.progress(received, already, started))
	}

	for {
		n, readErr := src.Read(buf)
		if n > 0 {
			if _, writeErr := dst.Write(buf[:n]); writeErr != nil {
				return received, fmt.Errorf("no se pudo escribir el modelo: %w", writeErr)
			}
			received += int64(n)
			report(false)
		}
		if readErr == io.EOF {
			report(true)
			return received, nil
		}
		if readErr != nil {
			// A cancel arrives here as a context error on the body read, which is why cancellation is
			// bound to the request rather than polled between reads.
			if ctx.Err() != nil {
				return received, errCancelled
			}
			return received, fmt.Errorf("se cortó la descarga del modelo: %w", readErr)
		}
	}
}

// progress words one report.
//
// `already` is what was on disk when this session started, and the rate is computed WITHOUT it.
// Including it made resuming absurd: 400 MB already there plus 1 MB in the first second reads as
// 401 MB/s, so a minute of work is announced as "0 s". Review finding.
func (d *modelDownloader) progress(received, already int64, started time.Time) ModelProgress {
	p := ModelProgress{Received: received, Total: d.spec.Bytes,
		Percent: store.ProgressPercent(received, d.spec.Bytes)}
	loc := ""
	if d.locale != nil {
		loc = d.locale()
	}
	// TRANSLATED HERE. The row renders this line verbatim, so an English session was showing a Spanish
	// progress line — the one string in this feature the page could not fix for itself. Review finding.
	text := i18n.T(i18n.Locale(loc), "{done} de {total}", map[string]string{
		"done": store.FormatBytes(received), "total": store.FormatBytes(d.spec.Bytes)})
	elapsed := time.Since(started).Seconds()
	if elapsed > 0 {
		rate := int64(float64(received-already) / elapsed)
		if eta, ok := store.ETASeconds(received, d.spec.Bytes, rate); ok {
			text += i18n.T(i18n.Locale(loc), " · quedan {eta}", map[string]string{"eta": humanETA(loc, eta)})
		}
	}
	p.Text = text
	return p
}

// humanETA words a remaining time. Minutes, not seconds, past a minute: "quedan 143 s" is precision
// nobody asked for on a download this long.
func humanETA(locale string, seconds int) string {
	if seconds < 60 {
		return i18n.T(i18n.Locale(locale), "{n} s", map[string]string{"n": strconv.Itoa(seconds)})
	}
	return i18n.T(i18n.Locale(locale), "{n} min", map[string]string{"n": strconv.Itoa((seconds + 30) / 60)})
}

var errCancelled = errors.New("descarga cancelada")

// verify hashes the finished file and DELETES it when the digest is wrong.
//
// Deleting is the load-bearing half. A right-sized file with the wrong contents is the worst outcome
// available: whisper loads it and transcribes garbage, which reads as a bug in this app rather than as
// a bad download — and the next status() would report it as ok, because status() does not hash.
func (d *modelDownloader) verify(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("no se pudo leer el modelo descargado: %w", err)
	}
	sum := sha256.New()
	_, copyErr := io.Copy(sum, f)
	closeErr := f.Close()
	if copyErr != nil {
		return fmt.Errorf("no se pudo comprobar el modelo descargado: %w", copyErr)
	}
	if closeErr != nil {
		return fmt.Errorf("no se pudo comprobar el modelo descargado: %w", closeErr)
	}
	if got := hex.EncodeToString(sum.Sum(nil)); got != d.spec.SHA256 {
		_ = os.Remove(path)
		return errors.New("el modelo descargado no coincide con el esperado — se descartó, vuelve a intentarlo")
	}
	return nil
}

// cancel stops a download in flight, leaving what has arrived on disk so it can be resumed.
func (d *modelDownloader) cancel() {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.cancelFn != nil {
		d.cancelFn()
	}
}

// remove deletes the model. Missing is success: the caller asked for it to be gone.
func (d *modelDownloader) remove() error {
	if err := os.Remove(d.path()); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("no se pudo eliminar el modelo: %w", err)
	}
	// The half-finished one goes too. Leaving it would make "Delete" report success while the row
	// went straight back to saying "incomplete".
	_ = os.Remove(d.path() + partSuffix)
	return nil
}
