package app

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Juan-Motta/loqui-go/internal/store"
)

// A REAL HTTP SERVER, not a stubbed client. What is under test is resumption — a Range request, a 206
// with a partial body, and bytes appended to a file that already had some — and a fake round-tripper
// proves none of that. The pattern is the one internal/stt already uses for its handshakes.
func modelServer(t *testing.T, body []byte) (*httptest.Server, *int32Counter) {
	t.Helper()
	ranges := &int32Counter{}
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := int64(0)
		if h := r.Header.Get("Range"); h != "" {
			ranges.inc()
			var from int64
			if _, err := fmt.Sscanf(h, "bytes=%d-", &from); err == nil {
				start = from
			}
			w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, int64(len(body))-1, len(body)))
			w.WriteHeader(http.StatusPartialContent)
		}
		if start > int64(len(body)) {
			start = int64(len(body))
		}
		_, _ = w.Write(body[start:])
	})), ranges
}

type int32Counter struct {
	mu sync.Mutex
	n  int
}

func (c *int32Counter) inc() { c.mu.Lock(); c.n++; c.mu.Unlock() }
func (c *int32Counter) get() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.n
}

// downloaderFor builds a downloader whose spec matches the body the test server will serve, so the
// digest check is exercised for real rather than skipped.
func downloaderFor(t *testing.T, body []byte, url string) (*modelDownloader, string) {
	t.Helper()
	sum := sha256.Sum256(body)
	dir := t.TempDir()
	path := filepath.Join(dir, "ggml-small.bin")
	return &modelDownloader{
		spec: store.ModelSpec{
			File:   "ggml-small.bin",
			URL:    url,
			Bytes:  int64(len(body)),
			SHA256: hex.EncodeToString(sum[:]),
		},
		path:   func() string { return path },
		client: http.DefaultClient,
	}, path
}

func TestBundledModelStatusIsNotOfferedForDeletion(t *testing.T) {
	path := filepath.Join(t.TempDir(), "Loqui.app", "Contents", "Resources", "models", "ggml-small.bin")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("model"), 0o600); err != nil {
		t.Fatal(err)
	}

	d := &modelDownloader{
		spec:    store.ModelSpec{File: "ggml-small.bin", Bytes: 5},
		path:    func() string { return path },
		bundled: func(got string) bool { return got == path },
	}
	if status := d.status(); !status.Bundled {
		t.Fatalf("status.Bundled = false for packaged model %q", path)
	}
}

func TestADownloadLandsTheWholeModelAndVerifiesIt(t *testing.T) {
	body := []byte(strings.Repeat("loqui-model-", 400))
	srv, _ := modelServer(t, body)
	defer srv.Close()
	d, path := downloaderFor(t, body, srv.URL)

	if err := d.download(nil); err != nil {
		t.Fatalf("download: %v", err)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(body) {
		t.Errorf("the file on disk is not what the server sent (%d vs %d bytes)", len(got), len(body))
	}
	if v := d.status().Verdict; !v.OK {
		t.Errorf("after a complete download the verdict is %+v, want ok", v)
	}
}

// THE CASE THE WHOLE FEATURE EXISTS FOR. 465 MB over a domestic connection gets interrupted; without
// resumption the user starts from zero every time, which is the difference between a retry costing
// nothing and costing everything.
func TestAnInterruptedDownloadResumesInsteadOfStartingOver(t *testing.T) {
	body := []byte(strings.Repeat("loqui-model-", 400))
	srv, ranges := modelServer(t, body)
	defer srv.Close()
	d, path := downloaderFor(t, body, srv.URL)

	// A previous attempt that stopped a third of the way in. It lives in the .PART file, never at the
	// canonical path — see partSuffix: the real path is only ever occupied by a verified model.
	partial := body[:len(body)/3]
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path+".part", partial, 0o644); err != nil {
		t.Fatal(err)
	}
	// The row must be able to SEE that partial, or it would offer "Download" where it should offer
	// "Resume" — and the user would start 465 MB over.
	if v := d.status().Verdict; v.Problem != store.ModelIncomplete {
		t.Fatalf("precondition: verdict = %+v, want incomplete", v)
	}

	if err := d.download(nil); err != nil {
		t.Fatalf("download: %v", err)
	}

	if ranges.get() != 1 {
		t.Errorf("the server saw %d Range requests, want exactly 1 — it started over", ranges.get())
	}
	got, _ := os.ReadFile(path)
	if string(got) != string(body) {
		t.Errorf("resumed file is wrong: %d bytes, want %d", len(got), len(body))
	}
	if v := d.status().Verdict; !v.OK {
		t.Errorf("verdict after resuming = %+v, want ok", v)
	}
}

// A WRONG DIGEST MUST NOT LEAVE THE FILE IN PLACE. Whisper would load it and transcribe garbage,
// which reads as a bug in this app rather than as a bad download.
func TestAModelThatFailsItsDigestIsNotKept(t *testing.T) {
	body := []byte(strings.Repeat("x", 500))
	srv, _ := modelServer(t, body)
	defer srv.Close()
	d, path := downloaderFor(t, body, srv.URL)
	d.spec.SHA256 = "0000000000000000000000000000000000000000000000000000000000000000"

	err := d.download(nil)

	if err == nil {
		t.Fatal("a digest mismatch must be reported")
	}
	if _, statErr := os.Stat(path); !errors.Is(statErr, os.ErrNotExist) {
		t.Errorf("the corrupt file is still on disk — whisper would load it")
	}
}

// Progress is reported, and the report is monotonic: a bar that goes backwards reads as a bug.
func TestProgressIsReportedAndNeverGoesBackwards(t *testing.T) {
	body := []byte(strings.Repeat("loqui-model-", 4000))
	srv, _ := modelServer(t, body)
	defer srv.Close()
	d, _ := downloaderFor(t, body, srv.URL)

	var seen []int
	if err := d.download(func(p ModelProgress) { seen = append(seen, p.Percent) }); err != nil {
		t.Fatalf("download: %v", err)
	}
	if len(seen) == 0 {
		t.Fatal("no progress was reported at all")
	}
	for i := 1; i < len(seen); i++ {
		if seen[i] < seen[i-1] {
			t.Fatalf("progress went backwards: %v", seen)
		}
	}
	if last := seen[len(seen)-1]; last != 100 {
		t.Errorf("the last report was %d%%, want 100", last)
	}
}

// Removing is offered only for a model this app downloaded, and it has to actually remove it.
func TestRemoveDeletesTheModel(t *testing.T) {
	body := []byte("small")
	srv, _ := modelServer(t, body)
	defer srv.Close()
	d, path := downloaderFor(t, body, srv.URL)
	if err := os.WriteFile(path, body, 0o644); err != nil {
		t.Fatal(err)
	}

	if err := d.remove(); err != nil {
		t.Fatalf("remove: %v", err)
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Error("the model is still there")
	}
	if v := d.status().Verdict; v.Problem != store.ModelMissing {
		t.Errorf("verdict after removing = %+v, want missing", v)
	}
}

// ── THE PART FILE, which is what makes every other guarantee here true ────────────────────────
//
// The canonical path must never hold a file that has not been verified. Everything else in the app
// checks that path by EXISTENCE (dictation.go) or by SIZE (provider_fallback.go), so a partial or
// unverified file sitting there means Whisper is launched against garbage — which surfaces as
// nonsense transcription, not as a bad download. Found by a cross-engine review.
func TestThePartialDownloadNeverOccupiesTheRealPath(t *testing.T) {
	body := []byte(strings.Repeat("loqui-model-", 400))
	released := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(body[:10])
		w.(http.Flusher).Flush()
		<-released // stall mid-body, with the real path still empty
		_, _ = w.Write(body[10:])
	}))
	defer srv.Close()
	d, path := downloaderFor(t, body, srv.URL)

	done := make(chan error, 1)
	go func() { done <- d.download(nil) }()

	// While it runs, the real path must not exist and the .part must.
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(path + ".part"); err == nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("the real model path exists mid-download — Whisper would load a partial file")
	}
	if _, err := os.Stat(path + ".part"); err != nil {
		t.Errorf("no .part file while downloading: %v", err)
	}
	close(released)
	if err := <-done; err != nil {
		t.Fatalf("download: %v", err)
	}
	if _, err := os.Stat(path + ".part"); !errors.Is(err, os.ErrNotExist) {
		t.Error("the .part file survived a successful download")
	}
	if st := d.status(); !st.Verdict.OK {
		t.Errorf("verdict = %+v, want ok", st.Verdict)
	}
}

// A SERVER THAT IGNORES Range answers 200 with the whole body. Appending that to what is already on
// disk produces a file of the right length made of the wrong bytes — caught by the digest, but only
// after another 465 MB.
func TestAServerThatIgnoresRangeMakesItStartOver(t *testing.T) {
	body := []byte(strings.Repeat("loqui-model-", 400))
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(body) // 200, whole body, Range ignored
	}))
	defer srv.Close()
	d, path := downloaderFor(t, body, srv.URL)
	if err := os.WriteFile(path+".part", body[:100], 0o644); err != nil {
		t.Fatal(err)
	}

	if err := d.download(nil); err != nil {
		t.Fatalf("download: %v", err)
	}
	got, _ := os.ReadFile(path)
	if string(got) != string(body) {
		t.Errorf("got %d bytes, want %d — the ignored Range was appended instead of restarting",
			len(got), len(body))
	}
}

// A 206 whose Content-Range starts somewhere ELSE than we asked for must be refused, not appended.
// A caching proxy is the realistic source, and the result would be a good prefix glued to the wrong
// suffix.
func TestAMisalignedRangeIsRefused(t *testing.T) {
	body := []byte(strings.Repeat("loqui-model-", 400))
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Asked for bytes=100-, answers from 0 anyway but claims 206.
		w.Header().Set("Content-Range", fmt.Sprintf("bytes 0-%d/%d", len(body)-1, len(body)))
		w.WriteHeader(http.StatusPartialContent)
		_, _ = w.Write(body)
	}))
	defer srv.Close()
	d, path := downloaderFor(t, body, srv.URL)
	if err := os.WriteFile(path+".part", body[:100], 0o644); err != nil {
		t.Fatal(err)
	}

	err := d.download(nil)

	if err == nil {
		t.Fatal("a misaligned 206 must be reported, not appended")
	}
	if _, statErr := os.Stat(path); !errors.Is(statErr, os.ErrNotExist) {
		t.Error("a model was published from a misaligned range")
	}
}

// Cancel has to work while the transfer is STALLED, which is the case it exists for. Checking a
// channel before a blocking read cannot do it — the cancellation has to reach the request.
func TestCancelStopsAStalledDownload(t *testing.T) {
	body := []byte(strings.Repeat("x", 5000))
	block := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(body[:10])
		w.(http.Flusher).Flush()
		<-block // never sends the rest
	}))
	defer srv.Close()
	defer close(block)
	d, _ := downloaderFor(t, body, srv.URL)

	done := make(chan error, 1)
	go func() { done <- d.download(nil) }()
	// Wait until it is actually reading, then cancel.
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) && !d.status().Downloading {
		time.Sleep(10 * time.Millisecond)
	}
	d.cancel()

	select {
	case err := <-done:
		if err == nil {
			t.Error("a cancelled download must report why")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Cancel did not stop a stalled download — it is not bound to the request")
	}
}

// Two downloads at once would interleave writes into one file.
func TestASecondDownloadIsRefusedWhileOneRuns(t *testing.T) {
	body := []byte(strings.Repeat("y", 5000))
	block := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(body[:10])
		w.(http.Flusher).Flush()
		<-block
	}))
	defer srv.Close()
	d, _ := downloaderFor(t, body, srv.URL)

	go func() { _ = d.download(nil) }()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) && !d.status().Downloading {
		time.Sleep(10 * time.Millisecond)
	}
	second := d.download(nil)
	close(block)
	d.cancel()

	if second == nil {
		t.Error("a second concurrent download must be refused")
	}
}

// THE ETA MUST MEASURE THIS SESSION, not the file. Resuming 400 MB and receiving 1 MB in a second is
// not 401 MB/s, and reporting it as such shows "0 s remaining" for the next minute.
func TestTheETAIgnoresBytesThatWereAlreadyOnDisk(t *testing.T) {
	d := &modelDownloader{spec: store.ModelSpec{Bytes: 500_000_000}}
	// 400 MB was already there; 1 MB arrived in this session, one second ago.
	p := d.progress(401_000_000, 400_000_000, time.Now().Add(-time.Second))
	if !strings.Contains(p.Text, "min") {
		t.Errorf("progress text = %q — an ETA computed from 1 MB/s should be minutes, not seconds", p.Text)
	}
}
